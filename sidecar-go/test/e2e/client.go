package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Client is a thin HTTP client for E2E test requests.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new E2E test client targeting baseURL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// Get sends a GET request to path.
func (c *Client) Get(path string) (*http.Response, error) {
	return c.httpClient.Get(c.baseURL + path)
}

// Post sends a POST request with an empty body to path.
func (c *Client) Post(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

// PostJSON marshals body as JSON and sends a POST request to path.
func (c *Client) PostJSON(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

// PostRaw sends a POST request with raw bytes to path.
func (c *Client) PostRaw(path string, contentType string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return c.httpClient.Do(req)
}

// MustGet calls Get and fatals the test on network error.
func (c *Client) MustGet(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := c.Get(path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// MustPost calls Post and fatals the test on network error.
func (c *Client) MustPost(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := c.Post(path)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// MustPostJSON calls PostJSON and fatals the test on network or marshal error.
func (c *Client) MustPostJSON(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	resp, err := c.PostJSON(path, body)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// DecodeJSON decodes the response body as JSON into v.
// The caller is responsible for closing resp.Body.
func DecodeJSON[T any](t *testing.T, resp *http.Response, v *T) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}
