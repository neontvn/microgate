package dashboard

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// Event represents a single Server-Sent Event payload
type Event struct {
	Type string `json:"type"`
	JSON []byte `json:"data"`
}

// Broker manages connected SSE clients and broadcasts events
type Broker struct {
	mu         sync.RWMutex
	clients    map[chan Event]bool
	broadcast  chan Event
	register   chan chan Event
	unregister chan chan Event
}

// NewBroker creates and starts a new SSE Broker
func NewBroker() *Broker {
	b := &Broker{
		clients:    make(map[chan Event]bool),
		broadcast:  make(chan Event, 256),
		register:   make(chan chan Event),
		unregister: make(chan chan Event),
	}
	go b.start()
	return b
}

func (b *Broker) start() {
	for {
		select {
		case ch := <-b.register:
			b.mu.Lock()
			b.clients[ch] = true
			b.mu.Unlock()
			log.Printf("SSE Broker: New client connected (total: %d)", len(b.clients))

		case ch := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[ch]; ok {
				delete(b.clients, ch)
				close(ch)
				log.Printf("SSE Broker: Client disconnected (total: %d)", len(b.clients))
			}
			b.mu.Unlock()

		case event := <-b.broadcast:
			b.mu.RLock()
			for ch := range b.clients {
				// Use non-blocking send to avoid slow clients blocking the broker
				select {
				case ch <- event:
				default:
					log.Printf("SSE Broker: Dropped event for slow client")
				}
			}
			b.mu.RUnlock()
		}
	}
}

// Subscribe adds a new client and returns a channel to listen for events
func (b *Broker) Subscribe() chan Event {
	ch := make(chan Event, 256)
	b.register <- ch
	return ch
}

// Unsubscribe removes a client
func (b *Broker) Unsubscribe(ch chan Event) {
	b.unregister <- ch
}

// Broadcast sends an event to all connected clients
func (b *Broker) Broadcast(eventType string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("SSE Broker Error: failed to marshal event '%s': %v", eventType, err)
		return
	}
	b.broadcast <- Event{Type: eventType, JSON: data}
}

// StreamHandler returns an HTTP handler for establishing SSE connections
func (b *Broker) StreamHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set headers for Server-Sent Events
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// Allow CORS for the dashboard UI
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		ch := b.Subscribe()
		defer b.Unsubscribe(ch)

		// Send initial connection event (optional, helps React hook know it's connected)
		fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
		flusher.Flush()

		// Listen for connection close or new events
		for {
			select {
			case <-r.Context().Done():
				// Client disconnected
				return
			case event := <-ch:
				// Write the event format exactly as standard demands
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.JSON)
				flusher.Flush()
			}
		}
	}
}
