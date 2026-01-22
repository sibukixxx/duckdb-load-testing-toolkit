package models

// Event represents a detailed metrics event from k6 load test
type Event struct {
	// Basic identification
	RunID  string `json:"run_id"`
	Ts     int64  `json:"ts"`
	PodID  string `json:"pod_id"`
	VU     int    `json:"vu,omitempty"`      // Virtual User ID
	Iter   int    `json:"iter,omitempty"`    // Iteration number

	// Request info
	Method string `json:"method,omitempty"` // HTTP method (GET, POST, etc.)
	URL    string `json:"url"`
	Name   string `json:"name,omitempty"`   // Scenario/request name

	// Response info
	Status  int `json:"status"`
	BodyLen int `json:"body_len"`

	// Detailed timings (milliseconds)
	RTT            float64 `json:"rtt"`                       // Total round-trip time
	DNSLookup      float64 `json:"dns_lookup,omitempty"`      // DNS resolution time
	TCPConnect     float64 `json:"tcp_connect,omitempty"`     // TCP connection time
	TLSHandshake   float64 `json:"tls_handshake,omitempty"`   // TLS handshake time
	TTFB           float64 `json:"ttfb,omitempty"`            // Time to first byte
	ContentTransfer float64 `json:"content_transfer,omitempty"` // Content download time

	// Size metrics
	RequestSize  int `json:"request_size,omitempty"`  // Request size in bytes
	ResponseSize int `json:"response_size,omitempty"` // Response size in bytes (headers + body)

	// Error handling
	ErrorCode string `json:"error_code,omitempty"` // Error code if failed
	ErrorMsg  string `json:"error_msg,omitempty"`  // Error message if failed

	// Custom tags
	Tags map[string]string `json:"tags,omitempty"` // Custom tags (region, user_id, etc.)
}

// IsError returns true if the event represents an error
func (e *Event) IsError() bool {
	return e.ErrorCode != "" || e.Status >= 400
}

// GetTag returns a tag value or empty string if not found
func (e *Event) GetTag(key string) string {
	if e.Tags == nil {
		return ""
	}
	return e.Tags[key]
}
