
"use strict";

// ==========================================
// 1. CONFIGURATION
// ==========================================

// Temporary URL for local development.
// Later, we will connect through Nginx using "/api/metrics".
const API_URL = "http://localhost:5000/metrics";

const AUTO_REFRESH_INTERVAL_MS = 30000; // 30 seconds


// ==========================================
// 2. FIND HTML ELEMENTS
// ==========================================

// Server information
const hostnameElement        = document.getElementById("hostname");
const operatingSystemElement = document.getElementById("operating-system");
const architectureElement    = document.getElementById("architecture");
const webServerElement       = document.getElementById("web-server");

// CPU information
const cpuLoad1Element  = document.getElementById("cpu-load-1");
const cpuLoad5Element  = document.getElementById("cpu-load-5");
const cpuLoad15Element = document.getElementById("cpu-load-15");
const cpuCoresElement  = document.getElementById("cpu-cores");

// Memory information
const memoryUsageElement       = document.getElementById("memory-usage");
const memoryBarElement         = document.getElementById("memory-bar");
const memoryProgressElement    = document.getElementById("memory-progress");
const memoryDescriptionElement = document.getElementById("memory-description");
const memoryTotalElement       = document.getElementById("memory-total");
const memoryAvailableElement   = document.getElementById("memory-available");
const applicationMemoryElement = document.getElementById("application-memory");

// Uptime and client information
const uptimeElement   = document.getElementById("system-uptime");
const clientIpElement = document.getElementById("client-ip");

// Status and interface elements
const connectionStatusElement = document.getElementById("connection-status");
const statusIndicatorElement  = document.getElementById("status-indicator");
const refreshButton           = document.getElementById("refresh-button");
const errorMessageElement     = document.getElementById("error-message");
const lastUpdatedElement      = document.getElementById("last-updated");
const footerStatusElement     = document.getElementById("footer-status");


// ==========================================
// 3. INITIALISE LUCIDE ICONS
// ==========================================

if (typeof lucide !== "undefined") {
    lucide.createIcons();
}


// ==========================================
// 4. CPU SPARKLINE CHART
// ==========================================

const CPU_HISTORY_MAX = 20;
const cpuHistory = [];

let cpuChart = null;

function initCpuChart() {
    const canvas = document.getElementById("cpu-chart");
    if (!canvas || typeof Chart === "undefined") return;

    const ctx = canvas.getContext("2d");

    cpuChart = new Chart(ctx, {
        type: "line",
        data: {
            labels: Array(CPU_HISTORY_MAX).fill(""),
            datasets: [{
                data: Array(CPU_HISTORY_MAX).fill(null),
                borderColor: "#3b82f6",
                borderWidth: 2,
                backgroundColor: "rgba(59, 130, 246, 0.12)",
                fill: true,
                tension: 0.4,
                pointRadius: 0,
                pointHoverRadius: 3,
            }],
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            animation: { duration: 400 },
            plugins: { legend: { display: false }, tooltip: { enabled: false } },
            scales: {
                x: { display: false },
                y: {
                    display: false,
                    min: 0,
                    suggestedMax: 2,
                },
            },
        },
    });
}

function updateCpuChart(value) {
    if (!cpuChart) return;

    cpuHistory.push(value);
    if (cpuHistory.length > CPU_HISTORY_MAX) {
        cpuHistory.shift();
    }

    // Pad with nulls if fewer than max readings yet
    const padded = [
        ...Array(CPU_HISTORY_MAX - cpuHistory.length).fill(null),
        ...cpuHistory,
    ];

    cpuChart.data.datasets[0].data = padded;
    cpuChart.update("none"); // skip animation for live updates
}


// ==========================================
// 5. HELPER: FORMAT UPTIME
// ==========================================

function formatUptime(totalSeconds) {
    const seconds = Math.floor(totalSeconds);
    const days    = Math.floor(seconds / 86400);
    const hours   = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    return `${days}d ${hours}h ${minutes}m`;
}


// ==========================================
// 6. HELPER: MEMORY BAR COLOR
// ==========================================

function memoryBarColor(percent) {
    if (percent > 85) return "#ef4444"; // red
    if (percent > 60) return "#f59e0b"; // orange
    return "#10b981";                   // green
}


// ==========================================
// 7. MAIN FUNCTION: LOAD BACKEND METRICS
// ==========================================

async function loadMetrics() {
    // Prepare the interface
    connectionStatusElement.textContent = "Connecting...";
    statusIndicatorElement.classList.remove("online", "offline");
    errorMessageElement.hidden = true;
    refreshButton.disabled = true;

    try {
        // Request metrics from the Go backend
        const response = await fetch(API_URL);

        // Check the HTTP response status
        if (!response.ok) {
            throw new Error(`Backend returned HTTP ${response.status}`);
        }

        // Convert the response JSON into a JavaScript object
        const data = await response.json();

        // --------------------------------------
        // 7A. SERVER INFORMATION
        // --------------------------------------

        hostnameElement.textContent        = data.server.hostname;
        operatingSystemElement.textContent = data.server.os;
        architectureElement.textContent    = data.server.architecture;

        // This header will be provided by Nginx later.
        const respondingWebServer = response.headers.get("X-Web-Server");
        webServerElement.textContent = respondingWebServer || "Not configured";

        // --------------------------------------
        // 7B. CPU INFORMATION
        // --------------------------------------

        const cpuLoad1 = Number(data.cpu.loadAverage1);
        cpuLoad1Element.textContent  = cpuLoad1.toFixed(2);
        cpuLoad5Element.textContent  = Number(data.cpu.loadAverage5).toFixed(2);
        cpuLoad15Element.textContent = Number(data.cpu.loadAverage15).toFixed(2);
        cpuCoresElement.textContent  = data.cpu.cores;

        updateCpuChart(cpuLoad1);

        // --------------------------------------
        // 7C. MEMORY INFORMATION
        // --------------------------------------

        const memoryUsage     = Number(data.memory.usagePercent);
        const safeMemoryUsage = Math.max(0, Math.min(100, memoryUsage));

        memoryUsageElement.textContent = `${memoryUsage.toFixed(1)}%`;

        memoryBarElement.style.width      = `${safeMemoryUsage}%`;
        memoryBarElement.style.background = memoryBarColor(safeMemoryUsage);

        memoryProgressElement.setAttribute("aria-valuenow", safeMemoryUsage);

        memoryDescriptionElement.textContent =
            `${data.memory.systemAvailableMB} MB available`;

        memoryTotalElement.textContent =
            `${data.memory.systemTotalMB} MB`;

        memoryAvailableElement.textContent =
            `${data.memory.systemAvailableMB} MB`;

        applicationMemoryElement.textContent =
            `${data.memory.appUsedMB} MB`;

        // --------------------------------------
        // 7D. UPTIME AND CLIENT IP
        // --------------------------------------

        uptimeElement.textContent =
            formatUptime(data.server.uptimeSeconds);

        clientIpElement.textContent = data.clientIP;

        // --------------------------------------
        // 7E. CONNECTION STATUS
        // --------------------------------------

        connectionStatusElement.textContent = "Backend connected";
        statusIndicatorElement.classList.add("online");

        lastUpdatedElement.textContent =
            `Last updated: ${new Date().toLocaleTimeString()}`;

        footerStatusElement.textContent = "Metrics loaded successfully";

    } catch (error) {
        // Handle request or response errors
        connectionStatusElement.textContent = "Backend unavailable";
        statusIndicatorElement.classList.add("offline");

        errorMessageElement.textContent =
            `Could not load metrics: ${error.message}`;

        errorMessageElement.hidden = false;

        footerStatusElement.textContent = "Unable to retrieve metrics";

        console.error("Failed to load metrics:", error);

    } finally {
        // Enable the Refresh button again
        refreshButton.disabled = false;
    }
}


// ==========================================
// 8. REFRESH BUTTON
// ==========================================

refreshButton.addEventListener("click", loadMetrics);


// ==========================================
// 9. INITIAL PAGE LOAD + AUTO-REFRESH
// ==========================================

initCpuChart();
loadMetrics();

setInterval(loadMetrics, AUTO_REFRESH_INTERVAL_MS);

