package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Models

type Host struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Token     string    `json:"token"`
	LastSeen  time.Time `json:"last_seen"`
	Uptime    uint64    `json:"uptime"`
	CPUUsage  float64   `json:"cpu_usage"`
	MemTotal  uint64    `json:"mem_total"`
	MemUsed   uint64    `json:"mem_used"`
	MemPct    float64   `json:"mem_percent"`
	Status    string    `json:"status"` // "online" or "offline"
}

type HostWithDetails struct {
	Host
	Containers []Container `json:"containers"`
}

type Container struct {
	ID          int    `json:"-"`
	HostID      int    `json:"host_id"`
	ContainerID string `json:"container_id"`
	Names       string `json:"names"`
	Image       string `json:"image"`
	State       string `json:"state"`
	StatusStr   string `json:"status_str"`
}

// Agent Response structure
type AgentSystemInfo struct {
	Hostname string  `json:"hostname"`
	OS       string  `json:"os"`
	Platform string  `json:"platform"`
	Uptime   uint64  `json:"uptime_seconds"`
	CPUUsage float64 `json:"cpu_usage_percent"`
	MemTotal uint64  `json:"mem_total"`
	MemUsed  uint64  `json:"mem_used"`
	MemFree  uint64  `json:"mem_free"`
	MemPct   float64 `json:"mem_percent"`
}

type AgentContainerInfo struct {
	ID     string   `json:"id"`
	Names  []string `json:"names"`
	Image  string   `json:"image"`
	State  string   `json:"state"`
	Status string   `json:"status"`
}

type AgentMetricsResponse struct {
	System     AgentSystemInfo      `json:"system"`
	Containers []AgentContainerInfo `json:"containers"`
}

// App State
type App struct {
	DB *sql.DB
}

func initDB() *sql.DB {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data.db"
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}

	// Create tables
	query := `
	CREATE TABLE IF NOT EXISTS hosts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		url TEXT,
		token TEXT,
		last_seen DATETIME,
		uptime INTEGER DEFAULT 0,
		cpu_usage REAL DEFAULT 0,
		mem_total INTEGER DEFAULT 0,
		mem_used INTEGER DEFAULT 0,
		mem_percent REAL DEFAULT 0,
		status TEXT DEFAULT 'offline'
	);
	
	CREATE TABLE IF NOT EXISTS containers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER,
		container_id TEXT,
		names TEXT,
		image TEXT,
		state TEXT,
		status_str TEXT
	);
	`
	_, err = db.Exec(query)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	return db
}

func (a *App) startPoller() {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for {
			a.pollAllHosts()
			<-ticker.C
		}
	}()
}

func (a *App) pollAllHosts() {
	rows, err := a.DB.Query("SELECT id, url, token FROM hosts")
	if err != nil {
		log.Printf("Poll DB Error: %v", err)
		return
	}
	defer rows.Close()

	var wg sync.WaitGroup
	
	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	client := &http.Client{
		Transport: customTransport,
		Timeout:   10 * time.Second,
	}

	for rows.Next() {
		var id int
		var url, token string
		if err := rows.Scan(&id, &url, &token); err == nil {
			wg.Add(1)
			go a.pollHost(client, id, url, token, &wg)
		}
	}
	wg.Wait()
}

func (a *App) pollHost(client *http.Client, id int, url string, token string, wg *sync.WaitGroup) {
	defer wg.Done()
	req, err := http.NewRequest("GET", url+"/metrics", nil)
	if err != nil {
		a.updateHostStatus(id, "offline", nil)
		return
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		a.updateHostStatus(id, "offline", nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.updateHostStatus(id, "offline", nil)
		return
	}

	var metrics AgentMetricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		a.updateHostStatus(id, "offline", nil)
		return
	}

	a.updateHostStatus(id, "online", &metrics)
}

func (a *App) updateHostStatus(id int, status string, metrics *AgentMetricsResponse) {
	if status == "offline" || metrics == nil {
		a.DB.Exec("UPDATE hosts SET status = 'offline' WHERE id = ?", id)
		return
	}

	// Update host metrics
	_, err := a.DB.Exec(`
		UPDATE hosts 
		SET last_seen = ?, uptime = ?, cpu_usage = ?, mem_total = ?, mem_used = ?, mem_percent = ?, status = 'online'
		WHERE id = ?`,
		time.Now(), metrics.System.Uptime, metrics.System.CPUUsage, 
		metrics.System.MemTotal, metrics.System.MemUsed, metrics.System.MemPct, id)

	if err != nil {
		log.Printf("Error updating host %d: %v", id, err)
		return
	}

	// Update containers (simple wipe and re-insert approach for exact sync)
	a.DB.Exec("DELETE FROM containers WHERE host_id = ?", id)
	for _, c := range metrics.Containers {
		namesStr := ""
		if len(c.Names) > 0 {
			namesStr = c.Names[0]
		}
		a.DB.Exec(`INSERT INTO containers (host_id, container_id, names, image, state, status_str)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, c.ID, namesStr, c.Image, c.State, c.Status)
	}
}

// Handlers

func (a *App) getHostsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	rows, err := a.DB.Query("SELECT id, name, url, last_seen, uptime, cpu_usage, mem_total, mem_used, mem_percent, status FROM hosts")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var hosts []HostWithDetails
	for rows.Next() {
		var h HostWithDetails
		var lastSeen sql.NullTime
		err := rows.Scan(&h.ID, &h.Name, &h.URL, &lastSeen, &h.Uptime, &h.CPUUsage, &h.MemTotal, &h.MemUsed, &h.MemPct, &h.Status)
		if err == nil {
			if lastSeen.Valid {
				h.LastSeen = lastSeen.Time
			}
			hosts = append(hosts, h)
		}
	}

	// Attach containers
	for i := range hosts {
		cRows, _ := a.DB.Query("SELECT container_id, names, image, state, status_str FROM containers WHERE host_id = ?", hosts[i].ID)
		var conts []Container
		for cRows.Next() {
			var c Container
			cRows.Scan(&c.ContainerID, &c.Names, &c.Image, &c.State, &c.StatusStr)
			conts = append(conts, c)
		}
		cRows.Close()
		hosts[i].Containers = conts
	}

	if hosts == nil {
		hosts = []HostWithDetails{} // Return empty array instead of null
	}
	json.NewEncoder(w).Encode(hosts)
}

func (a *App) addHostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var input struct {
		Name  string `json:"name"`
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	res, err := a.DB.Exec("INSERT INTO hosts (name, url, token) VALUES (?, ?, ?)", input.Name, input.URL, input.Token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	id, _ := res.LastInsertId()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"id": id})
}

func (a *App) deleteHostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)
	
	a.DB.Exec("DELETE FROM containers WHERE host_id = ?", id)
	a.DB.Exec("DELETE FROM hosts WHERE id = ?", id)
	
	w.WriteHeader(http.StatusOK)
}

// Main

func main() {
	db := initDB()
	app := &App{DB: db}
	
	app.startPoller()

	http.HandleFunc("/api/hosts", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			app.getHostsHandler(w, r)
		case http.MethodPost:
			app.addHostHandler(w, r)
		case http.MethodDelete:
			app.deleteHostHandler(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Serve static files from 'public' directory
	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s...", port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
