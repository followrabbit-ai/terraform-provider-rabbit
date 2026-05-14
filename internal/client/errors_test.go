package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIError_messageFromSpring(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"timestamp":"now","status":400,"error":"Bad Request","message":"name is blank","path":"/x"}`))
	}))
	defer srv.Close()

	c, err := New(Config{Endpoint: srv.URL, HTTP: srv.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Do(context.Background(), "GET", "/x", nil, nil)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != 400 {
		t.Errorf("Status=%d want 400", apiErr.Status)
	}
	if apiErr.Message != "name is blank" {
		t.Errorf("Message=%q", apiErr.Message)
	}
	if apiErr.Code != "Bad Request" {
		t.Errorf("Code=%q", apiErr.Code)
	}
}

func TestIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := New(Config{Endpoint: srv.URL, HTTP: srv.Client()})
	err := c.Do(context.Background(), "GET", "/missing", nil, nil)
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound did not match: %v", err)
	}
}
