package registry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRetry500ThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("GET", srv.URL, nil)

	resp, err := retryDo(client, req)
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", calls.Load())
	}
}

func TestRetry404NoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("GET", srv.URL, nil)

	resp, err := retryDo(client, req)
	if err != nil {
		t.Fatalf("expected response (not error) for 404: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 attempt for 404, got %d", calls.Load())
	}
}

func TestRetryAllAttemptsFail(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("GET", srv.URL, nil)

	resp, err := retryDo(client, req)
	if resp != nil {
		resp.Body.Close()
		t.Error("expected nil response when all attempts fail")
	}
	if err == nil {
		t.Fatal("expected error when all attempts fail")
	}
	if !strings.Contains(err.Error(), "4 attempts") {
		t.Errorf("error should mention attempt count, got: %v", err)
	}
	if calls.Load() != 4 {
		t.Errorf("expected 4 attempts (1 initial + 3 retries), got %d", calls.Load())
	}
}

func TestRetry429RateLimited(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("GET", srv.URL, nil)

	resp, err := retryDo(client, req)
	if err != nil {
		t.Fatalf("expected success after rate limit retry: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 attempts, got %d", calls.Load())
	}
}
