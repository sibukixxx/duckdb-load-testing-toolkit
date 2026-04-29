package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// PodStatus represents the status of a sidecar pod
type PodStatus string

const (
	PodStatusUnknown   PodStatus = "unknown"
	PodStatusHealthy   PodStatus = "healthy"
	PodStatusUnhealthy PodStatus = "unhealthy"
	PodStatusFlushing  PodStatus = "flushing"
)

// Pod represents a sidecar pod
type Pod struct {
	Name       string    `json:"name"`
	Address    string    `json:"address"` // e.g., "10.0.0.1:8081"
	Status     PodStatus `json:"status"`
	BufferSize int       `json:"buffer_size"`
	LastSeen   time.Time `json:"last_seen"`
}

// TestRun represents a distributed test run
type TestRun struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // "pending", "running", "completed", "failed"
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time,omitempty"`
	Pods      []string  `json:"pods"`
}

// Controller manages distributed load test runs
type Controller struct {
	pods   map[string]*Pod
	podsMu sync.RWMutex
	runs   map[string]*TestRun
	runsMu sync.RWMutex
	client *http.Client
}

// NewController creates a new orchestration controller
func NewController() *Controller {
	return &Controller{
		pods: make(map[string]*Pod),
		runs: make(map[string]*TestRun),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// RegisterPod registers a sidecar pod with the controller
func (c *Controller) RegisterPod(name, address string) {
	c.podsMu.Lock()
	defer c.podsMu.Unlock()

	c.pods[name] = &Pod{
		Name:     name,
		Address:  address,
		Status:   PodStatusUnknown,
		LastSeen: time.Now(),
	}
}

// UnregisterPod removes a pod from the controller
func (c *Controller) UnregisterPod(name string) {
	c.podsMu.Lock()
	defer c.podsMu.Unlock()
	delete(c.pods, name)
}

// GetPods returns all registered pods
func (c *Controller) GetPods() []*Pod {
	c.podsMu.RLock()
	defer c.podsMu.RUnlock()

	pods := make([]*Pod, 0, len(c.pods))
	for _, pod := range c.pods {
		pods = append(pods, pod)
	}
	return pods
}

// HealthCheckAll checks health of all registered pods
func (c *Controller) HealthCheckAll(ctx context.Context) map[string]error {
	c.podsMu.RLock()
	pods := make([]*Pod, 0, len(c.pods))
	for _, pod := range c.pods {
		pods = append(pods, pod)
	}
	c.podsMu.RUnlock()

	results := make(map[string]error)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, pod := range pods {
		wg.Add(1)
		go func(p *Pod) {
			defer wg.Done()
			err := c.healthCheck(ctx, p)

			mu.Lock()
			results[p.Name] = err
			mu.Unlock()

			c.podsMu.Lock()
			if err != nil {
				c.pods[p.Name].Status = PodStatusUnhealthy
			} else {
				c.pods[p.Name].Status = PodStatusHealthy
				c.pods[p.Name].LastSeen = time.Now()
			}
			c.podsMu.Unlock()
		}(pod)
	}

	wg.Wait()
	return results
}

// healthCheck checks health of a single pod
func (c *Controller) healthCheck(ctx context.Context, pod *Pod) error {
	url := fmt.Sprintf("http://%s/api/v1/health", pod.Address)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy status: %d", resp.StatusCode)
	}

	// Parse response to get buffer size
	var health struct {
		BufferSize int `json:"buffer_size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err == nil {
		c.podsMu.Lock()
		c.pods[pod.Name].BufferSize = health.BufferSize
		c.podsMu.Unlock()
	}

	return nil
}

// FlushAllResult holds the result of flushing all pods
type FlushAllResult struct {
	Pod      string `json:"pod"`
	Flushed  int    `json:"flushed"`
	Uploaded bool   `json:"uploaded"`
	Key      string `json:"key,omitempty"`
	Error    string `json:"error,omitempty"`
}

// FlushAll triggers flush on all registered pods
func (c *Controller) FlushAll(ctx context.Context, upload bool) []FlushAllResult {
	c.podsMu.RLock()
	pods := make([]*Pod, 0, len(c.pods))
	for _, pod := range c.pods {
		pods = append(pods, pod)
	}
	c.podsMu.RUnlock()

	results := make([]FlushAllResult, 0, len(pods))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, pod := range pods {
		wg.Add(1)
		go func(p *Pod) {
			defer wg.Done()

			c.podsMu.Lock()
			c.pods[p.Name].Status = PodStatusFlushing
			c.podsMu.Unlock()

			result := c.flushPod(ctx, p, upload)

			mu.Lock()
			results = append(results, result)
			mu.Unlock()

			c.podsMu.Lock()
			if result.Error == "" {
				c.pods[p.Name].Status = PodStatusHealthy
			} else {
				c.pods[p.Name].Status = PodStatusUnhealthy
			}
			c.podsMu.Unlock()
		}(pod)
	}

	wg.Wait()
	return results
}

// flushPod triggers flush on a single pod
func (c *Controller) flushPod(ctx context.Context, pod *Pod, upload bool) FlushAllResult {
	result := FlushAllResult{Pod: pod.Name}

	endpoint := "/api/v1/flush"
	if upload {
		endpoint = "/api/v1/flush-upload"
	}

	url := fmt.Sprintf("http://%s%s", pod.Address, endpoint)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	resp, err := c.client.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		result.Error = fmt.Sprintf("status %d: %s", resp.StatusCode, string(body))
		return result
	}

	var flushResp struct {
		Flushed  int    `json:"flushed"`
		Uploaded bool   `json:"uploaded"`
		Key      string `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&flushResp); err != nil {
		result.Error = fmt.Sprintf("decode error: %v", err)
		return result
	}

	result.Flushed = flushResp.Flushed
	result.Uploaded = flushResp.Uploaded
	result.Key = flushResp.Key

	return result
}

// StartRun creates a new test run
func (c *Controller) StartRun(id string) (*TestRun, error) {
	c.runsMu.Lock()
	defer c.runsMu.Unlock()

	if _, exists := c.runs[id]; exists {
		return nil, fmt.Errorf("run %s already exists", id)
	}

	// Get healthy pods
	c.podsMu.RLock()
	pods := make([]string, 0)
	for name, pod := range c.pods {
		if pod.Status == PodStatusHealthy {
			pods = append(pods, name)
		}
	}
	c.podsMu.RUnlock()

	run := &TestRun{
		ID:        id,
		Status:    "running",
		StartTime: time.Now(),
		Pods:      pods,
	}

	c.runs[id] = run
	return run, nil
}

// EndRun marks a test run as completed
func (c *Controller) EndRun(id string) (*TestRun, error) {
	c.runsMu.Lock()
	defer c.runsMu.Unlock()

	run, exists := c.runs[id]
	if !exists {
		return nil, fmt.Errorf("run %s not found", id)
	}

	run.Status = "completed"
	run.EndTime = time.Now()

	return run, nil
}

// GetRun returns a specific test run
func (c *Controller) GetRun(id string) (*TestRun, bool) {
	c.runsMu.RLock()
	defer c.runsMu.RUnlock()

	run, exists := c.runs[id]
	return run, exists
}

// GetRuns returns all test runs
func (c *Controller) GetRuns() []*TestRun {
	c.runsMu.RLock()
	defer c.runsMu.RUnlock()

	runs := make([]*TestRun, 0, len(c.runs))
	for _, run := range c.runs {
		runs = append(runs, run)
	}
	return runs
}

// WaitForCompletion waits for all pods to have empty buffers
func (c *Controller) WaitForCompletion(ctx context.Context, checkInterval time.Duration, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		// Health check to get buffer sizes
		c.HealthCheckAll(ctx)

		// Check if all buffers are empty
		allEmpty := true
		c.podsMu.RLock()
		for _, pod := range c.pods {
			if pod.Status == PodStatusHealthy && pod.BufferSize > 0 {
				allEmpty = false
				break
			}
		}
		c.podsMu.RUnlock()

		if allEmpty {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(checkInterval):
			continue
		}
	}

	return fmt.Errorf("timeout waiting for completion")
}

// GetStats returns aggregate statistics from all pods
func (c *Controller) GetStats(ctx context.Context) (map[string]interface{}, error) {
	c.podsMu.RLock()
	pods := make([]*Pod, 0, len(c.pods))
	for _, pod := range c.pods {
		pods = append(pods, pod)
	}
	c.podsMu.RUnlock()

	var totalRequests int64
	var totalErrors int64
	var totalBuffered int

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, pod := range pods {
		wg.Add(1)
		go func(p *Pod) {
			defer wg.Done()

			url := fmt.Sprintf("http://%s/api/v1/stats", p.Address)
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return
			}

			resp, err := c.client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			var stats struct {
				TotalRequests int64 `json:"total_requests"`
				ErrorCount    int64 `json:"error_count"`
				BufferSize    int   `json:"buffer_size"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
				return
			}

			mu.Lock()
			totalRequests += stats.TotalRequests
			totalErrors += stats.ErrorCount
			totalBuffered += stats.BufferSize
			mu.Unlock()
		}(pod)
	}

	wg.Wait()

	return map[string]interface{}{
		"total_requests": totalRequests,
		"total_errors":   totalErrors,
		"total_buffered": totalBuffered,
		"pod_count":      len(pods),
	}, nil
}
