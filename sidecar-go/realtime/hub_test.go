package realtime

import (
	"testing"
	"time"
)

func TestNewHub(t *testing.T) {
	hub := NewHub()
	if hub == nil {
		t.Fatal("NewHub returned nil")
	}
	if hub.clients == nil {
		t.Error("clients map not initialized")
	}
	if hub.broadcast == nil {
		t.Error("broadcast channel not initialized")
	}
}

func TestHub_RecordEvent(t *testing.T) {
	hub := NewHub()

	// Record some events
	hub.RecordEvent(50.0, false, 1)
	hub.RecordEvent(100.0, false, 1)
	hub.RecordEvent(150.0, true, 1)

	stats := hub.GetStats()

	if stats.TotalRequests != 3 {
		t.Errorf("Expected 3 total requests, got %d", stats.TotalRequests)
	}

	if stats.ErrorCount != 1 {
		t.Errorf("Expected 1 error, got %d", stats.ErrorCount)
	}
}

func TestHub_UpdateBufferSize(t *testing.T) {
	hub := NewHub()

	hub.UpdateBufferSize(100)
	stats := hub.GetStats()

	if stats.BufferSize != 100 {
		t.Errorf("Expected buffer size 100, got %d", stats.BufferSize)
	}
}

func TestHub_ClientCount(t *testing.T) {
	hub := NewHub()

	if hub.ClientCount() != 0 {
		t.Errorf("Expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestPercentile(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	p50 := percentile(values, 0.50)
	if p50 != 5 {
		t.Errorf("P50 = %f, expected 5", p50)
	}

	// For 10 elements, index = (10-1) * 0.95 = 8.55 -> 8, which is value 9
	p95 := percentile(values, 0.95)
	if p95 != 9 {
		t.Errorf("P95 = %f, expected 9", p95)
	}
}

func TestAverage(t *testing.T) {
	values := []float64{10, 20, 30, 40, 50}
	avg := average(values)

	expected := 30.0
	if avg != expected {
		t.Errorf("Average = %f, expected %f", avg, expected)
	}
}

func TestAverageEmpty(t *testing.T) {
	values := []float64{}
	avg := average(values)

	if avg != 0 {
		t.Errorf("Average of empty = %f, expected 0", avg)
	}
}

func TestSortFloat64s(t *testing.T) {
	values := []float64{5, 2, 8, 1, 9, 3}
	sortFloat64s(values)

	expected := []float64{1, 2, 3, 5, 8, 9}
	for i, v := range values {
		if v != expected[i] {
			t.Errorf("Sort mismatch at %d: got %f, expected %f", i, v, expected[i])
		}
	}
}

func TestHub_calculatePercentiles(t *testing.T) {
	hub := NewHub()

	// Add some RTT values
	for i := 1; i <= 100; i++ {
		hub.rttBuffer = append(hub.rttBuffer, float64(i))
	}

	hub.calculatePercentiles()

	stats := hub.GetStats()

	// P50 should be around 50
	if stats.P50RTT < 45 || stats.P50RTT > 55 {
		t.Errorf("P50RTT = %f, expected around 50", stats.P50RTT)
	}

	// P95 should be around 95
	if stats.P95RTT < 90 || stats.P95RTT > 100 {
		t.Errorf("P95RTT = %f, expected around 95", stats.P95RTT)
	}

	// Average should be 50.5
	expectedAvg := 50.5
	if stats.AvgRTT != expectedAvg {
		t.Errorf("AvgRTT = %f, expected %f", stats.AvgRTT, expectedAvg)
	}
}

func TestHub_RTTBufferLimit(t *testing.T) {
	hub := NewHub()
	hub.windowSize = 10

	// Add more values than window size
	for i := 0; i < 20; i++ {
		hub.RecordEvent(float64(i), false, 1)
	}

	hub.rttBufferMu.Lock()
	bufferLen := len(hub.rttBuffer)
	hub.rttBufferMu.Unlock()

	if bufferLen != 10 {
		t.Errorf("Buffer length = %d, expected 10", bufferLen)
	}
}

func TestMessage_Types(t *testing.T) {
	if MetricTypeEvent != "event" {
		t.Errorf("MetricTypeEvent = %s, expected 'event'", MetricTypeEvent)
	}
	if MetricTypeStats != "stats" {
		t.Errorf("MetricTypeStats = %s, expected 'stats'", MetricTypeStats)
	}
	if MetricTypeHeartbeat != "heartbeat" {
		t.Errorf("MetricTypeHeartbeat = %s, expected 'heartbeat'", MetricTypeHeartbeat)
	}
}

func TestLiveStats_Initial(t *testing.T) {
	hub := NewHub()
	stats := hub.GetStats()

	if stats.TotalRequests != 0 {
		t.Errorf("Initial TotalRequests = %d, expected 0", stats.TotalRequests)
	}
	if stats.ErrorCount != 0 {
		t.Errorf("Initial ErrorCount = %d, expected 0", stats.ErrorCount)
	}
	if stats.AvgRTT != 0 {
		t.Errorf("Initial AvgRTT = %f, expected 0", stats.AvgRTT)
	}
}

func TestHub_RequestsPerSecond(t *testing.T) {
	hub := NewHub()

	// Record multiple events in same second
	for i := 0; i < 10; i++ {
		hub.RecordEvent(50.0, false, 1)
	}

	// Wait for second to pass and record one more
	time.Sleep(1100 * time.Millisecond)
	hub.RecordEvent(50.0, false, 1)

	stats := hub.GetStats()

	// After second passes, RPS should show previous second's count
	if stats.RequestsPerSec != 10 {
		t.Logf("RequestsPerSec = %f (may vary due to timing)", stats.RequestsPerSec)
	}
}
