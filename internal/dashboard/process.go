package dashboard

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// ProcessStatus represents the current state of a managed process
type ProcessStatus string

const (
	StatusRunning ProcessStatus = "running"
	StatusStopped ProcessStatus = "stopped"
	StatusCrashed ProcessStatus = "crashed"
)

// lineBuffer is a thread-safe ring buffer that stores the last N output lines.
type lineBuffer struct {
	lines []string
	mu    sync.RWMutex
	size  int
	index int
	count int
}

func newLineBuffer(capacity int) *lineBuffer {
	return &lineBuffer{
		lines: make([]string, capacity),
		size:  capacity,
	}
}

func (b *lineBuffer) Add(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines[b.index] = line
	b.index = (b.index + 1) % b.size
	if b.count < b.size {
		b.count++
	}
}

// Recent returns the last n lines, oldest first.
func (b *lineBuffer) Recent(n int) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n > b.count {
		n = b.count
	}
	if n <= 0 {
		return []string{}
	}
	result := make([]string, n)
	startIdx := b.index - n
	if startIdx < 0 {
		startIdx += b.size
	}
	for i := 0; i < n; i++ {
		idx := (startIdx + i) % b.size
		result[i] = b.lines[idx]
	}
	return result
}

// ManagedProcess holds metadata and control structures for a backend process
type ManagedProcess struct {
	ID        string        `json:"id"`
	Command   string        `json:"command"`
	Args      []string      `json:"args"`
	Port      int           `json:"port"`
	Status    ProcessStatus `json:"status"`
	PID       int           `json:"pid,omitempty"`
	StartedAt *time.Time    `json:"started_at,omitempty"`

	cmd    *exec.Cmd
	cancel context.CancelFunc
	output *lineBuffer
}

// ProcessManager controls the lifecycle of backend processes
type ProcessManager struct {
	processes     map[string]*ManagedProcess
	mu            sync.RWMutex
	OnStateChange func(p ManagedProcess) // hook for SSE updates
}

// NewProcessManager creates a new process manager
func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*ManagedProcess),
	}
}

// Add registers a new process to be managed
func (m *ProcessManager) Add(id, command string, args []string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.processes[id]; exists {
		return fmt.Errorf("process with ID %s already exists", id)
	}

	m.processes[id] = &ManagedProcess{
		ID:      id,
		Command: command,
		Args:    args,
		Port:    port,
		Status:  StatusStopped,
	}

	return nil
}

// Start launches a managed process
func (m *ProcessManager) Start(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, exists := m.processes[id]
	if !exists {
		return fmt.Errorf("process with ID %s not found", id)
	}

	if p.Status == StatusRunning {
		return fmt.Errorf("process %s is already running", id)
	}

	// Create context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, p.Command, p.Args...)

	// Initialize output buffer if needed
	if p.output == nil {
		p.output = newLineBuffer(200)
	}

	// Capture stdout and stderr, tee to os.Stdout/os.Stderr for debugging
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		cancel()
		p.Status = StatusCrashed
		return fmt.Errorf("failed to start process: %w", err)
	}

	// Scan stdout and stderr in background goroutines
	scanPipe := func(pipe io.ReadCloser, dest *os.File) {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			line := scanner.Text()
			p.output.Add(line)
			fmt.Fprintln(dest, line)
		}
	}
	go scanPipe(stdoutPipe, os.Stdout)
	go scanPipe(stderrPipe, os.Stderr)

	// Update state
	now := time.Now()
	p.cmd = cmd
	p.cancel = cancel
	p.PID = cmd.Process.Pid
	p.Status = StatusRunning
	p.StartedAt = &now

	// Fire event
	if m.OnStateChange != nil {
		m.OnStateChange(*p)
	}

	// Monitor process in background
	go func(proc *ManagedProcess, c *exec.Cmd) {
		err := c.Wait()

		m.mu.Lock()
		defer m.mu.Unlock()

		// If the process we're monitoring is still the active one
		if proc.cmd == c {
			proc.cmd = nil
			proc.cancel = nil
			proc.PID = 0
			proc.StartedAt = nil

			// If it exited with an error and wasn't explicitly stopped via context cancellation
			if err != nil && c.ProcessState.ExitCode() != -1 {
				fmt.Printf("[ProcessManager] process %s crashed: %v\n", proc.ID, err)
				proc.Status = StatusCrashed
			} else {
				fmt.Printf("[ProcessManager] process %s stopped normally\n", proc.ID)
				proc.Status = StatusStopped
			}

			// Fire event for the transition to stopped/crashed
			if m.OnStateChange != nil {
				m.OnStateChange(*proc)
			}
		}
	}(p, cmd)

	return nil
}

// Stop terminates a managed process
func (m *ProcessManager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, exists := m.processes[id]
	if !exists {
		return fmt.Errorf("process with ID %s not found", id)
	}

	if p.Status != StatusRunning {
		return fmt.Errorf("process %s is not running", id)
	}

	// Cancel the context to kill the process
	if p.cancel != nil {
		p.cancel()
	}

	p.Status = StatusStopped
	p.cmd = nil
	p.cancel = nil
	p.PID = 0
	p.StartedAt = nil

	return nil
}

// List returns a snapshot of all managed processes
func (m *ProcessManager) List() []ManagedProcess {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]ManagedProcess, 0, len(m.processes))
	for _, p := range m.processes {
		list = append(list, *p)
	}
	return list
}

// Logs returns the recent output lines for a given process.
func (m *ProcessManager) Logs(id string, lines int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, exists := m.processes[id]
	if !exists {
		return nil, fmt.Errorf("process with ID %s not found", id)
	}
	if p.output == nil {
		return []string{}, nil
	}
	return p.output.Recent(lines), nil
}

// StopAll gracefully shuts down all running processes
func (m *ProcessManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.processes {
		if p.Status == StatusRunning && p.cancel != nil {
			p.cancel()
		}
	}
}
