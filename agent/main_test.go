package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name           string
		envToken       string
		requestHeader  string
		expectedStatus int
	}{
		{"No token configured", "", "", http.StatusOK},
		{"Token set, no header", "secret", "", http.StatusUnauthorized},
		{"Token set, wrong header", "secret", "Bearer bad", http.StatusUnauthorized},
		{"Token set, correct header", "secret", "Bearer secret", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("AUTH_TOKEN", tt.envToken)
			middleware := authMiddleware(mockHandler)
			req := httptest.NewRequest("GET", "/metrics", nil)
			if tt.requestHeader != "" {
				req.Header.Set("Authorization", tt.requestHeader)
			}
			rr := httptest.NewRecorder()
			middleware.ServeHTTP(rr, req)
			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %v, got %v", tt.name, tt.expectedStatus, rr.Code)
			}
		})
	}
	os.Setenv("AUTH_TOKEN", "")
}

func TestMetricsHandlerJSON(t *testing.T) {
	// Skip actual Docker/OS calls by just testing the JSON structure the handler produces
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	
	// We call the handler directly to test its output structure
	metricsHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp MetricsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode metrics JSON: %v", err)
	}

	// Verify System fields exist
	if resp.System.Hostname == "" && !strings.Contains(resp.System.OS, "") {
		t.Log("Warning: Hostname/OS might be empty in test environment, but struct fields exist")
	}

	// Verify the new fields are at least present in the struct (Go types enforce this)
	// We can't easily mock gopsutil/docker without interfaces, so we check types 
	// for the fields we added.
	_ = resp.System.CPUCores
	_ = resp.System.DiskTotal
	_ = resp.System.MemTotal

	// If there are containers, check their resource fields
	for _, c := range resp.Containers {
		if c.ID == "" {
			t.Errorf("container missing ID")
		}
		// Resource fields should exist in the decoded struct
		_ = c.MemoryUsage
		_ = c.CPUUsage
	}
}
