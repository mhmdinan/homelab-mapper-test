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
        mem_total INTEGER DEFAULT 0,
        mem_used INTEGER DEFAULT 0,
        mem_percent REAL DEFAULT 0,
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
        ports TEXT
    )""")

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
        cursor.execute("""
            INSERT INTO hosts (name, url, status, last_seen, cpu_usage, mem_percent, uptime)
            VALUES (?, ?, ?, ?, ?, ?, ?)
        """, (name, url, status, now_iso, random.uniform(2, 45), random.uniform(10, 80), random.randint(3600, 1000000)))
        
        host_id = cursor.lastrowid

        if status == "online":
            # Add random number of containers to each host
            num_containers = random.randint(3, 8)
            selected_apps = random.sample(image_pool, num_containers)
            
            for img, app_name, ports in selected_apps:
                state = "running" if random.random() > 0.1 else "exited"
                container_id = os.urandom(8).hex()
                cursor.execute("""
                    INSERT INTO containers (host_id, container_id, names, image, state, status_str, ports)
                    VALUES (?, ?, ?, ?, ?, ?, ?)
                """, (host_id, container_id, "/" + app_name.lower().replace(" ", "-"), img, state, "Up 2 days" if state == "running" else "Exited (0) 5 hours ago", ports))

    conn.commit()
    conn.close()
    print("Demo data seeded successfully! Refresh your browser to see the new layout.")

if __name__ == "__main__":
    seed_data()
