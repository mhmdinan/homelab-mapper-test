import sqlite3
import os
import random
from datetime import datetime

# Path to the database
# Try to find the db in server/ relative to the script location
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DB_PATH = os.path.join(SCRIPT_DIR, '..', 'server', 'data.db')

def seed_data():
    print(f"Connecting to database at: {DB_PATH}")
    if not os.path.exists(DB_PATH):
        # Create a tiny shim to ensure folder exists
        os.makedirs(os.path.dirname(DB_PATH), exist_ok=True)
        # Just creating the file is not enough, open it.
    
    conn = sqlite3.connect(DB_PATH, timeout=10)
    cursor = conn.cursor()
    
    # Enable WAL mode to match server
    cursor.execute("PRAGMA journal_mode=WAL")
    cursor.execute("PRAGMA busy_timeout=5000")

    # Clear existing data for a clean test
    cursor.execute("DROP TABLE IF EXISTS containers")
    cursor.execute("DROP TABLE IF EXISTS hosts")
    
    # Re-create tables if they were dropped (to ensure schema matches main.go exactly)
    cursor.execute("""
    CREATE TABLE hosts (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT,
        url TEXT,
        token TEXT,
        last_seen DATETIME,
        uptime INTEGER DEFAULT 0,
        cpu_usage REAL DEFAULT 0,
        cpu_cores INTEGER DEFAULT 0,
        mem_total INTEGER DEFAULT 0,
        mem_used INTEGER DEFAULT 0,
        mem_percent REAL DEFAULT 0,
        disk_total INTEGER DEFAULT 0,
        disk_used INTEGER DEFAULT 0,
        status TEXT DEFAULT 'offline'
    )""")
    
    cursor.execute("""
    CREATE TABLE containers (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        host_id INTEGER,
        container_id TEXT,
        names TEXT,
        image TEXT,
        state TEXT,
        status_str TEXT,
        ports TEXT,
        memory_usage INTEGER DEFAULT 0,
        cpu_usage REAL DEFAULT 0
    )""")

    cursor.execute("CREATE TABLE IF NOT EXISTS migrations (id INTEGER PRIMARY KEY, name TEXT)")
    # Mark current migrations as applied since we seeded the exact latest schema
    cursor.execute("INSERT OR IGNORE INTO migrations (id, name) VALUES (1, 'baseline_schema')")
    cursor.execute("INSERT OR IGNORE INTO migrations (id, name) VALUES (2, 'add_host_extended_metrics')")
    cursor.execute("INSERT OR IGNORE INTO migrations (id, name) VALUES (3, 'add_container_resource_metrics')")

    # Define some demo hosts
    hosts_to_add = [
        ("Raspberry Pi 4", "https://192.168.1.10:8443", "online"),
        ("Main Server", "https://192.168.1.50:8443", "online"),
        ("Storage NAS", "https://192.168.1.20:8443", "online"),
        ("External VPS", "https://vps.example.com:8443", "offline"),
    ]

    # ... image_pool stays the same ...
    image_pool = [
        ("jellyfin/jellyfin", "Jellyfin", "8096, 8920"),
        ("homeassistant/home-assistant", "Home Assistant", "8123"),
        ("linuxserver/plex", "Plex", "32400"),
        ("pihole/pihole", "Pi-hole", "53, 80"),
        ("containrrr/watchtower", "Watchtower", "None"),
        ("nginx:latest", "Nginx Proxy", "80, 443"),
        ("portainer/portainer-ce", "Portainer", "9000, 9443"),
        ("postgres:15-alpine", "PostgreSQL", "5432"),
        ("redis:alpine", "Redis", "6379"),
        ("linuxserver/deluge", "Deluge", "8112, 58846"),
        ("grafana/grafana", "Grafana", "3000"),
        ("prom/prometheus", "Prometheus", "9090")
    ]

    for name, url, status in hosts_to_add:
        # Use ISO8601 string for compatibility
        now_iso = datetime.now().strftime("%Y-%m-%dT%H:%M:%SZ")
        
        # New randomized hardware specs
        cpu_cores = random.choice([2, 4, 8, 16])
        mem_total = random.choice([8, 16, 32, 64]) * 1024 * 1024 * 1024 # GB to Bytes
        mem_used = int(mem_total * random.uniform(0.1, 0.8))
        disk_total = random.choice([128, 256, 512, 1024]) * 1024 * 1024 * 1024 # GB to Bytes
        disk_used = int(disk_total * random.uniform(0.1, 0.7))

        cursor.execute("""
            INSERT INTO hosts (name, url, status, last_seen, cpu_usage, cpu_cores, mem_total, mem_used, mem_percent, disk_total, disk_used, uptime)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """, (name, url, status, now_iso, random.uniform(2, 45), cpu_cores, mem_total, mem_used, (mem_used/mem_total)*100, disk_total, disk_used, random.randint(3600, 1000000)))
        
        host_id = cursor.lastrowid

        if status == "online":
            # Add random number of containers to each host
            num_containers = random.randint(3, 8)
            selected_apps = random.sample(image_pool, num_containers)
            
            for img, app_name, ports in selected_apps:
                state = "running" if random.random() > 0.1 else "exited"
                container_id = os.urandom(8).hex()
                # Randomize container stats
                c_mem = random.randint(50, 800) * 1024 * 1024 if state == "running" else 0
                c_cpu = random.uniform(0.1, 5.0) if state == "running" else 0

                cursor.execute("""
                    INSERT INTO containers (host_id, container_id, names, image, state, status_str, ports, memory_usage, cpu_usage)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                """, (host_id, container_id, "/" + app_name.lower().replace(" ", "-"), img, state, "Up 2 days" if state == "running" else "Exited (0) 5 hours ago", ports, c_mem, c_cpu))

    conn.commit()
    conn.close()
    print("Demo data seeded successfully! Refresh your browser to see the new layout.")

if __name__ == "__main__":
    seed_data()
