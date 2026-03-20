package registry

import (
	"fmt"
	"net/http"
	"time"
)

const (
	maxRetries    = 3
	retryBaseWait = 500 * time.Millisecond
)

// retryDo executes an HTTP request with retry logic for transient errors.
// Retries on 5xx, 429, and network errors. Does NOT retry on 4xx (those are definitive).
func retryDo(client *http.Client, req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := retryBaseWait * time.Duration(1<<(attempt-1)) // exponential backoff
			time.Sleep(wait)
		}

		// Clone the request body if present (can't re-read after first attempt)
		var reqCopy *http.Request
		if req.Body == nil || req.Method == "GET" || req.Method == "HEAD" {
			reqCopy = req
		} else {
			// For non-GET with body, we can't retry safely
			return client.Do(req)
		}

		resp, err := client.Do(reqCopy)
		if err != nil {
			lastErr = err
			continue // network error, retry
		}

		// Retry on server errors and rate limiting
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}
	return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, lastErr)
}
