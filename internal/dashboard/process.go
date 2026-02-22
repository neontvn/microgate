package dashboard

import (
	"context"
	"fmt"
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

	// Pipe output to gateway stdout for debugging crashes
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start the process
	if err := cmd.Start(); err != nil {
		cancel()
		p.Status = StatusCrashed
		return fmt.Errorf("failed to start process: %w", err)
	}

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
