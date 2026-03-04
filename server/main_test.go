package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMigrations(t *testing.T) {
	// Use a temporary DB for testing migrations
	dbPath := "test_migrations.db"
	os.Setenv("DB_PATH", dbPath)
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	db := initDB()
	defer db.Close()

	// 1. Verify migrations table exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM migrations").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query migrations table: %v", err)
	}
	if count < 3 {
		t.Errorf("expected at least 3 migrations applied, got %d", count)
	}

	// 2. Test Idempotency: Running initDB again should not fail
	db2 := initDB()
	db2.Close()
}

func TestUpdateHostStatusAndGetJSON(t *testing.T) {
	dbPath := "test_metrics.db"
	os.Setenv("DB_PATH", dbPath)
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	db := initDB()
	app := &App{DB: db}
	defer db.Close()

	// 1. Insert a mock host
	res, _ := db.Exec("INSERT INTO hosts (name, url, token) VALUES (?, ?, ?)", "Test Host", "http://localhost", "tok")
	hostID, _ := res.LastInsertId()

	// 2. Mock an agent response with new metrics
	mockResp := &AgentMetricsResponse{
		System: AgentSystemInfo{
			CPUCores:  8,
			MemTotal:  16 * 1024 * 1024 * 1024,
			DiskTotal: 500 * 1024 * 1024 * 1024,
			Uptime:    3600,
		},
		Containers: []AgentContainerInfo{
			{ID: "cont123", Names: []string{"/web"}, State: "running", MemoryUsage: 256 * 1024 * 1024},
		},
	}

	app.updateHostStatus(int(hostID), "online", mockResp)

	// 3. Verify via API handler
	req := httptest.NewRequest("GET", "/api/hosts", nil)
	rr := httptest.NewRecorder()
	app.getHostsHandler(rr, req)

	var hosts []HostWithDetails
	json.NewDecoder(rr.Body).Decode(&hosts)

	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}

	h := hosts[0]
	if h.CPUCores != 8 || h.DiskTotal == 0 {
		t.Errorf("Host metrics not persisted correctly: %+v", h)
	}

	if len(h.Containers) != 1 || h.Containers[0].MemoryUsage == 0 {
		t.Errorf("Container metrics not persisted correctly: %+v", h.Containers)
	}
}
