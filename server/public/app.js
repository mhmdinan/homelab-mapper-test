const hostsGrid = document.getElementById('hosts-grid');
const addHostBtn = document.getElementById('add-host-btn');
const modal = document.getElementById('add-host-modal');
const closeBtn = document.querySelector('.close-btn');
const form = document.getElementById('add-host-form');

// --- Polling & Rendering ---
async function fetchHosts() {
    try {
        const res = await fetch('/api/hosts');
        if (!res.ok) throw new Error('API failed');
        const hosts = await res.json();
        renderHosts(hosts);
    } catch (e) {
        console.error("Failed to fetch hosts:", e);
    }
}

function renderHosts(hosts) {
    if (!hosts || hosts.length === 0) {
        hostsGrid.innerHTML = `<div style="grid-column: 1/-1; text-align: center; color: var(--text-muted); padding: 3rem;">
            No hosts added yet. Click "+ Add Host" to start monitoring your homelab!
        </div>`;
        return;
    }

    hostsGrid.innerHTML = '';
    hosts.forEach(host => {
        const card = document.createElement('div');
        card.className = 'host-card';

        const statusClass = host.status === 'online' ? 'status-online' : 'status-offline';
        const statusText = host.status === 'online' ? 'Online' : 'Offline';

        let statsHtml = '';
        if (host.status === 'online') {
            const cpu = host.cpu_usage.toFixed(1) + '%';
            const mem = host.mem_percent.toFixed(1) + '%';
            const uptime = formatUptime(host.uptime);
            statsHtml = `
            <div class="metrics-bar">
                <div class="metric">
                    <div class="metric-val">${cpu}</div>
                    <div class="metric-label">CPU</div>
                </div>
                <div class="metric">
                    <div class="metric-val">${mem}</div>
                    <div class="metric-label">RAM</div>
                </div>
                <div class="metric">
                    <div class="metric-val">${uptime}</div>
                    <div class="metric-label">UP</div>
                </div>
            </div>`;
        }

        let appsHtml = '<div class="apps-section"><h3>Deployed Docker Apps</h3><div class="app-list">';
        if (host.containers && host.containers.length > 0) {
            host.containers.forEach(c => {
                const name = c.names ? c.names.replace(/^\//, '') : c.container_id.substring(0, 8);
                const isRunning = c.state === 'running';
                appsHtml += `
                <div class="app-item ${isRunning ? 'running' : 'exited'}">
                    <div>
                        <div class="app-name">${name}</div>
                        <div class="app-status">${c.image}</div>
                    </div>
                </div>`;
            });
        } else {
            appsHtml += '<div style="color:var(--text-muted); font-size:0.85rem;">No containers found.</div>';
        }
        appsHtml += '</div></div>';

        card.innerHTML = `
            <div class="card-header">
                <h2>${host.name}</h2>
                <span class="status-badge ${statusClass}">${statusText}</span>
            </div>
            <div class="host-url">${host.url}</div>
            ${statsHtml}
            ${appsHtml}
            <button class="btn delete-btn" onclick="deleteHost(${host.id})">Remove Host</button>
        `;
        hostsGrid.appendChild(card);
    });
}

function formatUptime(seconds) {
    if (!seconds) return '0s';
    const d = Math.floor(seconds / (3600 * 24));
    const h = Math.floor(seconds % (3600 * 24) / 3600);
    if (d > 0) return `${d}d ${h}h`;
    const m = Math.floor(seconds % 3600 / 60);
    return `${h}h ${m}m`;
}

// --- Interactions ---

async function deleteHost(id) {
    if (!confirm("Are you sure you want to remove this host?")) return;
    await fetch(`/api/hosts?id=${id}`, { method: 'DELETE' });
    fetchHosts();
}

addHostBtn.addEventListener('click', () => {
    modal.classList.remove('hidden');
});

closeBtn.addEventListener('click', () => {
    modal.classList.add('hidden');
});

modal.addEventListener('click', (e) => {
    if (e.target === modal) modal.classList.add('hidden');
});

form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const name = document.getElementById('host-name').value;
    const url = document.getElementById('host-url').value;
    const token = document.getElementById('host-token').value;

    const btn = form.querySelector('button');
    btn.textContent = 'Adding...';
    btn.disabled = true;

    try {
        const res = await fetch('/api/hosts', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name, url, token })
        });
        if (res.ok) {
            form.reset();
            modal.classList.add('hidden');
            fetchHosts();
        } else {
            alert('Failed to add host');
        }
    } catch (err) {
        alert('Error adding host');
    } finally {
        btn.textContent = 'Connect Host';
        btn.disabled = false;
    }
});

// Startup
fetchHosts();
setInterval(fetchHosts, 5000);
