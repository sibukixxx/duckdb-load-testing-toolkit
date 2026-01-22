package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewController(t *testing.T) {
	c := NewController()
	if c == nil {
		t.Fatal("NewController returned nil")
	}
	if c.pods == nil {
		t.Error("pods map not initialized")
	}
	if c.runs == nil {
		t.Error("runs map not initialized")
	}
}

func TestController_RegisterUnregisterPod(t *testing.T) {
	c := NewController()

	// Register
	c.RegisterPod("pod-1", "10.0.0.1:8081")
	c.RegisterPod("pod-2", "10.0.0.2:8081")

	pods := c.GetPods()
	if len(pods) != 2 {
		t.Errorf("Expected 2 pods, got %d", len(pods))
	}

	// Unregister
	c.UnregisterPod("pod-1")
	pods = c.GetPods()
	if len(pods) != 1 {
		t.Errorf("Expected 1 pod after unregister, got %d", len(pods))
	}
}

func TestController_HealthCheckAll(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "ok",
				"buffer_size": 10,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c := NewController()
	// Extract host:port from server URL
	addr := server.Listener.Addr().String()
	c.RegisterPod("test-pod", addr)

	ctx := context.Background()
	results := c.HealthCheckAll(ctx)

	if err, ok := results["test-pod"]; ok && err != nil {
		t.Errorf("Health check failed: %v", err)
	}

	// Verify pod status was updated
	pods := c.GetPods()
	if len(pods) != 1 {
		t.Fatal("Expected 1 pod")
	}
	if pods[0].Status != PodStatusHealthy {
		t.Errorf("Expected status healthy, got %s", pods[0].Status)
	}
	if pods[0].BufferSize != 10 {
		t.Errorf("Expected buffer size 10, got %d", pods[0].BufferSize)
	}
}

func TestController_FlushAll(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/flush-upload" && r.Method == "POST" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"flushed":  100,
				"uploaded": true,
				"key":      "runs/test/file.duckdb",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c := NewController()
	addr := server.Listener.Addr().String()
	c.RegisterPod("test-pod", addr)

	ctx := context.Background()
	results := c.FlushAll(ctx, true)

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Error != "" {
		t.Errorf("Flush failed: %s", result.Error)
	}
	if result.Flushed != 100 {
		t.Errorf("Expected 100 flushed, got %d", result.Flushed)
	}
	if !result.Uploaded {
		t.Error("Expected uploaded to be true")
	}
}

func TestController_StartEndRun(t *testing.T) {
	c := NewController()

	// Register a healthy pod
	c.RegisterPod("pod-1", "10.0.0.1:8081")
	c.podsMu.Lock()
	c.pods["pod-1"].Status = PodStatusHealthy
	c.podsMu.Unlock()

	// Start run
	run, err := c.StartRun("test-run-001")
	if err != nil {
		t.Fatalf("StartRun failed: %v", err)
	}
	if run.ID != "test-run-001" {
		t.Errorf("Expected run ID 'test-run-001', got '%s'", run.ID)
	}
	if run.Status != "running" {
		t.Errorf("Expected status 'running', got '%s'", run.Status)
	}
	if len(run.Pods) != 1 {
		t.Errorf("Expected 1 pod in run, got %d", len(run.Pods))
	}

	// Try to start duplicate run
	_, err = c.StartRun("test-run-001")
	if err == nil {
		t.Error("Expected error for duplicate run")
	}

	// End run
	run, err = c.EndRun("test-run-001")
	if err != nil {
		t.Fatalf("EndRun failed: %v", err)
	}
	if run.Status != "completed" {
		t.Errorf("Expected status 'completed', got '%s'", run.Status)
	}
	if run.EndTime.IsZero() {
		t.Error("Expected EndTime to be set")
	}
}

func TestController_GetRun(t *testing.T) {
	c := NewController()
	c.RegisterPod("pod-1", "10.0.0.1:8081")
	c.podsMu.Lock()
	c.pods["pod-1"].Status = PodStatusHealthy
	c.podsMu.Unlock()

	c.StartRun("test-run")

	run, exists := c.GetRun("test-run")
	if !exists {
		t.Error("Expected run to exist")
	}
	if run.ID != "test-run" {
		t.Errorf("Expected run ID 'test-run', got '%s'", run.ID)
	}

	_, exists = c.GetRun("nonexistent")
	if exists {
		t.Error("Expected nonexistent run to not exist")
	}
}

func TestController_GetRuns(t *testing.T) {
	c := NewController()
	c.RegisterPod("pod-1", "10.0.0.1:8081")
	c.podsMu.Lock()
	c.pods["pod-1"].Status = PodStatusHealthy
	c.podsMu.Unlock()

	c.StartRun("run-1")
	c.StartRun("run-2")
	c.StartRun("run-3")

	runs := c.GetRuns()
	if len(runs) != 3 {
		t.Errorf("Expected 3 runs, got %d", len(runs))
	}
}

func TestController_WaitForCompletion(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			callCount++
			bufferSize := 10
			if callCount >= 3 {
				bufferSize = 0 // Empty after 3 calls
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "ok",
				"buffer_size": bufferSize,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c := NewController()
	addr := server.Listener.Addr().String()
	c.RegisterPod("test-pod", addr)
	c.podsMu.Lock()
	c.pods["test-pod"].Status = PodStatusHealthy
	c.podsMu.Unlock()

	ctx := context.Background()
	err := c.WaitForCompletion(ctx, 100*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Errorf("WaitForCompletion failed: %v", err)
	}
}

func TestController_WaitForCompletion_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "ok",
				"buffer_size": 100, // Never empty
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c := NewController()
	addr := server.Listener.Addr().String()
	c.RegisterPod("test-pod", addr)
	c.podsMu.Lock()
	c.pods["test-pod"].Status = PodStatusHealthy
	c.podsMu.Unlock()

	ctx := context.Background()
	err := c.WaitForCompletion(ctx, 50*time.Millisecond, 200*time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func TestController_GetStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/stats" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"total_requests": 1000,
				"error_count":    50,
				"buffer_size":    10,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c := NewController()
	addr := server.Listener.Addr().String()
	c.RegisterPod("test-pod", addr)

	ctx := context.Background()
	stats, err := c.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats["total_requests"].(int64) != 1000 {
		t.Errorf("Expected 1000 requests, got %v", stats["total_requests"])
	}
	if stats["total_errors"].(int64) != 50 {
		t.Errorf("Expected 50 errors, got %v", stats["total_errors"])
	}
	if stats["pod_count"].(int) != 1 {
		t.Errorf("Expected 1 pod, got %v", stats["pod_count"])
	}
}
