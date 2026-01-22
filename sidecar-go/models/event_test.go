package models

import (
	"encoding/json"
	"testing"
)

func TestEvent_IsError(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		expected bool
	}{
		{
			name:     "successful request",
			event:    Event{Status: 200},
			expected: false,
		},
		{
			name:     "redirect",
			event:    Event{Status: 302},
			expected: false,
		},
		{
			name:     "client error",
			event:    Event{Status: 404},
			expected: true,
		},
		{
			name:     "server error",
			event:    Event{Status: 500},
			expected: true,
		},
		{
			name:     "error code set",
			event:    Event{Status: 0, ErrorCode: "TIMEOUT"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.IsError(); got != tt.expected {
				t.Errorf("IsError() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestEvent_GetTag(t *testing.T) {
	event := Event{
		Tags: map[string]string{
			"region":  "us-east-1",
			"user_id": "user123",
		},
	}

	tests := []struct {
		key      string
		expected string
	}{
		{"region", "us-east-1"},
		{"user_id", "user123"},
		{"nonexistent", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := event.GetTag(tt.key); got != tt.expected {
				t.Errorf("GetTag(%s) = %v, expected %v", tt.key, got, tt.expected)
			}
		})
	}

	// Test nil tags
	eventNoTags := Event{}
	if got := eventNoTags.GetTag("any"); got != "" {
		t.Errorf("GetTag on nil tags = %v, expected empty string", got)
	}
}

func TestEvent_JSONSerialization(t *testing.T) {
	event := Event{
		RunID:          "test-run",
		Ts:             1704067200000,
		PodID:          "pod-1",
		VU:             1,
		Iter:           10,
		Method:         "POST",
		URL:            "https://example.com/api",
		Name:           "login",
		Status:         200,
		BodyLen:        1024,
		RTT:            123.45,
		DNSLookup:      5.0,
		TCPConnect:     10.0,
		TLSHandshake:   20.0,
		TTFB:           80.0,
		ContentTransfer: 8.45,
		RequestSize:    256,
		ResponseSize:   1280,
		Tags: map[string]string{
			"region": "us-east-1",
		},
	}

	// Marshal
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}

	// Unmarshal
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	// Verify
	if decoded.RunID != event.RunID {
		t.Errorf("RunID mismatch: got %s, expected %s", decoded.RunID, event.RunID)
	}
	if decoded.RTT != event.RTT {
		t.Errorf("RTT mismatch: got %f, expected %f", decoded.RTT, event.RTT)
	}
	if decoded.DNSLookup != event.DNSLookup {
		t.Errorf("DNSLookup mismatch: got %f, expected %f", decoded.DNSLookup, event.DNSLookup)
	}
	if decoded.GetTag("region") != "us-east-1" {
		t.Errorf("Tag region mismatch: got %s, expected us-east-1", decoded.GetTag("region"))
	}
}

func TestEvent_MinimalJSON(t *testing.T) {
	// Test backward compatibility with minimal event (old format)
	jsonData := `{
		"run_id": "test",
		"ts": 1704067200000,
		"pod_id": "pod-1",
		"url": "https://example.com",
		"status": 200,
		"rtt": 50.0,
		"body_len": 100
	}`

	var event Event
	if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
		t.Fatalf("Failed to unmarshal minimal event: %v", err)
	}

	if event.RunID != "test" {
		t.Errorf("RunID = %s, expected test", event.RunID)
	}
	if event.Status != 200 {
		t.Errorf("Status = %d, expected 200", event.Status)
	}
	// Optional fields should be zero values
	if event.DNSLookup != 0 {
		t.Errorf("DNSLookup = %f, expected 0", event.DNSLookup)
	}
	if event.Tags != nil {
		t.Errorf("Tags = %v, expected nil", event.Tags)
	}
}
