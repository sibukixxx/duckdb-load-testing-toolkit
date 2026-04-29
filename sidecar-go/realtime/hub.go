package realtime

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// MetricType represents the type of metric being streamed
type MetricType string

const (
	MetricTypeEvent     MetricType = "event"
	MetricTypeStats     MetricType = "stats"
	MetricTypeHeartbeat MetricType = "heartbeat"
)

// Message represents a message sent to clients
type Message struct {
	Type      MetricType  `json:"type"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// LiveStats represents real-time statistics
type LiveStats struct {
	TotalRequests  int64   `json:"total_requests"`
	RequestsPerSec float64 `json:"requests_per_sec"`
	ErrorCount     int64   `json:"error_count"`
	ErrorRate      float64 `json:"error_rate"`
	AvgRTT         float64 `json:"avg_rtt_ms"`
	P50RTT         float64 `json:"p50_rtt_ms"`
	P95RTT         float64 `json:"p95_rtt_ms"`
	P99RTT         float64 `json:"p99_rtt_ms"`
	ActiveVUs      int     `json:"active_vus"`
	BufferSize     int     `json:"buffer_size"`
	LastEventTime  int64   `json:"last_event_time"`
}

// Hub manages WebSocket clients and broadcasts messages
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex

	// Stats tracking
	stats       LiveStats
	statsMu     sync.RWMutex
	rttBuffer   []float64
	rttBufferMu sync.Mutex
	windowSize  int
	lastSecond  int64
	reqCount    int64
}

// NewHub creates a new Hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		rttBuffer:  make([]float64, 0, 1000),
		windowSize: 1000, // Keep last 1000 RTT values for percentile calculation
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("Client connected, total: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("Client disconnected, total: %d", len(h.clients))

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client buffer full, mark for removal
					go func(c *Client) {
						h.unregister <- c
					}(client)
				}
			}
			h.mu.RUnlock()

		case <-ticker.C:
			h.sendStats()
		}
	}
}

// Register registers a client
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister unregisters a client
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// ClientCount returns the number of connected clients
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// RecordEvent records an incoming event for stats calculation
func (h *Hub) RecordEvent(rtt float64, isError bool, vu int) {
	now := time.Now().Unix()

	h.statsMu.Lock()
	h.stats.TotalRequests++
	if isError {
		h.stats.ErrorCount++
	}
	h.stats.LastEventTime = time.Now().UnixMilli()

	// Track requests per second
	if now != h.lastSecond {
		h.stats.RequestsPerSec = float64(h.reqCount)
		h.reqCount = 1
		h.lastSecond = now
	} else {
		h.reqCount++
	}
	h.statsMu.Unlock()

	// Add RTT to buffer
	h.rttBufferMu.Lock()
	h.rttBuffer = append(h.rttBuffer, rtt)
	if len(h.rttBuffer) > h.windowSize {
		h.rttBuffer = h.rttBuffer[1:]
	}
	h.rttBufferMu.Unlock()
}

// UpdateBufferSize updates the current buffer size
func (h *Hub) UpdateBufferSize(size int) {
	h.statsMu.Lock()
	h.stats.BufferSize = size
	h.statsMu.Unlock()
}

// sendStats broadcasts current stats to all clients
func (h *Hub) sendStats() {
	h.calculatePercentiles()

	h.statsMu.RLock()
	stats := h.stats
	if stats.TotalRequests > 0 {
		stats.ErrorRate = float64(stats.ErrorCount) / float64(stats.TotalRequests) * 100
	}
	h.statsMu.RUnlock()

	msg := Message{
		Type:      MetricTypeStats,
		Timestamp: time.Now().UnixMilli(),
		Data:      stats,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal stats: %v", err)
		return
	}

	h.broadcast <- data
}

// calculatePercentiles calculates RTT percentiles from the buffer
func (h *Hub) calculatePercentiles() {
	h.rttBufferMu.Lock()
	if len(h.rttBuffer) == 0 {
		h.rttBufferMu.Unlock()
		return
	}

	// Copy buffer
	values := make([]float64, len(h.rttBuffer))
	copy(values, h.rttBuffer)
	h.rttBufferMu.Unlock()

	// Sort for percentile calculation
	sortFloat64s(values)

	h.statsMu.Lock()
	h.stats.AvgRTT = average(values)
	h.stats.P50RTT = percentile(values, 0.50)
	h.stats.P95RTT = percentile(values, 0.95)
	h.stats.P99RTT = percentile(values, 0.99)
	h.statsMu.Unlock()
}

// BroadcastEvent broadcasts an event to all clients
func (h *Hub) BroadcastEvent(event interface{}) {
	msg := Message{
		Type:      MetricTypeEvent,
		Timestamp: time.Now().UnixMilli(),
		Data:      event,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return
	}

	h.broadcast <- data
}

// GetStats returns current statistics
func (h *Hub) GetStats() LiveStats {
	h.statsMu.RLock()
	defer h.statsMu.RUnlock()
	return h.stats
}

// Helper functions

func sortFloat64s(values []float64) {
	// Simple insertion sort for small arrays
	for i := 1; i < len(values); i++ {
		key := values[i]
		j := i - 1
		for j >= 0 && values[j] > key {
			values[j+1] = values[j]
			j--
		}
		values[j+1] = key
	}
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func percentile(sortedValues []float64, p float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}
	index := int(float64(len(sortedValues)-1) * p)
	return sortedValues[index]
}
