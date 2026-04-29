package e2e

import (
	"net/http"
	"testing"
)

// AssertStatus asserts that resp has the expected HTTP status code.
// It does not close the response body.
func AssertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Errorf("HTTP status: got %d, want %d", resp.StatusCode, want)
	}
}

// AssertOK asserts HTTP 200 OK.
func AssertOK(t *testing.T, resp *http.Response) {
	t.Helper()
	AssertStatus(t, resp, http.StatusOK)
}

// AssertAccepted asserts HTTP 202 Accepted.
func AssertAccepted(t *testing.T, resp *http.Response) {
	t.Helper()
	AssertStatus(t, resp, http.StatusAccepted)
}

// AssertBadRequest asserts HTTP 400 Bad Request.
func AssertBadRequest(t *testing.T, resp *http.Response) {
	t.Helper()
	AssertStatus(t, resp, http.StatusBadRequest)
}

// AssertNotFound asserts HTTP 404 Not Found.
func AssertNotFound(t *testing.T, resp *http.Response) {
	t.Helper()
	AssertStatus(t, resp, http.StatusNotFound)
}

// AssertInternalServerError asserts HTTP 500.
func AssertInternalServerError(t *testing.T, resp *http.Response) {
	t.Helper()
	AssertStatus(t, resp, http.StatusInternalServerError)
}
