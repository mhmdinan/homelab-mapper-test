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
        lastHostsData = hosts;

        if (currentView === 'grid') {
            renderHosts(hosts);
        } else {
            renderDiagram(hosts);
        }
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

// --- Diagram & View Toggle ---
const viewToggleBtn = document.getElementById('view-toggle-btn');
const diagramContainer = document.getElementById('diagram-container');
let currentView = 'grid'; // 'grid' | 'diagram'
let network = null;
let lastHostsData = [];

viewToggleBtn.addEventListener('click', () => {
    if (currentView === 'grid') {
        currentView = 'diagram';
        hostsGrid.classList.add('hidden');
        diagramContainer.classList.remove('hidden');
        viewToggleBtn.textContent = 'Grid View';
        renderDiagram(lastHostsData);
    } else {
        currentView = 'grid';
        diagramContainer.classList.add('hidden');
        hostsGrid.classList.remove('hidden');
        viewToggleBtn.textContent = 'Diagram View';
        renderHosts(lastHostsData);
    }
});

// Helper to guess icon from github.com/walkxcode/dashboard-icons
function getAppIcon(imageStr) {
    if (!imageStr) return 'https://cdn.jsdelivr.net/gh/walkxcode/dashboard-icons/png/docker.png';

    // Example: "linuxserver/jellyfin:latest" -> "jellyfin"
    // Example: "ghcr.io/home-assistant/home-assistant:stable" -> "home-assistant"
    let clean = imageStr.split(':')[0]; // remove tag
    let parts = clean.split('/');
    let appName = parts[parts.length - 1]; // get the last part

    // Handle common names that might differ slightly in the icon repo
    appName = appName.toLowerCase()
        .replace('-web', '')
        .replace('-app', '');

    return `https://cdn.jsdelivr.net/gh/walkxcode/dashboard-icons/png/${appName}.png`;
}

function renderDiagram(hosts) {
    if (!diagramContainer) return;

    let nodes = new vis.DataSet([]);
    let edges = new vis.DataSet([]);

    // Central Node (The Server/Router itself)
    nodes.add({
        id: 'server',
        label: 'Homelab Mapper\n(Server)',
        shape: 'image',
        image: 'https://cdn.jsdelivr.net/gh/walkxcode/dashboard-icons/png/router.png',
        size: 40,
        font: { color: '#c9d1d9', size: 16, bold: true }
    });

    hosts.forEach(host => {
        // Add Host Node
        const hostId = `host_${host.id}`;
        const isOnline = host.status === 'online';
        nodes.add({
            id: hostId,
            label: `${host.name}\n${host.url}`,
            shape: 'image',
            image: isOnline ? 'https://cdn.jsdelivr.net/gh/walkxcode/dashboard-icons/png/server.png' : 'https://cdn.jsdelivr.net/gh/walkxcode/dashboard-icons/png/server-error.png',
            size: 30,
            font: { color: isOnline ? '#3fb950' : '#ff7b72', size: 14 }
        });

        // Edge from Server to Host
        edges.add({
            from: 'server',
            to: hostId,
            length: 150,
            color: { color: isOnline ? 'rgba(35, 134, 54, 0.5)' : 'rgba(218, 54, 51, 0.5)' },
            dashes: !isOnline
        });

        // Add App Nodes for this Host
        if (host.containers && host.containers.length > 0) {
            host.containers.forEach(c => {
                const containerId = `app_${c.container_id}`;
                const appTitle = c.names ? c.names.replace(/^\//, '') : c.container_id.substring(0, 8);
                const isRunning = c.state === 'running';

                nodes.add({
                    id: containerId,
                    label: appTitle,
                    shape: 'image',
                    image: getAppIcon(c.image),
                    brokenImage: 'https://cdn.jsdelivr.net/gh/walkxcode/dashboard-icons/png/docker.png', // Fallback if no icon exists
                    size: 20,
                    font: { color: isRunning ? '#c9d1d9' : '#8b949e', size: 12 }
                });

                edges.add({
                    from: hostId,
                    to: containerId,
                    length: 100,
                    color: { color: isRunning ? 'rgba(88, 166, 255, 0.4)' : 'rgba(139, 148, 158, 0.4)' },
                    dashes: !isRunning
                });
            });
        }
    });

    const data = { nodes, edges };
    const options = {
        interaction: { hover: true },
        physics: {
            solver: 'forceAtlas2Based',
            forceAtlas2Based: {
                gravitationalConstant: -100,
                centralGravity: 0.01,
                springLength: 100,
                springConstant: 0.08
            }
        }
    };

    if (network) {
        // Destroy old network instance to prevent memory leaks if re-rendering completely
        network.destroy();
    }
    network = new vis.Network(diagramContainer, data, options);
}

// Startup
fetchHosts();
setInterval(fetchHosts, 5000);
