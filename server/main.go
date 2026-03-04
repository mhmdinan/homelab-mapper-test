package main

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
	"strings"

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
	CPUCores  int       `json:"cpu_cores"`
	MemTotal  uint64    `json:"mem_total"`
	MemUsed   uint64    `json:"mem_used"`
	MemPct    float64   `json:"mem_percent"`
	DiskTotal uint64    `json:"disk_total"`
	DiskUsed  uint64    `json:"disk_used"`
	Status    string    `json:"status"` // "online" or "offline"
}

type HostWithDetails struct {
	Host
	Containers []Container `json:"containers"`
}

type Container struct {
	ID          int     `json:"-"`
	HostID      int     `json:"host_id"`
	ContainerID string  `json:"container_id"`
	Names       string  `json:"names"`
	Image       string  `json:"image"`
	State       string  `json:"state"`
	StatusStr   string  `json:"status_str"`
	Ports       string  `json:"ports"`
	MemoryUsage uint64  `json:"memory_usage"`
	CPUUsage    float64 `json:"cpu_usage"`
}

// Agent Response structure
type AgentSystemInfo struct {
	Hostname string  `json:"hostname"`
	OS       string  `json:"os"`
	Platform string  `json:"platform"`
	Uptime   uint64  `json:"uptime_seconds"`
	CPUUsage float64 `json:"cpu_usage_percent"`
	CPUCores int     `json:"cpu_cores"`
	MemTotal uint64  `json:"mem_total"`
	MemUsed  uint64  `json:"mem_used"`
	MemFree  uint64  `json:"mem_free"`
	MemPct   float64 `json:"mem_percent"`
	DiskTotal uint64 `json:"disk_total"`
	DiskUsed  uint64 `json:"disk_used"`
}

type AgentContainerInfo struct {
	ID          string   `json:"id"`
	Names       []string `json:"names"`
	Image       string   `json:"image"`
	State       string   `json:"state"`
	Status      string   `json:"status"`
	Ports       []string `json:"ports"`
	MemoryUsage uint64   `json:"memory_usage"`
	CPUUsage    float64  `json:"cpu_usage"`
}

type AgentMetricsResponse struct {
	System     AgentSystemInfo      `json:"system"`
	Containers []AgentContainerInfo `json:"containers"`
}

// App State
type App struct {
	DB *sql.DB
	mu sync.Mutex // Protects concurrent DB writes
}

func runMigrations(db *sql.DB) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS migrations (id INTEGER PRIMARY KEY, name TEXT)`)
	if err != nil {
		log.Fatalf("Migration system failed: %v", err)
	}

	steps := []struct {
		id   int
		name string
		sql  string
	}{
		{1, "baseline_schema", `
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
				status_str TEXT,
				ports TEXT
			);
		`},
		{2, "add_host_extended_metrics", `
			ALTER TABLE hosts ADD COLUMN cpu_cores INTEGER DEFAULT 0;
			ALTER TABLE hosts ADD COLUMN disk_total INTEGER DEFAULT 0;
			ALTER TABLE hosts ADD COLUMN disk_used INTEGER DEFAULT 0;
		`},
		{3, "add_container_resource_metrics", `
			ALTER TABLE containers ADD COLUMN memory_usage INTEGER DEFAULT 0;
			ALTER TABLE containers ADD COLUMN cpu_usage REAL DEFAULT 0;
		`},
	}

	for _, step := range steps {
		var exists int
		db.QueryRow("SELECT COUNT(*) FROM migrations WHERE id = ?", step.id).Scan(&exists)
		if exists == 0 {
			log.Printf("Checking migration %d: %s...", step.id, step.name)
			_, err := db.Exec(step.sql)
			if err != nil {
				// Ignore errors about columns/tables already existing
				errMsg := err.Error()
				if strings.Contains(errMsg, "duplicate column name") || strings.Contains(errMsg, "already exists") {
					log.Printf("Migration %d already partially applied (checked by SQL error). Skipping...", step.id)
				} else {
					log.Fatalf("Migration %d failed: %v", step.id, err)
				}
			}
			db.Exec("INSERT INTO migrations (id, name) VALUES (?, ?)", step.id, step.name)
		}
	}
}

func initDB() *sql.DB {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data.db"
	}
	
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}

	runMigrations(db)

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
	a.mu.Lock()
	defer a.mu.Unlock()

	if status == "offline" || metrics == nil {
		a.DB.Exec("UPDATE hosts SET status = 'offline' WHERE id = ?", id)
		return
	}

	// Update host metrics
	_, err := a.DB.Exec(`
		UPDATE hosts 
		SET last_seen = ?, uptime = ?, cpu_usage = ?, cpu_cores = ?, mem_total = ?, mem_used = ?, mem_percent = ?, 
		    disk_total = ?, disk_used = ?, status = 'online'
		WHERE id = ?`,
		time.Now(), metrics.System.Uptime, metrics.System.CPUUsage, metrics.System.CPUCores,
		metrics.System.MemTotal, metrics.System.MemUsed, metrics.System.MemPct, 
		metrics.System.DiskTotal, metrics.System.DiskUsed, id)

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
		portsStr := strings.Join(c.Ports, ", ")
		a.DB.Exec(`INSERT INTO containers (host_id, container_id, names, image, state, status_str, ports, memory_usage, cpu_usage)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, c.ID, namesStr, c.Image, c.State, c.Status, portsStr, c.MemoryUsage, c.CPUUsage)
	}
}

// Handlers

func (a *App) getHostsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	rows, err := a.DB.Query("SELECT id, name, url, last_seen, uptime, cpu_usage, cpu_cores, mem_total, mem_used, mem_percent, disk_total, disk_used, status FROM hosts")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var hosts []HostWithDetails
	for rows.Next() {
		var h HostWithDetails
		var lastSeen sql.NullTime
		err := rows.Scan(&h.ID, &h.Name, &h.URL, &lastSeen, &h.Uptime, &h.CPUUsage, &h.CPUCores, &h.MemTotal, &h.MemUsed, &h.MemPct, &h.DiskTotal, &h.DiskUsed, &h.Status)
		if err == nil {
			if lastSeen.Valid {
				h.LastSeen = lastSeen.Time
			}
			hosts = append(hosts, h)
		}
	}

	// Attach containers
	for i := range hosts {
		cRows, _ := a.DB.Query("SELECT container_id, names, image, state, status_str, ports, memory_usage, cpu_usage FROM containers WHERE host_id = ?", hosts[i].ID)
		var conts []Container
		for cRows.Next() {
			var c Container
			cRows.Scan(&c.ContainerID, &c.Names, &c.Image, &c.State, &c.StatusStr, &c.Ports, &c.MemoryUsage, &c.CPUUsage)
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

	a.mu.Lock()
	res, err := a.DB.Exec("INSERT INTO hosts (name, url, token) VALUES (?, ?, ?)", input.Name, input.URL, input.Token)
	a.mu.Unlock()
	
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
	
	a.mu.Lock()
	a.DB.Exec("DELETE FROM containers WHERE host_id = ?", id)
	a.DB.Exec("DELETE FROM hosts WHERE id = ?", id)
	a.mu.Unlock()
	
	w.WriteHeader(http.StatusOK)
}

// Main

func main() {
	db := initDB()
	app := &App{DB: db}
	
	app.startPoller()

	http.HandleFunc("/api/hosts", func(w http.ResponseWriter, r *http.Request) {
		// Discourage browsers from forcing HTTPS on this local dev port
		w.Header().Set("Strict-Transport-Security", "max-age=0")
		
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
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=0")
		fs.ServeHTTP(w, r)
	}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on HTTP port %s (UI access)...", port)
	log.Printf("Secure agent polling is active (InsecureSkipVerify enabled for agents).")
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
