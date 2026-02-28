package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetHostsEmpty(t *testing.T) {
	db := initDB()
	app := &App{DB: db}
	defer db.Close()

	req := httptest.NewRequest("GET", "/api/hosts", nil)
	rr := httptest.NewRecorder()

	app.getHostsHandler(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %v, got %v", http.StatusOK, status)
	}

	var hosts []HostWithDetails
	if err := json.NewDecoder(rr.Body).Decode(&hosts); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts, got %d", len(hosts))
	}
}
