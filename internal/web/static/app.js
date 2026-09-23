/**
 * tDocs Modern Frontend Engine
 * Zero-Build, High-Performance, Accessible
 */

// --- Global State ---
let currentFolderId = null;
let currentFolderPath = [];
let cachedFolders = [];
let cachedFiles = [];
let currentView = localStorage.getItem("tdocs_view") || localStorage.getItem("robdocs_view") || localStorage.getItem("teledrive_view") || "grid";
let currentSort = { field: "name", order: "asc" };
let currentTab = "overview";
let searchDebounceTimer = null;
let activeSearchQuery = "";
let selectedIds = new Set();
let favoriteIds = new Set();
let folderStats = {};
let ovRecentLimit = 8;
let uploadSeq = 0;

// --- Authenticated API helper (CSRF + session expiry handling) ---
function csrfToken() {
    const m = document.cookie.match(/(?:^|; )tdocs_csrf=([^;]*)/);
    return m ? decodeURIComponent(m[1]) : "";
}

async function apiFetch(url, opts = {}) {
    opts.credentials = opts.credentials || "same-origin";
    opts.headers = Object.assign({}, opts.headers || {});
    const method = (opts.method || "GET").toUpperCase();
    if (["POST", "PUT", "DELETE", "PATCH"].includes(method)) {
        opts.headers["X-CSRF-Token"] = csrfToken();
    }
    const res = await window.fetch(url, opts);
    if (res.status === 401 && !url.includes("/login")) {
        window.location.href = "/login";
        throw new Error("Session expired, please log in again");
    }
    return res;
}

// Upload Manager State
let uploadQueue = [];
let isUploading = false;
let uploadStats = { total: 0, completed: 0, failed: 0 };
let isDrawerMinimized = false;

// --- SVG Icons Map (Lucide-based) ---
const ICONS = {
    folder: `<path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z"/>`,
    "folder-plus": `<path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z"/><line x1="12" y1="10" x2="12" y2="16"/><line x1="9" y1="13" x2="15" y2="13"/>`,
    file: `<path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"/><polyline points="14 2 14 8 20 8"/>`,
    "file-text": `<path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><line x1="10" y1="9" x2="8" y2="9"/>`,
    film: `<rect width="20" height="20" x="2" y="2" rx="2.18" ry="2.18"/><line x1="7" y1="2" x2="7" y2="22"/><line x1="17" y1="2" x2="17" y2="22"/><line x1="2" y1="12" x2="22" y2="12"/><line x1="2" y1="7" x2="7" y2="7"/><line x1="2" y1="17" x2="7" y2="17"/><line x1="17" y1="17" x2="22" y2="17"/><line x1="17" y1="7" x2="22" y2="7"/>`,
    music: `<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>`,
    image: `<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/>`,
    download: `<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>`,
    upload: `<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/>`,
    "share-2": `<circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/>`,
    "trash-2": `<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/><line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/>`,
    edit: `<path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/><path d="m15 5 4 4"/>`,
    sun: `<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>`,
    moon: `<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>`,
    search: `<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`,
    x: `<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`,
    check: `<polyline points="20 6 9 17 4 12"/>`,
    "alert-circle": `<circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>`,
    info: `<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>`,
    "grid": `<rect width="7" height="7" x="3" y="3" rx="1"/><rect width="7" height="7" x="14" y="3" rx="1"/><rect width="7" height="7" x="14" y="14" rx="1"/><rect width="7" height="7" x="3" y="14" rx="1"/>`,
    "list": `<line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/>`,
    "chevron-right": `<polyline points="9 18 15 12 9 6"/>`,
    "chevron-down": `<polyline points="6 9 12 15 18 9"/>`,
    "chevron-up": `<polyline points="18 15 12 9 6 15"/>`,
    "more-vertical": `<circle cx="12" cy="12" r="1"/><circle cx="12" cy="5" r="1"/><circle cx="12" cy="19" r="1"/>`,
    copy: `<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>`,
    "qr-code": `<rect width="5" height="5" x="3" y="3" rx="1"/><rect width="5" height="5" x="16" y="3" rx="1"/><rect width="5" height="5" x="3" y="16" rx="1"/><path d="M21 16h-3a2 2 0 0 0-2 2v3"/><path d="M21 21v.01"/><path d="M12 7v3a2 2 0 0 1-2 2H7"/><path d="M3 12h.01"/><path d="M12 3h.01"/><path d="M12 16v.01"/><path d="M16 12h1"/><path d="M21 12v.01"/><path d="M12 21v-1"/>`,
    lock: `<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`,
    menu: `<line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="18" x2="20" y2="18"/>`,
    zap: `<polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>`,
    shield: `<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>`,
    database: `<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/><path d="M3 12c0 1.66 4 3 9 3s9-1.34 9-3"/>`,
    "rotate-ccw": `<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/>`,
    "help-circle": `<circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/>`,
    star: `<polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>`,
    settings: `<circle cx="12" cy="12" r="3"/><path d="M12 1v4M12 19v4M4.2 4.2l2.8 2.8M17 17l2.8 2.8M1 12h4M19 12h4M4.2 19.8 7 17M17 7l2.8-2.8"/>`,
    "refresh-cw": `<path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"/><path d="M16 21h5v-5"/>`,
    bell: `<path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/>`,
    user: `<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>`,
    play: `<polygon points="6 3 20 12 6 21 6 3"/>`,
    pause: `<rect width="4" height="16" x="6" y="4"/><rect width="4" height="16" x="14" y="4"/>`,
    eye: `<path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z"/><circle cx="12" cy="12" r="3"/>`,
    clock: `<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>`,
    activity: `<path d="M22 12h-4l-3 9L9 3l-3 9H2"/>`,
    "skip-back": `<polygon points="19 20 9 12 19 4 19 20"/><line x1="5" y1="19" x2="5" y2="5"/>`,
    "skip-forward": `<polygon points="5 4 15 12 5 20 5 4"/><line x1="19" y1="5" x2="19" y2="19"/>`,
    "picture-in-picture": `<rect width="20" height="14" x="2" y="3" rx="2"/><rect width="7" height="5" x="12" y="12" rx="1"/>`,
    maximize: `<path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/>`,
    "zoom-in": `<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/><path d="M11 8v6M8 11h6"/>`,
    "chevron-left": `<polyline points="15 18 9 12 15 6"/>`,
    shuffle: `<polyline points="16 3 21 3 21 8"/><line x1="4" y1="20" x2="21" y2="3"/><polyline points="21 16 21 21 16 21"/><line x1="15" y1="15" x2="21" y2="21"/><line x1="4" y1="4" x2="9" y2="9"/>`,
    repeat: `<polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/>`,
    "repeat-1": `<polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/><text x="12" y="15" font-size="8" text-anchor="middle" fill="currentColor" stroke="none">1</text>`,
    volume: `<polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/>`,
    "volume-x": `<polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><line x1="23" y1="9" x2="17" y2="15"/><line x1="17" y1="9" x2="23" y2="15"/>`
};

function getIcon(name, extraClasses = "") {
    const path = ICONS[name] || ICONS.file;
    return `<svg class="icon ${extraClasses}" viewBox="0 0 24 24">${path}</svg>`;
}

// --- Initialization ---
document.addEventListener("DOMContentLoaded", () => {
    initTheme();
    setupDropzone();
    setupSearch();
    setupMobileSidebar();
    setupContextMenu();
    setupShortcuts();
    initSidebarCollapse();
    MediaEngine.init();
    loadOverview();
    updateViewButtons();
    refreshStatus();
    refreshNotifications();
    checkTelegramSetup();
    refreshTelegramState(false);
    checkForUpdates();
    setInterval(checkTelegramSetup, 20000);
    setInterval(refreshStatus, 15000);
    setInterval(refreshNotifications, 30000);
    document.addEventListener("click", () => {
        closeNotifDropdown();
        closeAvatarMenu();
        hideContextMenu();
    });
});

// --- Telegram storage state -------------------------------------------------
// The dashboard only ever shows CONNECTED when the backend verified it with a
// real network check. Configuration values on their own are never enough.
const TG_STATE = {
    not_configured: { text: "Not configured", short: "Not Configured", color: "var(--text-muted)", live: false },
    authentication_required: { text: "Authentication required", short: "Auth Required", color: "var(--warning)", live: false },
    connecting: { text: "Connecting…", short: "Connecting", color: "var(--primary)", live: false },
    connected: { text: "Connected", short: "Connected", color: "var(--success)", live: true },
    disconnected: { text: "Disconnected", short: "Disconnected", color: "var(--warning)", live: false },
    error: { text: "Error", short: "Error", color: "var(--danger)", live: false },
};

function tgStateInfo(state) {
    return TG_STATE[state] || TG_STATE.error;
}

// --- Live Status Widget ---
async function refreshStatus() {
    const dot = document.getElementById("status-dot");
    const label = document.getElementById("status-label");
    const channel = document.getElementById("status-channel");
    const files = document.getElementById("status-files");
    if (!label) return;

    try {
        const res = await apiFetch("/api/status");
        if (!res.ok) throw new Error("status " + res.status);
        const s = await res.json();

        const state = s.telegram_state || "not_configured";
        const info = tgStateInfo(state);
        label.innerText = "Telegram: " + info.text;
        if (dot) {
            dot.style.background = info.color;
            dot.classList.toggle("live", !!info.live);
        }
        if (channel) {
            channel.innerText = info.live ? "Connected" : (s.storage_channel ? "Bound (unverified)" : "Not bound");
        }
        if (files) files.innerText = typeof s.files === "number" ? s.files : "0";
    } catch (err) {
        if (label) label.innerText = "Server unreachable";
        if (dot) { dot.style.background = "var(--danger)"; dot.classList.remove("live"); }
    }
}

// Honest placeholder for panels whose data lives in the Telegram Storage
// Channel (snapshots, sync history) when the backend cannot serve them.
async function renderTelegramUnavailable(tbodyId, body) {
    const tbody = document.getElementById(tbodyId);
    const emptyState = document.getElementById("empty-snapshots-state");
    if (emptyState) emptyState.style.display = "none";
    if (!tbody) return;
    const info = tgStateInfo((body && body.state) || "error");
    const rows = tbody.closest("table") ? tbody.closest("table").querySelectorAll("thead th").length || 1 : 1;
    tbody.innerHTML = `
        <tr><td colspan="${rows}" style="padding: 26px 18px;">
            <div class="tg-unavailable">
                <span class="tg-state-dot" style="background: ${info.color};"></span>
                <div>
                    <div class="tg-unavailable-title">Telegram Storage — ${escapeHtml(info.short)}</div>
                    <p class="tg-unavailable-text">${escapeHtml((body && body.message) || "The storage backend could not be reached.")}</p>
                    <p class="tg-unavailable-hint">Snapshots and sync history live in your Telegram Storage Channel, so they are unavailable until the backend is connected.</p>
                </div>
            </div>
        </td></tr>`;
}

// Explicit, real backend test — never reports success on configuration alone.
async function testTelegramConnection() {
    const btn = document.getElementById("btn-test-connection");
    const label = document.getElementById("test-conn-text");
    const box = document.getElementById("verify-results");
    if (btn) btn.disabled = true;
    if (label) label.innerText = "Testing…";
    try {
        const res = await apiFetch("/api/telegram/test", { method: "POST" });
        const data = await res.json().catch(() => ({}));
        const info = tgStateInfo(data.state);
        const checks = data.checks || {};
        const row = (ok, text) => `<li style="display:flex;gap:8px;align-items:center;">` +
            `<span class="status-dot" style="background:${ok ? "var(--success)" : "var(--text-muted)"};animation:none;"></span>${escapeHtml(text)}</li>`;
        if (box) {
            box.innerHTML = `
                <div class="card" style="cursor:default;padding:18px;">
                    <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;">
                        <strong>${escapeHtml(info.short)}</strong>
                        <span class="tg-state-pill tg-${escapeHtml(data.state || "error")}">${escapeHtml(info.short)}</span>
                    </div>
                    <p style="color:var(--text-secondary);font-size:0.85rem;margin-top:6px;">${escapeHtml(data.message || "Connection verified.")}</p>
                    <ul style="display:flex;flex-direction:column;gap:6px;margin-top:12px;font-size:0.83rem;list-style:none;">
                        ${row(!!checks.api_reachable, "Telegram API credentials present")}
                        ${row(!!checks.authentication, "Authentication valid")}
                        ${row(!!checks.storage_channel, "Storage Channel accessible")}
                        ${row(!!checks.session_persisted, "MTProto session stored on this server")}
                    </ul>
                </div>`;
        }
        if (data.ok) showToast("Telegram storage verified", "success");
        else showToast(info.text, "error", 6000);
        refreshTelegramState(false);
    } catch (e) {
        showToast("Connection test failed: " + e.message, "error", 6000);
    } finally {
        if (btn) btn.disabled = false;
        if (label) label.innerText = "Test Connection";
    }
}

let lastTelegramState = null;

// --- Self-update banner ------------------------------------------------------
// Polls /api/update/status once per page load (the server caches GitHub for
// 24h). Shows a banner with the exact upgrade command for this install kind.
// Dismissal is remembered per released version, not forever.
async function checkForUpdates(retry) {
    try {
        const res = await apiFetch("/api/update/status");
        if (!res.ok) return;
        const s = await res.json();
        if (s.checking && !s.latest && !retry) {
            // First check still in flight server-side; retry once shortly.
            setTimeout(() => checkForUpdates(true), 15000);
            return;
        }
        if (!s.available || !s.latest) return;
        if (localStorage.getItem("tdocs_update_dismissed") === s.latest) return;
        const banner = document.getElementById("update-banner");
        if (!banner) return;
        const ver = document.getElementById("update-version");
        if (ver) ver.textContent = "tDocs " + s.latest + " (running " + (s.current || "?") + ")";
        const hint = document.getElementById("update-hint");
        if (hint) hint.textContent = s.hint || "tdocs update";
        const link = document.getElementById("update-release-link");
        if (link && s.url) link.href = s.url;
        banner.style.display = "";
    } catch (_) {
        // Update checks are best-effort; never break the dashboard.
    }
}

function dismissUpdateBanner() {
    const banner = document.getElementById("update-banner");
    const ver = document.getElementById("update-version");
    try {
        const m = banner && ver ? (ver.textContent.match(/tDocs\s+(\S+)/) || []) : [];
        if (m[1]) localStorage.setItem("tdocs_update_dismissed", m[1]);
    } catch (_) {}
    if (banner) banner.style.display = "none";
}

function copyUpdateHint() {
    const hint = document.getElementById("update-hint");
    const text = hint ? hint.textContent : "";
    if (!text) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(
            () => showToast("Upgrade command copied", "success", 2500),
            () => showToast(text, "info", 6000)
        );
    } else {
        showToast(text, "info", 6000);
    }
}

async function refreshTelegramState(live) {
    try {
        const res = await apiFetch("/api/telegram/state" + (live ? "?live=1" : ""));
        if (!res.ok) return null;
        const s = await res.json();
        lastTelegramState = s;
        const banner = document.getElementById("telegram-setup-banner");
        if (banner) banner.style.display = s.state === "connected" ? "none" : "";
        const title = document.querySelector("#telegram-setup-banner .wizard-banner-title");
        const sub = document.querySelector("#telegram-setup-banner .wizard-banner-sub");
        const info = tgStateInfo(s.state);
        if (title) title.innerText = "Telegram Storage — " + info.short;
        if (sub) sub.innerText = s.message || "Connect your Telegram account to start storing files.";
        const pill = document.getElementById("tg-state-pill");
        if (pill) {
            pill.innerText = info.short;
            pill.className = "tg-state-pill tg-" + (s.state || "error");
        }
        return s;
    } catch (e) {
        return null;
    }
}

// --- Theme Management ---
function initTheme() {
    const savedTheme = localStorage.getItem("tdocs_theme") || localStorage.getItem("robdocs_theme") || localStorage.getItem("teledrive_theme");
    const systemDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    // Dark-first console theme: an explicit choice always wins, otherwise
    // follow the OS (`prefers-color-scheme`) and only fall back to dark.
    const theme = savedTheme || "light";
    setTheme(theme);
}

function toggleTheme() {
    const current = document.documentElement.getAttribute("data-theme") || "light";
    const next = current === "dark" ? "light" : "dark";
    setTheme(next);
}

function setTheme(theme) {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("tdocs_theme", theme);
    const themeBtn = document.getElementById("theme-toggle-btn");
    if (themeBtn) {
        themeBtn.innerHTML = theme === "dark" ? getIcon("sun") : getIcon("moon");
        themeBtn.setAttribute("title", `Switch to ${theme === "dark" ? "Light" : "Dark"} mode`);
    }
    // Swap header logo variant so the wordmark stays readable.
    document.querySelectorAll('img[data-dark-src]').forEach(img => {
        const light = img.getAttribute('src');
        const dark  = img.getAttribute('data-dark-src');
        if (!light || !dark) return;
        img.src = theme === 'dark' ? dark : light;
    });
}

// --- Toast System ---
function showToast(message, type = "info", duration = 3500) {
    let container = document.getElementById("toast-container");
    if (!container) {
        container = document.createElement("div");
        container.id = "toast-container";
        container.className = "toast-container";
        document.body.appendChild(container);
    }

    const toast = document.createElement("div");
    toast.className = `toast ${type}`;
    const iconName = type === "success" ? "check" : type === "error" ? "alert-circle" : "info";
    toast.innerHTML = `
        <span style="color: var(--${type === "error" ? "danger" : type === "success" ? "success" : "accent"});">
            ${getIcon(iconName)}
        </span>
        <div class="toast-content">${escapeHtml(message)}</div>
        <button class="btn-icon" onclick="this.parentElement.remove()">${getIcon("x", "icon-sm")}</button>
    `;

    container.appendChild(toast);
    setTimeout(() => {
        if (toast.parentElement) toast.remove();
    }, duration);
}

// --- Navigation Tabs ---
const ALL_TABS = ["overview", "drive", "favorites", "shares", "trash", "storage", "snapshots", "music", "videos", "images", "settings"];

// Sidebar sections that reuse the shared media library view with a fixed kind.
const MEDIA_TABS = { music: "audio/", videos: "video/", images: "image/" };

function switchTab(tab) {
    currentTab = tab;
    document.querySelectorAll(".nav-link").forEach(el => el.classList.remove("active"));
    const navEl = document.getElementById(`nav-${tab}`);
    if (navEl) navEl.classList.add("active");

    const containers = {
        overview: "overview-content-container",
        drive: "drive-content-container",
        media: "media-content-container",
        favorites: "favorites-content-container",
        shares: "shares-content-container",
        storage: "storage-content-container",
        snapshots: "snapshots-content-container",
        trash: "trash-content-container",
        settings: "settings-content-container"
    };
    // Media sub-sections (Music / Videos / Images) render into the shared
    // media container so the library code stays single-source.
    const containerKey = MEDIA_TABS[tab] ? "media" : tab;
    for (const [name, id] of Object.entries(containers)) {
        const el = document.getElementById(id);
        if (el) el.style.display = (name === containerKey) ? "block" : "none";
    }
    clearSelection();

    if (tab === "overview") {
        loadOverview();
    } else if (tab === "drive") {
        loadDriveContent();
    } else if (MEDIA_TABS[tab]) {
        mediaKind = MEDIA_TABS[tab];
        const names = { music: "Music", videos: "Videos", images: "Images" };
        const label = document.getElementById("media-section-title");
        if (label) label.innerText = names[tab] || tab;
        const sub = document.getElementById("media-section-sub");
        if (sub) sub.innerText = names[tab] ? `Your ${names[tab].toLowerCase()} with built-in playback.` : "Music, videos and images with built-in playback.";
        setMediaKind(mediaKind);
    } else if (tab === "favorites") {
        loadFavorites();
    } else if (tab === "shares") {
        loadSharesContent();
    } else if (tab === "storage") {
        loadStorageCenter();
    } else if (tab === "snapshots") {
        loadSnapshotsContent();
    } else if (tab === "trash") {
        loadTrash();
    } else if (tab === "settings") {
        loadSettings();
    }

    closeMobileSidebar();
    closeAvatarMenu();
}

// --- Drive Content Loading ---
async function loadDriveContent() {
    skeletonCards(document.getElementById("folders-grid"), 4);
    skeletonCards(document.getElementById("files-grid"), 6);
    skeletonRows(document.getElementById("table-body"), 6);
    try {
        const folderParam = currentFolderId ? `?folder_id=${currentFolderId}` : "";
        const [foldersRes, filesRes] = await Promise.all([
            apiFetch(`/api/folders${folderParam}`),
            apiFetch(`/api/files${folderParam}`)
        ]);

        if (!foldersRes.ok || !filesRes.ok) throw new Error("Failed to fetch files/folders");

        cachedFolders = await foldersRes.json() || [];
        cachedFiles = await filesRes.json() || [];

        try {
            const statRes = await apiFetch("/api/folders/stats");
            if (statRes.ok) {
                const stats = await statRes.json() || [];
                folderStats = {};
                stats.forEach(st => { folderStats[st.id] = st; });
            }
        } catch (e) { /* stats are decorative */ }

        try {
            const favRes = await apiFetch("/api/favorites");
            if (favRes.ok) {
                const favs = await favRes.json() || [];
                favoriteIds = new Set(favs.map(f => f.id));
            }
        } catch (e) { /* favorites are decorative */ }

        renderBreadcrumbs();
        renderActiveView();
    } catch (err) {
        showToast("Error loading files: " + err.message, "error");
    }
}

// --- Breadcrumbs ---
function renderBreadcrumbs() {
    const el = document.getElementById("breadcrumbs");
    if (!el) return;

    if (activeSearchQuery) {
        el.innerHTML = `
            <span class="breadcrumb-item" onclick="clearSearch()">
                ${getIcon("folder", "icon-sm")} Drive
            </span>
            <span class="breadcrumb-separator">${getIcon("chevron-right", "icon-sm")}</span>
            <span class="breadcrumb-item active">Search results for "${escapeHtml(activeSearchQuery)}"</span>
        `;
        return;
    }

    let html = `
        <span class="breadcrumb-item ${currentFolderId === null ? "active" : ""}" onclick="navigateTo(null, 'Drive')">
            ${getIcon("folder", "icon-sm")} Drive
        </span>
    `;

    currentFolderPath.forEach((crumb, idx) => {
        html += `<span class="breadcrumb-separator">${getIcon("chevron-right", "icon-sm")}</span>`;
        if (idx === currentFolderPath.length - 1) {
            html += `<span class="breadcrumb-item active">${escapeHtml(crumb.name)}</span>`;
        } else {
            html += `<span class="breadcrumb-item" onclick="navigateTo('${crumb.id}', '${ja(crumb.name)}')">${escapeHtml(crumb.name)}</span>`;
        }
    });

    el.innerHTML = html;
}

function navigateTo(folderId, folderName) {
    if (folderId === null) {
        currentFolderId = null;
        currentFolderPath = [];
    } else {
        const idx = currentFolderPath.findIndex(f => f.id === folderId);
        if (idx >= 0) {
            currentFolderPath = currentFolderPath.slice(0, idx + 1);
        } else {
            currentFolderPath.push({ id: folderId, name: folderName });
        }
        currentFolderId = folderId;
    }
    clearSearch(false);
    loadDriveContent();
}

// --- View Switching (Grid vs List/Table) ---
function setViewMode(mode) {
    currentView = mode;
    localStorage.setItem("tdocs_view", mode);
    updateViewButtons();
    renderActiveView();
}

function updateViewButtons() {
    const gridBtn = document.getElementById("btn-view-grid");
    const listBtn = document.getElementById("btn-view-list");
    if (gridBtn && listBtn) {
        gridBtn.classList.toggle("active", currentView === "grid");
        listBtn.classList.toggle("active", currentView === "list");
    }
}

function renderActiveView() {
    // Drop selections for rows that no longer exist (post-delete refresh).
    const visible = new Set([...cachedFolders.map(f => f.id), ...cachedFiles.map(f => f.id)]);
    for (const id of [...selectedIds]) {
        if (!visible.has(id)) selectedIds.delete(id);
    }
    renderBulkBar();
    const folders = sortItems([...cachedFolders], currentSort.field, currentSort.order);
    const files = sortItems([...cachedFiles], currentSort.field, currentSort.order);

    if (currentView === "grid") {
        document.getElementById("grid-view-container").style.display = "block";
        document.getElementById("list-view-container").style.display = "none";
        renderFoldersGrid(folders);
        renderFilesGrid(files);
    } else {
        document.getElementById("grid-view-container").style.display = "none";
        document.getElementById("list-view-container").style.display = "block";
        renderTableList(folders, files);
    }
    updateSortIndicators();
}

// --- Sorting ---
function setSort(field) {
    if (currentSort.field === field) {
        currentSort.order = currentSort.order === "asc" ? "desc" : "asc";
    } else {
        currentSort.field = field;
        currentSort.order = "asc";
    }
    renderActiveView();
}

function sortItems(items, field, order) {
    return items.sort((a, b) => {
        let valA = a[field];
        let valB = b[field];

        if (field === "name") {
            valA = (a.name || "").toLowerCase();
            valB = (b.name || "").toLowerCase();
            return order === "asc" ? valA.localeCompare(valB) : valB.localeCompare(valA);
        }
        if (field === "size") {
            valA = a.size || 0;
            valB = b.size || 0;
            return order === "asc" ? valA - valB : valB - valA;
        }
        if (field === "date") {
            valA = new Date(a.updated_at || a.created_at || 0).getTime();
            valB = new Date(b.updated_at || b.created_at || 0).getTime();
            return order === "asc" ? valA - valB : valB - valA;
        }
        return 0;
    });
}

// --- Grid Rendering ---
function renderFoldersGrid(folders) {
    const container = document.getElementById("folders-grid");
    const section = document.getElementById("folders-section");
    if (!container) return;

    if (folders.length === 0) {
        section.style.display = "none";
        return;
    }
    section.style.display = "block";

    const maxBytes = Math.max(1, ...folders.map(f => (folderStats[f.id] || { bytes: 0 }).bytes));
    container.innerHTML = folders.map(f => {
        const st = folderStats[f.id] || { files: 0, bytes: 0 };
        const pct = Math.max(st.bytes > 0 ? 4 : 0, Math.round(st.bytes / maxBytes * 100));
        return `
        <div class="card" onclick="navigateTo('${f.id}', '${ja(f.name)}')" data-name="${escapeHtml(f.name)}" oncontextmenu="showCtxMenu(event, 'folder', '${f.id}', this)">
            <div class="card-top">
                <div class="card-icon-wrap tile-blue">${getIcon("folder")}</div>
                <div class="card-actions" onclick="event.stopPropagation()">
                    <button class="btn-icon" title="More actions" onclick="showCtxMenu(event, 'folder', '${f.id}', this.closest('.card'))">${getIcon("more-vertical", "icon-sm")}</button>
                </div>
            </div>
            <div>
                <div class="card-title" title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</div>
                <div class="card-meta"><span>${formatSize(st.bytes)}</span><span>${st.files} file${st.files === 1 ? "" : "s"}</span></div>
                <div class="folder-use"><div style="width: ${pct}%;"></div></div>
            </div>
        </div>
    `; }).join("");
}

function fileCardHTML(f, opts = {}) {
    const fileType = getFileCategory(f.mime_type, f.name);
    const fav = favoriteIds.has(f.id);
    const sel = opts.selectable === false ? "" : `<input type="checkbox" class="select-check card-select" data-id="${f.id}" ${selectedIds.has(f.id) ? "checked" : ""} onclick="event.stopPropagation(); onSelectChange()">`;
    return `
        <div class="card" onclick="openPreview('${f.id}', '${ja(f.name)}', '${f.mime_type}', ${f.size})" data-name="${escapeHtml(f.name)}" oncontextmenu="showCtxMenu(event, 'file', '${f.id}', this)">
            ${sel}
            <div class="card-top">
                <div class="card-icon-wrap ${fileType.tile}">
                    ${getIcon(fileType.icon)}
                </div>
                <div class="card-actions" onclick="event.stopPropagation()">
                    <button class="btn-icon" title="${fav ? "Unstar" : "Star"}" onclick="toggleFavorite('${f.id}', ${!fav})" style="${fav ? "color: var(--warning);" : ""}">${getIcon("star", "icon-sm")}</button>
                    <button class="btn-icon" title="Details" onclick="openDetailsDrawer('${f.id}')">${getIcon("eye", "icon-sm")}</button>
                    <button class="btn-icon" title="Share Link" onclick="openShareModal('${f.id}', '${ja(f.name)}')">${getIcon("share-2", "icon-sm")}</button>
                    <button class="btn-icon" title="Download" onclick="downloadFile('${f.id}', '${ja(f.name)}')">${getIcon("download", "icon-sm")}</button>
                    <button class="btn-icon" title="Rename" onclick="promptRenameFile('${f.id}', '${ja(f.name)}')">${getIcon("edit", "icon-sm")}</button>
                    <button class="btn-icon" title="Move to Trash" onclick="confirmDeleteFile('${f.id}', '${ja(f.name)}')">${getIcon("trash-2", "icon-sm")}</button>
                </div>
            </div>
            <div>
                <div class="card-title" title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</div>
                <div class="card-meta">${formatSize(f.size)} &bull; ${formatDate(f.updated_at || f.created_at)}</div>
            </div>
        </div>
    `;
}

function renderFilesGrid(files) {
    const container = document.getElementById("files-grid");
    const section = document.getElementById("files-section");
    const emptyState = document.getElementById("empty-drive-state");
    if (!container) return;

    if (files.length === 0 && cachedFolders.length === 0) {
        section.style.display = "none";
        if (emptyState) emptyState.style.display = "flex";
        return;
    }

    if (emptyState) emptyState.style.display = "none";

    if (files.length === 0) {
        section.style.display = "none";
        return;
    }
    section.style.display = "block";

    container.innerHTML = files.map(f => fileCardHTML(f)).join("");
}

// --- Table / List View Rendering ---
function renderTableList(folders, files) {
    const tbody = document.getElementById("table-body");
    const emptyState = document.getElementById("empty-drive-state");
    if (!tbody) return;

    if (folders.length === 0 && files.length === 0) {
        if (emptyState) emptyState.style.display = "flex";
        tbody.innerHTML = "";
        return;
    }
    if (emptyState) emptyState.style.display = "none";

    let rowsHtml = "";

    // Folders first
    folders.forEach(f => {
        rowsHtml += `
            <tr onclick="navigateTo('${f.id}', '${ja(f.name)}')" data-name="${escapeHtml(f.name)}" oncontextmenu="showCtxMenu(event, 'folder', '${f.id}', this)">
                <td>
                    <div class="table-name-cell">
                        <span style="color: var(--folder);">${getIcon("folder")}</span>
                        <span>${escapeHtml(f.name)}</span>
                    </div>
                </td>
                <td style="color: var(--text-muted);">&mdash;</td>
                <td style="color: var(--text-muted);">${formatDate(f.updated_at || f.created_at)}</td>
                <td onclick="event.stopPropagation()">
                    <div style="display: flex; gap: 4px;">
                        <button class="btn-icon" title="Rename" onclick="promptRenameFolder('${f.id}', '${ja(f.name)}')">${getIcon("edit", "icon-sm")}</button>
                        <button class="btn-icon" title="Delete" onclick="confirmDeleteFolder('${f.id}', '${ja(f.name)}')">${getIcon("trash-2", "icon-sm")}</button>
                    </div>
                </td>
            </tr>
        `;
    });

    // Files
    files.forEach(f => {
        const fileType = getFileCategory(f.mime_type, f.name);
        const fav = favoriteIds.has(f.id);
        rowsHtml += `
            <tr onclick="openPreview('${f.id}', '${ja(f.name)}', '${f.mime_type}', ${f.size})" data-name="${escapeHtml(f.name)}" oncontextmenu="showCtxMenu(event, 'file', '${f.id}', this)">
                <td onclick="event.stopPropagation()"><input type="checkbox" class="select-check" data-id="${f.id}" ${selectedIds.has(f.id) ? "checked" : ""} onchange="onSelectChange()"></td>
                <td>
                    <div class="table-name-cell">
                        <span class="card-icon-wrap ${fileType.tile}" style="width: 28px; height: 28px; border-radius: 8px;">
                            ${getIcon(fileType.icon, "icon-sm")}
                        </span>
                        <span title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</span>
                    </div>
                </td>
                <td>${formatSize(f.size)}</td>
                <td style="color: var(--text-muted);">${formatDate(f.updated_at || f.created_at)}</td>
                <td onclick="event.stopPropagation()">
                    <div style="display: flex; gap: 4px;">
                        <button class="btn-icon" title="${fav ? "Unstar" : "Star"}" onclick="toggleFavorite('${f.id}', ${!fav})" style="${fav ? "color: var(--warning);" : ""}">${getIcon("star", "icon-sm")}</button>
                        <button class="btn-icon" title="Details" onclick="openDetailsDrawer('${f.id}')">${getIcon("eye", "icon-sm")}</button>
                        <button class="btn-icon" title="Share Link" onclick="openShareModal('${f.id}', '${ja(f.name)}')">${getIcon("share-2", "icon-sm")}</button>
                        <button class="btn-icon" title="Download" onclick="downloadFile('${f.id}', '${ja(f.name)}')">${getIcon("download", "icon-sm")}</button>
                        <button class="btn-icon" title="Rename" onclick="promptRenameFile('${f.id}', '${ja(f.name)}')">${getIcon("edit", "icon-sm")}</button>
                        <button class="btn-icon" title="Move to Trash" onclick="confirmDeleteFile('${f.id}', '${ja(f.name)}')">${getIcon("trash-2", "icon-sm")}</button>
                    </div>
                </td>
            </tr>
        `;
    });

    tbody.innerHTML = rowsHtml;
}

// --- File Type Resolver ---
function getFileCategory(mime, filename = "") {
    mime = (mime || "").toLowerCase();
    const ext = filename.split(".").pop().toLowerCase();

    if (mime.startsWith("video/") || ["mp4", "mkv", "avi", "mov", "webm"].includes(ext)) {
        return { category: "video", icon: "film", tile: "tile-video" };
    }
    if (mime.startsWith("audio/") || ["mp3", "flac", "wav", "ogg", "m4a"].includes(ext)) {
        return { category: "audio", icon: "music", tile: "tile-audio" };
    }
    if (mime.startsWith("image/") || ["jpg", "jpeg", "png", "gif", "webp", "svg"].includes(ext)) {
        return { category: "image", icon: "image", tile: "tile-image" };
    }
    if (mime === "application/pdf" || ext === "pdf") {
        return { category: "pdf", icon: "file-text", tile: "tile-pdf" };
    }
    if (mime.startsWith("text/") || ["txt", "md", "json", "go", "py", "js", "html", "css", "yaml", "yml", "sql", "sh"].includes(ext)) {
        return { category: "code", icon: "file-text", tile: "tile-code" };
    }
    if (["zip", "rar", "7z", "tar", "gz"].includes(ext)) {
        return { category: "archive", icon: "file", tile: "tile-other" };
    }
    return { category: "file", icon: "file", tile: "tile-doc" };
}

// --- Search Handling ---
function setupSearch() {
    const input = document.getElementById("search-box");
    const clearBtn = document.getElementById("search-clear-btn");
    if (!input) return;

    input.addEventListener("input", (e) => {
        const q = e.target.value.trim();
        if (clearBtn) clearBtn.style.display = q ? "block" : "none";

        clearTimeout(searchDebounceTimer);
        searchDebounceTimer = setTimeout(() => {
            performSearch(q);
        }, 250);
    });
}

async function performSearch(query) {
    activeSearchQuery = query;
    const banner = document.getElementById("search-banner");
    const bannerText = document.getElementById("search-banner-text");

    if (!query) {
        if (banner) banner.style.display = "none";
        loadDriveContent();
        return;
    }

    try {
        const res = await apiFetch(`/api/files?search=${encodeURIComponent(query)}`);
        const files = await res.json() || [];
        cachedFolders = []; // Search results focus on matched virtual files
        cachedFiles = files;

        if (banner && bannerText) {
            banner.style.display = "flex";
            bannerText.innerText = `Found ${files.length} file${files.length === 1 ? "" : "s"} matching "${query}"`;
        }

        renderBreadcrumbs();
        renderActiveView();
    } catch (err) {
        showToast("Search failed: " + err.message, "error");
    }
}

function clearSearch(reload = true) {
    const input = document.getElementById("search-box");
    const clearBtn = document.getElementById("search-clear-btn");
    const banner = document.getElementById("search-banner");

    if (input) input.value = "";
    if (clearBtn) clearBtn.style.display = "none";
    if (banner) banner.style.display = "none";
    activeSearchQuery = "";

    if (reload) {
        loadDriveContent();
    }
}

// --- Shared Links View & API ---
async function loadSharesContent() {
    try {
        const res = await apiFetch("/api/shares");
        if (!res.ok) throw new Error("Failed to load share links");
        const shares = await res.json() || [];

        const tbody = document.getElementById("shares-table-body");
        const emptyState = document.getElementById("empty-shares-state");

        if (shares.length === 0) {
            if (emptyState) emptyState.style.display = "flex";
            if (tbody) tbody.innerHTML = "";
            return;
        }

        if (emptyState) emptyState.style.display = "none";
        if (!tbody) return;

        tbody.innerHTML = shares.map(s => {
            const shareUrl = `${window.location.origin}/s/${s.token}`;
            const isExpired = s.expires_at && new Date(s.expires_at) < new Date();
            return `
                <tr>
                    <td>
                        <div class="table-name-cell">
                            <span style="color: var(--primary);">${getIcon("share-2")}</span>
                            <span style="font-weight: 600;">${escapeHtml(s.file_name)}</span>
                            ${s.preview_only ? `<span class="nkind" style="display:inline-block;font-size:0.65rem;font-weight:800;padding:2px 8px;border-radius:99px;background:var(--primary-soft);color:var(--primary);margin-left:6px;">PREVIEW</span>` : ""}
                        </div>
                    </td>
                    <td>${formatSize(s.file_size)}</td>
                    <td>
                        ${s.has_password 
                            ? `<span style="color: var(--warning); display: flex; align-items: center; gap: 4px;">${getIcon("lock", "icon-sm")} Protected</span>` 
                            : `<span style="color: var(--text-muted);">None</span>`}
                    </td>
                    <td>
                        ${s.expires_at 
                            ? `<span style="color: ${isExpired ? "var(--danger)" : "var(--text-secondary)"};">${formatDate(s.expires_at)} ${isExpired ? "(Expired)" : ""}</span>` 
                            : `<span style="color: var(--text-muted);">Never</span>`}
                    </td>
                    <td>${s.download_count}</td>
                    <td>
                        <div style="display: flex; gap: 6px;">
                            <button class="btn btn-secondary" style="padding: 4px 10px; font-size: 0.75rem;" onclick="copyToClipboard('${shareUrl}')" title="Copy Link">
                                ${getIcon("copy", "icon-sm")} Copy
                            </button>
                            <button class="btn btn-secondary" style="padding: 4px 10px; font-size: 0.75rem;" onclick="openQrModal('${shareUrl}', '${ja(s.file_name)}')" title="Show QR Code">
                                ${getIcon("qr-code", "icon-sm")} QR
                            </button>
                            <button class="btn-icon btn-danger" style="padding: 4px 6px;" onclick="confirmRevokeShare('${s.id}', '${ja(s.file_name)}')" title="Revoke Share">
                                ${getIcon("trash-2", "icon-sm")}
                            </button>
                        </div>
                    </td>
                </tr>
            `;
        }).join("");
    } catch (err) {
        showToast("Failed to fetch shares: " + err.message, "error");
    }
}

function confirmRevokeShare(id, fileName) {
    showConfirmDialog({
        title: "Revoke Share Link",
        message: `Are you sure you want to revoke the share link for "${fileName}"? Anyone with this URL will immediately lose access.`,
        confirmText: "Revoke Link",
        danger: true,
        onConfirm: async () => {
            try {
                const res = await apiFetch(`/api/shares/${id}`, { method: "DELETE" });
                if (!res.ok) throw new Error("Revoke failed");
                showToast("Share link revoked successfully", "success");
                loadSharesContent();
            } catch (err) {
                showToast(err.message, "error");
            }
        }
    });
}

// --- Database Snapshots & Point-in-Time Restore ---
async function loadSnapshotsContent() {
    try {
        const res = await apiFetch("/api/snapshots");
        if (!res.ok) {
            // Snapshots live in the Telegram Storage Channel. Without a verified
            // backend, say so plainly instead of showing a fake empty list.
            const body = await res.json().catch(() => ({}));
            renderTelegramUnavailable("snapshots-table-body", body);
            return;
        }
        const snapshots = await res.json() || [];

        const tbody = document.getElementById("snapshots-table-body");
        const emptyState = document.getElementById("empty-snapshots-state");

        if (snapshots.length === 0) {
            if (emptyState) emptyState.style.display = "flex";
            if (tbody) tbody.innerHTML = "";
            return;
        }

        if (emptyState) emptyState.style.display = "none";
        if (!tbody) return;

        tbody.innerHTML = snapshots.map(s => {
            const statusBadge = s.is_pinned
                ? `<span style="color: var(--warning); font-size: 0.8rem; font-weight: 600; display: inline-flex; align-items: center; gap: 4px;">Pinned</span>`
                : `<span style="color: var(--text-muted); font-size: 0.8rem;">Archived</span>`;

            return `
                <tr>
                    <td>
                        <div class="table-name-cell">
                            <span style="color: var(--primary);">${getIcon("database")}</span>
                            <span style="font-weight: 600; font-family: monospace; font-size: 0.85rem;">${escapeHtml(s.file_name)}</span>
                        </div>
                    </td>
                    <td>${formatSize(s.size)}</td>
                    <td><span style="font-family: monospace; font-size: 0.85rem; color: var(--text-secondary);">#${s.message_id}</span></td>
                    <td>${formatDateTime(s.created_at)}</td>
                    <td>${statusBadge}</td>
                    <td>
                        <div style="display: flex; gap: 6px; justify-content: flex-end;">
                            <button class="btn btn-secondary" style="padding: 4px 10px; font-size: 0.75rem; color: var(--warning);" onclick="confirmRestoreSnapshot(${s.message_id}, '${ja(s.file_name)}')" title="Restore this snapshot">
                                ${getIcon("rotate-ccw", "icon-sm")} Restore
                            </button>
                            <button class="btn btn-secondary" style="padding: 4px 10px; font-size: 0.75rem;" onclick="downloadSnapshot(${s.message_id})" title="Download .db.gz">
                                ${getIcon("download", "icon-sm")} Download
                            </button>
                            <button class="btn-icon btn-danger" style="padding: 4px 6px;" onclick="confirmDeleteSnapshot(${s.message_id}, '${ja(s.file_name)}')" title="Delete snapshot">
                                ${getIcon("trash-2", "icon-sm")}
                            </button>
                        </div>
                    </td>
                </tr>
            `;
        }).join("");
    } catch (err) {
        showToast("Failed to fetch snapshots: " + err.message, "error");
    }
}

async function createSnapshotNow() {
    const btn = document.getElementById("btn-create-snapshot");
    const textSpan = document.getElementById("create-snapshot-text");
    if (btn) btn.disabled = true;
    if (textSpan) textSpan.innerText = "Creating snapshot...";

    try {
        const res = await apiFetch("/api/snapshots", { method: "POST" });
        if (!res.ok) {
            const errText = await res.text();
            throw new Error(errText || "Failed to create snapshot");
        }
        showToast("Snapshot created and pinned in the Telegram Storage Channel!", "success");
        await loadSnapshotsContent();
    } catch (err) {
        showToast("Snapshot error: " + err.message, "error");
    } finally {
        if (btn) btn.disabled = false;
        if (textSpan) textSpan.innerText = "Create Snapshot Now";
    }
}

function confirmRestoreSnapshot(id, fileName) {
    showConfirmDialog({
        title: "Point-in-Time Restore",
        message: `Are you sure you want to restore snapshot "${fileName}"? This will overwrite your current database. The page will reload once restore finishes.`,
        confirmText: "Restore Snapshot",
        danger: true,
        onConfirm: async () => {
            const overlay = document.getElementById("restore-overlay-modal");
            const overlayTitle = document.getElementById("restore-overlay-title");
            const overlayMsg = document.getElementById("restore-overlay-msg");
            if (overlay) overlay.style.display = "flex";
            if (overlayTitle) overlayTitle.innerText = "Restoring Database...";
            if (overlayMsg) overlayMsg.innerText = "Draining connections and applying SQLite snapshot. Please wait...";

            try {
                const res = await apiFetch(`/api/snapshots/${id}/restore`, { method: "POST" });
                if (!res.ok) {
                    const errText = await res.text();
                    throw new Error(errText || "Failed to restore snapshot");
                }
                if (overlayTitle) overlayTitle.innerText = "Restore Complete!";
                if (overlayMsg) overlayMsg.innerText = "Database successfully restored. Reloading page...";
                setTimeout(() => {
                    window.location.reload();
                }, 2000);
            } catch (err) {
                if (overlay) overlay.style.display = "none";
                showToast("Restore failed: " + err.message, "error", 6000);
            }
        }
    });
}

function downloadSnapshot(id) {
    window.location.href = `/api/snapshots/${id}/download`;
}

function confirmDeleteSnapshot(id, fileName) {
    showConfirmDialog({
        title: "Delete Snapshot",
        message: `Are you sure you want to delete snapshot "${fileName}" (Telegram Msg #${id}) from the Storage Channel?`,
        confirmText: "Delete Snapshot",
        danger: true,
        onConfirm: async () => {
            try {
                const res = await apiFetch(`/api/snapshots/${id}`, { method: "DELETE" });
                if (!res.ok) {
                    const errText = await res.text();
                    throw new Error(errText || "Failed to delete snapshot");
                }
                showToast("Snapshot deleted from Telegram Storage Channel", "success");
                loadSnapshotsContent();
            } catch (err) {
                showToast("Delete failed: " + err.message, "error");
            }
        }
    });
}

function handleSnapshotFileSelect(event) {
    const file = event.target.files && event.target.files[0];
    if (!file) return;

    // Reset input value so same file can be re-selected if needed
    event.target.value = "";

    showConfirmDialog({
        title: "Upload & Restore Snapshot",
        message: `Are you sure you want to restore from "${file.name}"? Current database contents will be replaced with this backup file.`,
        confirmText: "Upload & Overwrite",
        danger: true,
        onConfirm: async () => {
            const overlay = document.getElementById("restore-overlay-modal");
            const overlayTitle = document.getElementById("restore-overlay-title");
            const overlayMsg = document.getElementById("restore-overlay-msg");
            if (overlay) overlay.style.display = "flex";
            if (overlayTitle) overlayTitle.innerText = "Uploading & Restoring...";
            if (overlayMsg) overlayMsg.innerText = "Uploading local snapshot and applying to database. Please wait...";

            const formData = new FormData();
            formData.append("snapshot", file);

            try {
                const res = await apiFetch("/api/snapshots/upload-restore", {
                    method: "POST",
                    body: formData
                });
                if (!res.ok) {
                    const errText = await res.text();
                    throw new Error(errText || "Upload restore failed");
                }
                if (overlayTitle) overlayTitle.innerText = "Restore Complete!";
                if (overlayMsg) overlayMsg.innerText = "Local backup successfully restored. Reloading page...";
                setTimeout(() => {
                    window.location.reload();
                }, 2000);
            } catch (err) {
                if (overlay) overlay.style.display = "none";
                showToast("Upload restore failed: " + err.message, "error", 6000);
            }
        }
    });
}

// --- Channel Sync (recover file metadata from Telegram Storage Channel) ---
async function syncChannelNow() {
    const btn = document.getElementById("btn-sync-channel");
    const textSpan = document.getElementById("sync-channel-text");
    if (btn) btn.disabled = true;
    if (textSpan) textSpan.innerText = "Syncing...";

    try {
        const res = await apiFetch("/api/sync", { method: "POST" });
        if (!res.ok) {
            const t = await res.text().catch(() => "");
            throw new Error(t || "Channel sync failed");
        }
        const data = await res.json();
        showToast(`Channel sync done: ${data.inserted} recovered, ${data.updated} refreshed (${data.total} total).`, "success", 5000);
        await loadDriveContent();
    } catch (err) {
        showToast("Sync error: " + err.message, "error", 6000);
    } finally {
        if (btn) btn.disabled = false;
        if (textSpan) textSpan.innerText = "Sync from Channel";
    }
}

// --- Upload Management (Sequential Safe Mode) ---
function openFilePicker() {
    const fileInput = document.getElementById("file-input");
    if (!fileInput) {
        showToast("Upload unavailable: file input missing", "error");
        return;
    }
    fileInput.click();
}

function setupDropzone() {
    const fileInput = document.getElementById("file-input");
    if (!fileInput) return;

    // Wire up any dropzone element (drive tab + overview tab).
    document.querySelectorAll(".dropzone").forEach(dropzone => {
        dropzone.setAttribute("role", "button");
        dropzone.setAttribute("tabindex", "0");
        dropzone.setAttribute("aria-label", "Upload files: click to browse or drag and drop here");
        dropzone.addEventListener("click", openFilePicker);
        dropzone.addEventListener("keydown", e => {
            if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                openFilePicker();
            }
        });

        ["dragenter", "dragover"].forEach(evt => {
            dropzone.addEventListener(evt, e => {
                e.preventDefault();
                dropzone.classList.add("dragover");
            });
        });

        ["dragleave", "drop"].forEach(evt => {
            dropzone.addEventListener(evt, e => {
                e.preventDefault();
                dropzone.classList.remove("dragover");
            });
        });

        dropzone.addEventListener("drop", e => {
            if (e.dataTransfer.files.length > 0) {
                handleQueueFiles(e.dataTransfer.files);
            }
        });
    });

    fileInput.addEventListener("change", e => {
        if (e.target.files.length > 0) {
            handleQueueFiles(e.target.files);
        }
        // Reset so picking the same file again still fires change.
        e.target.value = "";
    });
}

function handleQueueFiles(fileList) {
    if (!fileList || fileList.length === 0) return;

    const files = Array.from(fileList);

    // Guard against zero-byte files early so the drawer never shows a
    // cryptic mid-stream failure.
    const usable = [];
    files.forEach(file => {
        if (!file || file.size <= 0) {
            showToast(`Skipped empty file: ${file ? file.name : "unknown"} (0 bytes)`, "error");
            return;
        }
        usable.push(file);
    });
    if (usable.length === 0) return;

    // Enqueue first, then render + start the sequential Safe Mode worker.
    usable.forEach(file => {
        uploadQueue.push({
            qid: ++uploadSeq,
            file,
            status: "queued",
            paused: false,
            progress: 0,
            chunkStatus: "Queued",
            error: "",
            sessionId: null,
            chunkSize: 5 * 1024 * 1024,
            nextChunk: 0
        });
    });
    uploadStats.total += usable.length;

    const drawer = document.getElementById("upload-drawer");
    if (drawer) {
        drawer.style.display = "block";
        drawer.classList.remove("minimized");
        isDrawerMinimized = false;
    }

    renderUploadDrawer();
    processNextUpload();
}

async function waitWhilePaused(item) {
    while (item.paused && item.status === "uploading") {
        item.chunkStatus = "Paused";
        renderUploadDrawer();
        await new Promise(r => setTimeout(r, 300));
    }
}

function pauseUpload(qid) {
    const item = uploadQueue.find(i => i.qid === qid);
    if (!item || item.status === "completed" || item.status === "failed") return;
    item.paused = true;
    if (item.status === "queued") {
        item.status = "paused";
        item.chunkStatus = "Paused";
    }
    renderUploadDrawer();
}

function resumeUpload(qid) {
    const item = uploadQueue.find(i => i.qid === qid);
    if (!item || item.status === "completed") return;
    item.paused = false;
    if (item.status === "paused" || item.status === "failed") {
        if (item.status === "failed") {
            item.sessionId = null;
            item.nextChunk = 0;
            item.progress = 0;
            item.error = "";
            uploadStats.failed = Math.max(0, uploadStats.failed - 1);
        }
        item.status = "queued";
        item.chunkStatus = "Queued";
    }
    renderUploadDrawer();
    processNextUpload();
}

async function processNextUpload() {
    if (isUploading) return;
    const currentItem = uploadQueue.find(item => item.status === "queued");
    if (!currentItem) {
        const allSettled = uploadQueue.length > 0 &&
            uploadQueue.every(i => i.status === "completed" || i.status === "failed" || i.status === "paused");
        if (allSettled) {
            renderUploadDrawer();
            const anyFailed = uploadQueue.some(i => i.status === "failed");
            const anyPaused = uploadQueue.some(i => i.status === "paused");
            // Keep failures/pauses visible; auto-dismiss clean runs.
            setTimeout(() => {
                if (isUploading || !allSettled) return;
                const drawer = document.getElementById("upload-drawer");
                if (drawer && !anyFailed && !anyPaused) {
                    drawer.style.display = "none";
                }
                if (!anyPaused) {
                    uploadQueue = [];
                    uploadStats = { total: 0, completed: 0, failed: 0 };
                }
            }, (anyFailed || anyPaused) ? 15000 : 6000);
        }
        return;
    }

    isUploading = true;
    currentItem.status = "uploading";
    currentItem.paused = false;
    currentItem.t0 = Date.now();
    currentItem._sp = null;
    renderUploadDrawer();

    const file = currentItem.file;

    try {
        if (!file || file.size <= 0) throw new Error("Empty files cannot be uploaded (0 bytes)");
        // 1. Init upload session (reused across pause/resume of the same item:
        // server-side chunk indexes are absolute, so re-sends are idempotent).
        let sessionId = currentItem.sessionId;
        let chunkSize = currentItem.chunkSize;
        if (!sessionId) {
            const initRes = await apiFetch("/api/upload/init", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                credentials: "same-origin",
                body: JSON.stringify({
                    name: file.name,
                    size: file.size,
                    mime_type: file.type || "application/octet-stream",
                    folder_id: currentFolderId
                })
            });

            if (!initRes.ok) {
                const t = await initRes.text().catch(() => "");
                throw new Error(t || "Upload initialization failed");
            }
            const session = await initRes.json();
            sessionId = session.id || session.session_id;
            if (!sessionId) throw new Error("Server did not return an upload session");
            chunkSize = session.chunk_size || 5 * 1024 * 1024;
            currentItem.sessionId = sessionId;
            currentItem.chunkSize = chunkSize;
        }
        const totalChunks = Math.max(1, Math.ceil(file.size / chunkSize));

        // 2. Upload chunk by chunk (FormData rebuilt per attempt: bodies are single-use)
        for (let chunkIdx = currentItem.nextChunk; chunkIdx < totalChunks; chunkIdx++) {
            await waitWhilePaused(currentItem);
            const start = chunkIdx * chunkSize;
            const end = Math.min(file.size, start + chunkSize);
            const chunkBlob = file.slice(start, end);

            currentItem.chunkStatus = `Chunk ${chunkIdx + 1}/${totalChunks}`;
            renderUploadDrawer();

            let lastErr = "";
            let success = false;
            for (let attempt = 0; attempt < 3 && !success; attempt++) {
                await waitWhilePaused(currentItem);
                const formData = new FormData();
                formData.append("session_id", sessionId);
                formData.append("chunk_index", String(chunkIdx));
                formData.append("chunk", chunkBlob, file.name);
                try {
                    const chunkRes = await apiFetch("/api/upload/chunk", {
                        method: "POST",
                        credentials: "same-origin",
                        body: formData
                    });
                    if (chunkRes.ok) {
                        success = true;
                    } else {
                        lastErr = await chunkRes.text().catch(() => "");
                        await new Promise(r => setTimeout(r, 800 * (attempt + 1)));
                    }
                } catch (e) {
                    lastErr = e.message || "network error";
                    await new Promise(r => setTimeout(r, 1000 * (attempt + 1)));
                }
            }
            if (!success) throw new Error(`Chunk ${chunkIdx + 1}/${totalChunks} failed${lastErr ? ": " + lastErr : ""}`);

            currentItem.nextChunk = chunkIdx + 1;
            currentItem.progress = Math.round((end / file.size) * 100);
            const elapsed = (Date.now() - (currentItem.t0 || Date.now())) / 1000;
            if (elapsed > 0.5) {
                const inst = end / elapsed;
                currentItem._sp = currentItem._sp == null ? inst : currentItem._sp * 0.6 + inst * 0.4;
                currentItem.chunkStatus = `Chunk ${chunkIdx + 1}/${totalChunks} \u2022 ${formatSize(currentItem._sp)}/s`;
            }
            renderUploadDrawer();
        }

        // 3. Complete and commit to Storage Channel
        currentItem.chunkStatus = "Committing to Storage Channel...";
        renderUploadDrawer();

        const completeRes = await apiFetch("/api/upload/complete", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify({ session_id: sessionId })
        });
        if (!completeRes.ok) {
            const t = await completeRes.text().catch(() => "");
            throw new Error(t || "Telegram MTProto document assembly failed");
        }
        const dupOf = completeRes.headers.get("X-Duplicate-Of");
        currentItem.status = "completed";
        currentItem.progress = 100;
        currentItem.chunkStatus = "Uploaded to Telegram Vault";
        uploadStats.completed++;
        if (dupOf) {
            showToast(`Uploaded ${file.name} — identical content already stored`, "info", 6000);
        } else {
            showToast(`Uploaded ${file.name}`, "success");
        }
        if (currentTab === "overview") loadOverview();
        if (currentTab === "drive") loadDriveContent();
    } catch (err) {
        currentItem.status = "failed";
        currentItem.error = err.message;
        uploadStats.failed++;
        showToast(`Upload failed: ${file.name} (${err.message})`, "error");
    } finally {
        isUploading = false;
        renderUploadDrawer();
        processNextUpload();
    }
}

function renderUploadDrawer() {
    const drawer = document.getElementById("upload-drawer");
    const headerTitle = document.getElementById("upload-drawer-title-text");
    const body = document.getElementById("upload-drawer-items");
    if (!drawer || !body) return;

    if (uploadQueue.length === 0) {
        drawer.style.display = "none";
        return;
    }

    const activeCount = uploadQueue.filter(i => i.status === "uploading" || i.status === "queued").length;
    const doneCount = uploadQueue.filter(i => i.status === "completed").length;
    const failedCount = uploadQueue.filter(i => i.status === "failed").length;
    if (headerTitle) {
        headerTitle.innerText = activeCount > 0
            ? `Uploading ${doneCount + failedCount}/${uploadQueue.length}`
            : `Uploads Finished (${doneCount} completed${failedCount ? `, ${failedCount} failed` : ""})`;
    }

    body.innerHTML = uploadQueue.map(item => {
        const statusText = item.status === "uploading"
            ? (item.chunkStatus || `${item.progress}%`)
            : item.status === "queued"
                ? "Queued"
                : item.status === "paused"
                    ? "Paused"
                    : "";
        const badge = item.status === "completed"
            ? `<span style="color: var(--success);">${getIcon("check", "icon-sm")} Done</span>`
            : item.status === "failed"
                ? `<span style="color: var(--danger);">${getIcon("alert-circle", "icon-sm")} Failed</span>`
                : "";
        const controls = item.status === "uploading" && !item.paused
            ? `<button class="btn-icon" style="width:26px;height:26px;" title="Pause" onclick="pauseUpload(${item.qid})">${getIcon("pause", "icon-sm")}</button>`
            : (item.status === "uploading" && item.paused) || item.status === "paused"
                ? `<button class="btn-icon" style="width:26px;height:26px;" title="Resume" onclick="resumeUpload(${item.qid})">${getIcon("play", "icon-sm")}</button>`
                : item.status === "failed"
                    ? `<button class="btn-icon" style="width:26px;height:26px;" title="Retry" onclick="resumeUpload(${item.qid})">${getIcon("refresh-cw", "icon-sm")}</button>`
                    : "";
        const errorLine = item.status === "failed" && item.error
            ? `<div style="color: var(--danger); font-size: 0.72rem; margin-top: 6px; word-break: break-word;">${escapeHtml(item.error)}</div>`
            : "";
        return `
        <div class="upload-item">
            <div class="upload-item-header">
                <span class="upload-item-name" title="${escapeHtml(item.file.name)}">${escapeHtml(item.file.name)}</span>
                <span class="upload-item-status" style="display:flex;align-items:center;gap:6px;">
                    ${statusText}
                    ${badge}
                    ${controls}
                </span>
            </div>
            <div class="progress-bar-wrap">
                <div class="progress-bar-fill ${item.status === "completed" ? "success" : item.status === "failed" ? "error" : ""}" style="width: ${item.progress}%;"></div>
            </div>
            ${errorLine}
        </div>
    `;
    }).join("");
}

function toggleUploadDrawer() {
    const drawer = document.getElementById("upload-drawer");
    if (!drawer) return;
    isDrawerMinimized = !isDrawerMinimized;
    drawer.classList.toggle("minimized", isDrawerMinimized);
    const minIcon = document.getElementById("drawer-min-icon");
    if (minIcon) minIcon.innerHTML = isDrawerMinimized ? getIcon("chevron-up", "icon-sm") : getIcon("chevron-down", "icon-sm");
}

function closeUploadDrawer() {
    const drawer = document.getElementById("upload-drawer");
    if (drawer) drawer.style.display = "none";
}

// --- Preview Modal (Range Streaming & Expanded Formats) ---
let previewState = null;
let videoQueue = [];
let galleryList = [];

async function openPreview(id, name, mime, size) {
    const modal = document.getElementById("preview-modal");
    const container = document.getElementById("preview-content");
    const title = document.getElementById("preview-title");
    const meta = document.getElementById("preview-meta");
    const downloadBtn = document.getElementById("preview-download-btn");
    const shareBtn = document.getElementById("preview-share-btn");

    if (!modal || !container || !title) return;

    previewState = { id, name, mime, size };
    title.innerText = name;
    if (meta) meta.innerText = `${formatSize(size)} • ${mime}`;
    if (downloadBtn) downloadBtn.onclick = () => downloadFile(id, name);
    if (shareBtn) shareBtn.onclick = () => { closePreview(); openShareModal(id, name); };

    const streamUrl = `/api/files/${id}/stream`;
    const fileCategory = getFileCategory(mime, name).category;

    if (fileCategory === "video") {
        videoQueue = [...cachedFiles].filter(f => getFileCategory(f.mime_type, f.name).category === "video");
        const qi = videoQueue.findIndex(f => f.id === id);
        // Resolve this video (and the next one) before the element asks for
        // bytes, so first-frame time is not spent on a backend round-trip.
        warmStream(id);
        if (qi >= 0 && videoQueue[qi + 1]) warmStream(videoQueue[qi + 1].id);
        container.innerHTML = `
            <div class="video-wrap">
                <video id="pv-video" controls autoplay preload="metadata" playsinline style="width: 100%; max-height: 60vh; border-radius: var(--radius-sm); outline: none; background: #000;">
                    <source src="${streamUrl}" type="${mime}">
                    Your browser does not support HTML5 video streaming.
                </video>
            </div>
            <div class="video-toolbar">
                <button class="btn-icon" title="Previous video" onclick="stepVideo(-1)" ${videoQueue.length < 2 ? "disabled" : ""}>${getIcon("skip-back", "icon-sm")}</button>
                <button class="btn-icon" title="Next video" onclick="stepVideo(1)" ${videoQueue.length < 2 ? "disabled" : ""}>${getIcon("skip-forward", "icon-sm")}</button>
                <select id="pv-speed" class="form-input" title="Playback speed" onchange="setVideoSpeed(parseFloat(this.value))">
                    <option value="0.5">0.5×</option><option value="0.75">0.75×</option>
                    <option value="1" selected>1×</option><option value="1.25">1.25×</option>
                    <option value="1.5">1.5×</option><option value="2">2×</option>
                </select>
                <button class="btn-icon" title="Picture in picture" onclick="videoPip()">${getIcon("picture-in-picture", "icon-sm")}</button>
                <button class="btn-icon" title="Fullscreen" onclick="videoFullscreen()">${getIcon("maximize", "icon-sm")}</button>
                <span id="pv-subs" style="font-size:0.75rem;color:var(--text-muted);"></span>
            </div>
        `;
        wireVideoElement(id);
    } else if (fileCategory === "audio") {
        MediaEngine.playQueue(
            [...cachedFiles].filter(f => getFileCategory(f.mime_type, f.name).category === "audio"),
            [...cachedFiles].filter(f => getFileCategory(f.mime_type, f.name).category === "audio").findIndex(f => f.id === id),
            { id, name, mime_type: mime, size }
        );
        container.innerHTML = `
            <div style="padding: 36px 20px; text-align: center; width: 100%;">
                <div class="mp-cover" style="width:72px;height:72px;margin:0 auto 16px;border-radius:20px;">${getIcon("music", "icon-lg")}</div>
                <div style="font-weight:800;">${escapeHtml(name)}</div>
                <div style="font-size:0.8rem;color:var(--text-muted);margin:4px 0 12px;">Playing in the mini player — browse freely, audio keeps going.</div>
                <button class="btn btn-secondary" onclick="MediaEngine.toggle()">Play / Pause</button>
            </div>
        `;
    } else if (fileCategory === "image") {
        if (!galleryList.some(g => g.id === id)) {
            galleryList = [...cachedFiles].filter(f => getFileCategory(f.mime_type, f.name).category === "image");
        }
        const gi = galleryList.findIndex(g => g.id === id);
        previewState.zoom = 1;
        container.innerHTML = `
            <div class="video-wrap" id="gallery-wrap">
                ${galleryList.length > 1 ? `<button class="gallery-nav gallery-prev" onclick="galleryStep(-1)" title="Previous">${getIcon("chevron-left", "icon-sm")}</button>` : ""}
                <img id="pv-img" src="${streamUrl}" style="max-width: 100%; max-height: 62vh; object-fit: contain; border-radius: var(--radius-sm); display:block; margin: 0 auto; cursor: zoom-in; transition: transform 0.15s ease;" onclick="galleryZoom()" alt="${escapeHtml(name)}">
                ${galleryList.length > 1 ? `<button class="gallery-nav gallery-next" onclick="galleryStep(1)" title="Next">${getIcon("chevron-right", "icon-sm")}</button>` : ""}
            </div>
            <div class="video-toolbar" style="justify-content: flex-end;">
                ${galleryList.length > 1 ? `<span style="font-size:0.78rem;color:var(--text-muted);">${gi + 1} / ${galleryList.length}</span>` : ""}
                <button class="btn-icon" title="Zoom in / out" onclick="galleryZoom()">${getIcon("zoom-in", "icon-sm")}</button>
                <button class="btn-icon" title="Fullscreen" onclick="galleryFullscreen()">${getIcon("maximize", "icon-sm")}</button>
            </div>
        `;
    } else if (fileCategory === "pdf") {
        container.innerHTML = `
            <iframe src="${streamUrl}" style="width: 100%; height: 65vh; border: none; border-radius: var(--radius-sm);"></iframe>
        `;
    } else if (fileCategory === "code") {
        container.innerHTML = `<div style="color: var(--text-muted); padding: 20px;">Loading preview...</div>`;
        try {
            const res = await apiFetch(streamUrl);
            const text = await res.text();
            container.innerHTML = renderTextPreview(name, text.slice(0, 100000));
        } catch (e) {
            container.innerHTML = `<p style="color: var(--danger);">Failed to load document text.</p>`;
        }
    } else if (/\.zip$/i.test(name)) {
        container.innerHTML = `<div style="color: var(--text-muted); padding: 20px;">Reading archive index...</div>`;
        inspectZip(id, name, size);
    } else {
        container.innerHTML = `
            <div class="empty-state">
                <div class="empty-icon">${getIcon("file", "icon-xl")}</div>
                <h3>Preview not available</h3>
                <p style="font-size: 0.85rem; color: var(--text-muted);">This file type cannot be previewed directly in browser.</p>
                <button class="btn btn-primary" onclick="downloadFile('${id}', '${ja(name)}')">
                    ${getIcon("download", "icon-sm")} Download File (${formatSize(size)})
                </button>
            </div>
        `;
    }

    modal.style.display = "flex";
}

// Detached-but-alive host so Picture-in-Picture keeps playing after the
// preview modal is torn down (clearing innerHTML would destroy the element).
function pipKeepAliveHost() {
    let host = document.getElementById("pip-keepalive");
    if (!host) {
        host = document.createElement("div");
        host.id = "pip-keepalive";
        host.setAttribute("aria-hidden", "true");
        host.style.cssText = "position:fixed;width:0;height:0;overflow:hidden;pointer-events:none;";
        document.body.appendChild(host);
    }
    return host;
}

function closePreview() {
    const modal = document.getElementById("preview-modal");
    const container = document.getElementById("preview-content");
    const v = document.getElementById("pv-video");
    const alreadyPip = !!(v && document.pictureInPictureElement === v);

    if (v && !alreadyPip) {
        if (!v.paused && !v.ended && document.pictureInPictureEnabled && !v.disablePictureInPicture) {
            // Keep watching while browsing: hand off to Picture-in-Picture.
            v.requestPictureInPicture().catch(() => v.pause());
        } else {
            v.pause();
        }
    }

    if (container) {
        // Clearing innerHTML destroys the element, which would end playback.
        // Park a PiP-rendered video in the keep-alive host first.
        if (v && document.pictureInPictureElement === v) pipKeepAliveHost().appendChild(v);
        container.innerHTML = "";
    }
    if (modal) modal.style.display = "none";
}

// --- Share Modal ---
let activeShareFileId = null;

function openShareModal(fileId, fileName) {
    activeShareFileId = fileId;
    const modal = document.getElementById("share-modal");
    const nameEl = document.getElementById("share-file-name");
    const resultBox = document.getElementById("share-result-box");
    const pwdInput = document.getElementById("share-password");

    if (nameEl) nameEl.innerText = fileName;
    if (resultBox) resultBox.style.display = "none";
    if (pwdInput) pwdInput.value = "";
    const poBox = document.getElementById("share-preview-only");
    if (poBox) poBox.checked = false;
    const maxBox = document.getElementById("share-maxdl");
    if (maxBox) maxBox.value = "0";
    if (modal) modal.style.display = "flex";
}

function closeShareModal() {
    const modal = document.getElementById("share-modal");
    if (modal) modal.style.display = "none";
}

async function createShareLinkSubmit() {
    return withBtnLoading(document.getElementById("share-submit-btn"), async () => {
    const password = document.getElementById("share-password").value.trim();
    const expiryDays = parseInt(document.getElementById("share-expiry").value);
    const maxDl = parseInt(document.getElementById("share-maxdl").value) || 0;
    const previewOnly = !!document.getElementById("share-preview-only").checked;

    try {
        const res = await apiFetch("/api/share", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                file_id: activeShareFileId,
                password: password || null,
                expiry_days: expiryDays || null,
                max_downloads: maxDl > 0 ? maxDl : null,
                preview_only: previewOnly
            })
        });

        if (!res.ok) throw new Error("Failed to create share link");
        const data = await res.json();
        const shareUrl = `${window.location.origin}/s/${data.token}`;

        document.getElementById("share-link-input").value = shareUrl;
        document.getElementById("share-result-box").style.display = "block";
        showToast("Share link generated!", "success");
    } catch (err) {
        showToast(err.message, "error");
    }
    });
}

function copyShareLinkInput() {
    const input = document.getElementById("share-link-input");
    copyToClipboard(input.value);
}

// --- QR Code Modal ---
function openQrModal(url, title = "Share Link") {
    const modal = document.getElementById("qr-modal");
    const titleEl = document.getElementById("qr-modal-title");
    const container = document.getElementById("qr-container");

    if (!modal || !container) return;
    if (titleEl) titleEl.innerText = title;

    // Lightweight QR SVG Generator via QuickChart SVG API or local fallback
    const qrApiUrl = `https://api.qrserver.com/v1/create-qr-code/?size=220x220&data=${encodeURIComponent(url)}`;
    container.innerHTML = `
        <div style="background: #fff; padding: 16px; border-radius: var(--radius-md); display: inline-block;">
            <img src="${qrApiUrl}" width="220" height="220" alt="QR Code" style="display: block;">
        </div>
        <p style="font-size: 0.8rem; color: var(--text-muted); margin-top: 12px; word-break: break-all;">
            ${escapeHtml(url)}
        </p>
    `;
    modal.style.display = "flex";
}

function closeQrModal() {
    const modal = document.getElementById("qr-modal");
    if (modal) modal.style.display = "none";
}

// --- Help & Guide Modal ---
function openHelpModal() {
    const modal = document.getElementById("help-modal");
    if (modal) modal.style.display = "flex";
}

function closeHelpModal() {
    const modal = document.getElementById("help-modal");
    if (modal) modal.style.display = "none";
}

// --- Custom Prompt & Confirm Dialogs (Replacing prompt/confirm) ---
function showPromptDialog({ title, label, defaultValue = "", placeholder = "", confirmText = "Save", onConfirm }) {
    const modal = document.getElementById("prompt-modal");
    const titleEl = document.getElementById("prompt-title");
    const labelEl = document.getElementById("prompt-label");
    const inputEl = document.getElementById("prompt-input");
    const submitBtn = document.getElementById("prompt-submit-btn");

    if (!modal || !inputEl) return;

    titleEl.innerText = title;
    labelEl.innerText = label;
    inputEl.value = defaultValue;
    inputEl.placeholder = placeholder;
    submitBtn.innerText = confirmText;

    modal.style.display = "flex";
    setTimeout(() => inputEl.focus(), 50);

    const handleSubmit = () => {
        const val = inputEl.value.trim();
        if (val) {
            modal.style.display = "none";
            submitBtn.removeEventListener("click", handleSubmit);
            inputEl.removeEventListener("keydown", keyHandler);
            onConfirm(val);
        }
    };

    const keyHandler = (e) => {
        if (e.key === "Enter") handleSubmit();
        if (e.key === "Escape") closePromptDialog();
    };

    submitBtn.onclick = handleSubmit;
    inputEl.onkeydown = keyHandler;
}

function closePromptDialog() {
    const modal = document.getElementById("prompt-modal");
    if (modal) modal.style.display = "none";
}

function showConfirmDialog({ title, message, confirmText = "Confirm", danger = true, onConfirm }) {
    const modal = document.getElementById("confirm-modal");
    const titleEl = document.getElementById("confirm-title");
    const msgEl = document.getElementById("confirm-message");
    const btn = document.getElementById("confirm-action-btn");

    if (!modal || !btn) return;

    titleEl.innerText = title;
    msgEl.innerText = message;
    btn.innerText = confirmText;
    btn.className = danger ? "btn btn-danger" : "btn btn-primary";

    modal.style.display = "flex";

    btn.onclick = () => {
        modal.style.display = "none";
        onConfirm();
    };
}

function closeConfirmDialog() {
    const modal = document.getElementById("confirm-modal");
    if (modal) modal.style.display = "none";
}

// --- CRUD Actions ---
function createFolderPrompt() {
    showPromptDialog({
        title: "Create Virtual Folder",
        label: "Folder Name",
        placeholder: "e.g., Documents, Work",
        confirmText: "Create Folder",
        onConfirm: async (name) => {
            try {
                const res = await apiFetch("/api/folders", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ name, parent_id: currentFolderId })
                });
                if (!res.ok) throw new Error("Failed to create folder");
                showToast(`Folder "${name}" created`, "success");
                loadDriveContent();
            } catch (err) {
                showToast(err.message, "error");
            }
        }
    });
}

function promptRenameFolder(id, currentName) {
    showPromptDialog({
        title: "Rename Virtual Folder",
        label: "New Folder Name",
        defaultValue: currentName,
        confirmText: "Rename",
        onConfirm: async (newName) => {
            if (newName === currentName) return;
            try {
                const res = await apiFetch(`/api/folders/${id}`, {
                    method: "PUT",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ name: newName })
                });
                if (!res.ok) throw new Error("Failed to rename folder");
                showToast("Folder renamed", "success");
                loadDriveContent();
            } catch (err) {
                showToast(err.message, "error");
            }
        }
    });
}

function confirmDeleteFolder(id, name) {
    showConfirmDialog({
        title: "Move Folder to Trash",
        message: `"${name}" and its contents move to Trash. You can restore them within Trash or purge them forever.`,
        confirmText: "Move to Trash",
        danger: true,
        onConfirm: async () => {
            try {
                const res = await apiFetch(`/api/folders/${id}`, { method: "DELETE" });
                if (!res.ok) throw new Error("Failed to delete folder");
                showToast(`Folder "${name}" deleted`, "success");
                loadDriveContent();
            } catch (err) {
                showToast(err.message, "error");
            }
        }
    });
}

function promptRenameFile(id, currentName) {
    showPromptDialog({
        title: "Rename Virtual File",
        label: "New File Name",
        defaultValue: currentName,
        confirmText: "Rename",
        onConfirm: async (newName) => {
            if (newName === currentName) return;
            try {
                const res = await apiFetch(`/api/files/${id}`, {
                    method: "PUT",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ name: newName })
                });
                if (!res.ok) throw new Error("Failed to rename file");
                showToast("File renamed", "success");
                loadDriveContent();
            } catch (err) {
                showToast(err.message, "error");
            }
        }
    });
}

function confirmDeleteFile(id, name) {
    showConfirmDialog({
        title: "Move File to Trash",
        message: `"${name}" moves to Trash. You can restore it or purge it forever from there.`,
        confirmText: "Move to Trash",
        danger: true,
        onConfirm: async () => {
            try {
                const res = await apiFetch(`/api/files/${id}`, { method: "DELETE" });
                if (!res.ok) throw new Error("Failed to delete file");
                showToast(`File "${name}" deleted`, "success");
                loadDriveContent();
            } catch (err) {
                showToast(err.message, "error");
            }
        }
    });
}

async function downloadFile(id, name) {
    // Signed expiring ticket: works without ambient cookies and survives
    // session expiry mid-click.
    try {
        const res = await apiFetch(`/api/files/${id}/ticket`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ expires_in: 600 })
        });
        if (res.ok) {
            const data = await res.json();
            if (data.url) {
                window.location.href = data.url;
                return;
            }
        }
    } catch (e) { /* fall through to cookie download */ }
    window.location.href = `/api/files/${id}/download`;
}

// --- Utilities ---
function formatSize(bytes) {
    if (!bytes || bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

function formatDate(dateStr) {
    if (!dateStr) return "";
    const d = new Date(dateStr);
    return d.toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
        year: "numeric"
    });
}

function formatDateTime(dateStr) {
    if (!dateStr) return "";
    const d = new Date(dateStr);
    return d.toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit"
    });
}

function escapeHtml(str) {
    if (!str) return "";
    return String(str)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

// Safe value for an inline `onclick="…"` string: the browser entity-decodes the
// attribute before parsing it as JS, so a raw apostrophe in a filename would
// terminate the string literal. jsq() escapes for the JS layer first, then
// escapeHtml() makes the result safe for the HTML attribute layer.
function ja(str) {
    return escapeHtml(jsq(str));
}

function copyToClipboard(text) {
    navigator.clipboard.writeText(text).then(() => {
        showToast("Copied to clipboard!", "success");
    }).catch(() => {
        showToast("Failed to copy", "error");
    });
}

// --- Mobile Sidebar Setup ---
function setupMobileSidebar() {
    const toggleBtn = document.getElementById("mobile-menu-btn");
    const sidebar = document.getElementById("sidebar");
    const backdrop = document.getElementById("sidebar-backdrop");

    if (toggleBtn && sidebar && backdrop) {
        toggleBtn.addEventListener("click", () => {
            const isOpen = sidebar.classList.toggle("open");
            backdrop.classList.toggle("open", isOpen);
            toggleBtn.setAttribute("aria-expanded", isOpen ? "true" : "false");
        });
        backdrop.addEventListener("click", closeMobileSidebar);
        document.addEventListener("keydown", e => {
            if (e.key === "Escape") closeMobileSidebar();
        });
    }
}

function closeMobileSidebar() {
    const sidebar = document.getElementById("sidebar");
    const backdrop = document.getElementById("sidebar-backdrop");
    const toggleBtn = document.getElementById("mobile-menu-btn");
    if (sidebar) sidebar.classList.remove("open");
    if (backdrop) backdrop.classList.remove("open");
    if (toggleBtn) toggleBtn.setAttribute("aria-expanded", "false");
}

// ================= Overview Folder Cards =================
function renderOverviewFolders(folders) {
    const section = document.getElementById("ov-folders-section");
    const grid = document.getElementById("ov-folders-grid");
    if (!grid) return;

    if (!folders || folders.length === 0) {
        if (section) section.style.display = "none";
        return;
    }
    if (section) section.style.display = "block";

    // Cap the overview to a single tidy row; the rest lives in All Files.
    const hues = ["#2f6bff", "#8b5cf6", "#f472b6", "#10b981", "#f59e0b"];
    const shown = folders.slice(0, 5);
    grid.innerHTML = shown.map((f, i) => {
        const st = folderStats[f.id] || { files: 0, bytes: 0 };
        const hue = hues[i % hues.length];
        const bytesLabel = st.bytes ? formatSize(st.bytes) : "Empty";
        return `
        <div class="folder-card" onclick="navigateTo('${f.id}', '${ja(f.name)}')" data-name="${escapeHtml(f.name)}" oncontextmenu="showCtxMenu(event, 'folder', '${f.id}', this)">
            <div class="folder-card-top">
                <span class="folder-glyph" aria-hidden="true" style="color:${hue};">
                    <svg viewBox="0 0 32 26" fill="none">
                        <path d="M1 5.5A3.5 3.5 0 0 1 4.5 2h5.9a3 3 0 0 1 2.4 1.2l1.3 1.7a3 3 0 0 0 2.4 1.2h10.5A3.5 3.5 0 0 1 30.5 9v11.5A3.5 3.5 0 0 1 27 24H4.5A3.5 3.5 0 0 1 1 20.5V5.5Z" fill="currentColor" opacity=".22"/>
                        <path d="M1 8.5A3.5 3.5 0 0 1 4.5 5h23A3.5 3.5 0 0 1 31 8.5v12A3.5 3.5 0 0 1 27.5 24h-23A3.5 3.5 0 0 1 1 20.5v-12Z" fill="currentColor"/>
                    </svg>
                </span>
                <button class="folder-kebab" title="More actions" onclick="event.stopPropagation(); showCtxMenu(event, 'folder', '${f.id}', this.closest('.folder-card'))">⋯</button>
            </div>
            <div class="folder-card-title" title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</div>
            <div class="folder-card-meta"><span>${bytesLabel}</span><span> · ${st.files} files</span></div>
        </div>`;
    }).join("");
}

// ================= Overview Dashboard =================
async function loadOverview() {
    skeletonCards(document.getElementById("ov-stat-cards"), 4);
    const _rl = document.getElementById("ov-recent-list");
    if (_rl) _rl.innerHTML = '<div class="skel skel-row"></div><div class="skel skel-row"></div><div class="skel skel-row"></div>';
    const _fg = document.getElementById("ov-folders-grid");
    if (_fg) _fg.innerHTML = '<div class="skel skel-card"></div><div class="skel skel-card"></div><div class="skel skel-card"></div>';
    try {
        const res = await apiFetch(`/api/stats?limit=${ovRecentLimit}`);
        if (!res.ok) throw new Error("stats failed");
        const s = await res.json();

        // Fetch folders for the "My Folders" section
        try {
            const [foldersRes, statsRes] = await Promise.all([
                apiFetch("/api/folders"),
                apiFetch("/api/folders/stats")
            ]);
            if (foldersRes.ok) {
                const folders = await foldersRes.json() || [];
                if (statsRes.ok) {
                    const stats = await statsRes.json() || [];
                    folderStats = {};
                    stats.forEach(st => { folderStats[st.id] = st; });
                }
                renderOverviewFolders(folders);
            }
        } catch (e) { /* folders are decorative */ }

        const cards = document.getElementById("ov-stat-cards");
        if (cards) {
            cards.innerHTML = `
                <div class="stat-card stat-card-hero">
                    <div class="hero-top">
                        <span class="hero-main">${getIcon("folder")}<span class="stat-value">${s.files}</span></span>
                        <span class="hero-cloud"><svg class="icon" viewBox="0 0 24 24"><path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/></svg></span>
                    </div>
                    <span class="stat-label">Files · ${s.folders} folders</span>
                    <span class="hero-foot"><span class="hero-quota">${formatSize(s.bytes)} stored</span><span class="hero-pill">Telegram Cloud</span></span>
                </div>
                <div class="stat-card"><span class="stat-label">Stored</span><span class="stat-value">${formatSize(s.bytes)}</span><span class="stat-sub">Telegram Cloud</span></div>
                <div class="stat-card"><span class="stat-label">Favorites</span><span class="stat-value">${s.favorites}</span><span class="stat-sub">starred files</span></div>
                <div class="stat-card"><span class="stat-label">Trash</span><span class="stat-value">${s.trashed_files}</span><span class="stat-sub">${s.versions} versions kept</span></div>
            `;
        }

        const usedEl = document.getElementById("ov-used");
        const totalQuota = 50 * 1024 * 1024 * 1024;
        const usedFrac = Math.min(1, (s.bytes || 0) / totalQuota);
        if (usedEl) usedEl.innerText = formatSize(s.bytes);
        const gauge = document.getElementById("ov-gauge");
        if (gauge) {
            const cats = s.breakdown || [];
            const total = cats.reduce((a, c) => a + c.bytes, 0) || 1;
            const colors = { image: "#2f6bff", video: "#8b5cf6", audio: "#f472b6", document: "#10b981", other: "#f59e0b" };
            // Reference storage ring: full track plus one coloured arc per
            // stored category, drawn back-to-back around the circle.
            const R = 48, CX = 60, CY = 60;
            const pt = frac => {
                const a = (-90 + frac * 360) * Math.PI / 180;
                return [CX + R * Math.cos(a), CY + R * Math.sin(a)];
            };
            const arc = (f0, f1) => {
                const [x0, y0] = pt(f0), [x1, y1] = pt(f1);
                const large = (f1 - f0) * 360 > 180 ? 1 : 0;
                return `M ${x0.toFixed(1)} ${y0.toFixed(1)} A ${R} ${R} 0 ${large} 1 ${x1.toFixed(1)} ${y1.toFixed(1)}`;
            };
            let acc = 0, segs = "";
            cats.forEach(c => {
                const frac = c.bytes / total;
                if (frac > 0.002) segs += `<path d="${arc(acc, acc + frac)}" stroke="${colors[c.category] || "#94a3b8"}" stroke-width="12" fill="none" stroke-linecap="round"/>`;
                acc += frac;
            });
            const usedArc = usedFrac > 0.002
                ? `<path d="${arc(0, usedFrac)}" stroke="url(#ovGaugeGrad)" stroke-width="12" fill="none" stroke-linecap="round" opacity="0.35"/>`
                : "";
            gauge.innerHTML = `
                <defs><linearGradient id="ovGaugeGrad" x1="0" y1="0" x2="1" y2="1">
                    <stop offset="0" stop-color="#4f8bff"/><stop offset="1" stop-color="#2f6bff"/>
                </linearGradient></defs>
                <path d="${arc(0, 1)}" stroke="var(--track)" stroke-width="12" fill="none" stroke-linecap="round"/>${usedArc}${segs}`;
        }

        const bd = document.getElementById("ov-breakdown");
        if (bd) {
            const cats = s.breakdown || [];
            const labels = { image: "Images", video: "Videos", audio: "Audio", document: "Documents", other: "Other files" };
            const dots = { image: "#2f6bff", video: "#8b5cf6", audio: "#f472b6", document: "#f59e0b", other: "#94a3b8" };
            bd.innerHTML = cats.length === 0
                ? `<p style="font-size:0.8rem;color:var(--text-muted);">Nothing stored yet.</p>`
                : cats.map(c => `
                    <div class="cat-item">
                        <span class="cat-dot" style="background: ${dots[c.category] || "#94a3b8"};"></span>
                        <span class="cat-name">${labels[c.category] || c.category}</span>
                        <span class="cat-bytes">${formatSize(c.bytes)}</span>
                    </div>`).join("");
        }

        const recent = document.getElementById("ov-recent-list");
        if (recent) {
            const files = s.recent || [];
            recent.innerHTML = files.length === 0
                ? `<p style="font-size:0.8rem;color:var(--text-muted);">No files yet — upload something.</p>`
                : files.map(f => {
                    const cat = getFileCategory(f.mime_type, f.name);
                    return `
                    <div class="recent-row" onclick="openPreview('${f.id}', '${ja(f.name)}', '${f.mime_type}', ${f.size})">
                        <span class="type-tile ${cat.tile}">${getIcon(cat.icon, "icon-sm")}</span>
                        <span class="recent-file-name"><span class="rname" title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</span><span class="recent-file-kind">${cat.category.charAt(0).toUpperCase() + cat.category.slice(1)} · ${(f.name.split('.').pop() || '').toUpperCase()}</span></span>
                        <span class="rmeta">${formatDate(f.updated_at || f.created_at)}</span>
                        <span class="rmeta">${formatSize(f.size)}</span>
                        <button class="recent-star ${favoriteIds.has(f.id) ? 'is-favorite' : ''}" onclick="event.stopPropagation(); toggleFavorite('${f.id}', ${!favoriteIds.has(f.id)})" title="Favorite">${getIcon("star", "icon-sm")}</button>
                        <button class="btn-icon" style="width:30px;height:30px;" title="More actions" onclick="event.stopPropagation(); showCtxMenu(event, 'file', '${f.id}', this)">${getIcon("more-vertical", "icon-sm")}</button>
                    </div>`;
                }).join("") + (files.length >= ovRecentLimit
                        ? `<button class="load-more-btn" onclick="ovRecentLimit += 12; loadOverview();">Load More ▾</button>`
                        : "");
        }

        const dupBox = document.getElementById("ov-duplicates");
        if (dupBox) {
            try {
                const dres = await apiFetch("/api/duplicates");
                const groups = await dres.json() || [];
                dupBox.innerHTML = groups.length === 0
                    ? `<p style="font-size:0.8rem;color:var(--text-muted);">No duplicate content detected.</p>`
                    : groups.slice(0, 5).map(g => `
                        <div class="recent-row" onclick="viewDuplicateGroup('${g.sha256}')">
                            <span style="color: var(--warning);">${getIcon("copy")}</span>
                            <span class="rname" title="${g.sha256}">${g.count} identical files</span>
                            <span class="rmeta">${formatSize(g.bytes)} wasted</span>
                        </div>`).join("");
            } catch (e) { /* optional */ }
        }
    } catch (err) {
        showToast("Overview failed: " + err.message, "error");
    }
}

async function viewDuplicateGroup(sha) {
    try {
        const res = await apiFetch(`/api/duplicates?sha=${encodeURIComponent(sha)}`);
        const files = await res.json() || [];
        showConfirmDialog({
            title: "Duplicate Files",
            message: files.map(f => `• ${f.name} (${formatSize(f.size)})`).join("\n") + "\n\nMove all copies except the newest to Trash?",
            confirmText: "Trash Duplicates",
            danger: true,
            onConfirm: async () => {
                const sorted = [...files].sort((a, b) => new Date(b.updated_at || b.created_at) - new Date(a.updated_at || a.created_at));
                const ids = sorted.slice(1).map(f => f.id);
                if (ids.length === 0) return;
                await apiFetch("/api/files/bulk", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ action: "trash", ids })
                });
                showToast(`Moved ${ids.length} duplicate(s) to Trash`, "success");
                loadOverview();
            }
        });
    } catch (err) {
        showToast(err.message, "error");
    }
}

// ================= Favorites =================
async function toggleFavorite(id, makeFav) {
    const had = favoriteIds.has(id);
    if (makeFav) favoriteIds.add(id); else favoriteIds.delete(id);
    if (currentTab === "favorites") loadFavorites();
    else if (currentTab === "media") loadMediaLibrary();
    else renderActiveView();
    try {
        const res = await apiFetch(`/api/files/${id}/favorite`, { method: makeFav ? "POST" : "DELETE" });
        if (!res.ok) throw new Error("Favorite update failed");
        showToast(makeFav ? "Added to favorites" : "Removed from favorites", "success");
    } catch (err) {
        if (had) favoriteIds.add(id); else favoriteIds.delete(id);
        if (currentTab === "favorites") loadFavorites();
        else if (currentTab === "media") loadMediaLibrary();
        else renderActiveView();
        showToast(err.message, "error");
    }
}

async function loadFavorites() {
    skeletonCards(document.getElementById("favorites-grid"), 6);
    try {
        const res = await apiFetch("/api/favorites");
        const files = await res.json() || [];
        const grid = document.getElementById("favorites-grid");
        const empty = document.getElementById("empty-favorites-state");
        if (!grid) return;
        if (files.length === 0) {
            grid.innerHTML = "";
            if (empty) empty.style.display = "flex";
            return;
        }
        if (empty) empty.style.display = "none";
        grid.innerHTML = files.map(f => fileCardHTML(f, { selectable: false })).join("");
    } catch (err) {
        showToast("Favorites failed: " + err.message, "error");
    }
}

// ================= Trash =================
async function loadTrash() {
    skeletonCards(document.getElementById("trash-files-grid"), 4);
    skeletonCards(document.getElementById("trash-folders-grid"), 4);
    try {
        const res = await apiFetch("/api/trash");
        const data = await res.json() || { files: [], folders: [] };
        const files = data.files || [], folders = data.folders || [];

        const fGrid = document.getElementById("trash-files-grid");
        const fSec = document.getElementById("trash-files-section");
        const foGrid = document.getElementById("trash-folders-grid");
        const foSec = document.getElementById("trash-folders-section");
        const empty = document.getElementById("empty-trash-state");

        if (foGrid && foSec) {
            if (folders.length === 0) { foSec.style.display = "none"; }
            else {
                foSec.style.display = "block";
                foGrid.innerHTML = folders.map(f => `
                    <div class="card">
                        <div class="card-top">
                            <div class="card-icon-wrap tile-blue">${getIcon("folder")}</div>
                            <div class="card-actions">
                                <button class="btn-icon" title="Restore" onclick="restoreTrashItem('folder', '${f.id}')">${getIcon("rotate-ccw", "icon-sm")}</button>
                                <button class="btn-icon" title="Purge forever" onclick="purgeTrashItem('folder', '${f.id}', '${ja(f.name)}')">${getIcon("trash-2", "icon-sm")}</button>
                            </div>
                        </div>
                        <div><div class="card-title">${escapeHtml(f.name)}</div><div class="card-meta">Trashed folder</div></div>
                    </div>`).join("");
            }
        }
        if (fGrid && fSec) {
            if (files.length === 0) { fSec.style.display = "none"; }
            else {
                fSec.style.display = "block";
                fGrid.innerHTML = files.map(f => `
                    <div class="card">
                        <div class="card-top">
                            <div class="card-icon-wrap ${getFileCategory(f.mime_type, f.name).tile}">${getIcon(getFileCategory(f.mime_type, f.name).icon)}</div>
                            <div class="card-actions">
                                <button class="btn-icon" title="Restore" onclick="restoreTrashItem('file', '${f.id}')">${getIcon("rotate-ccw", "icon-sm")}</button>
                                <button class="btn-icon" title="Purge forever" onclick="purgeTrashItem('file', '${f.id}', '${ja(f.name)}')">${getIcon("trash-2", "icon-sm")}</button>
                            </div>
                        </div>
                        <div><div class="card-title">${escapeHtml(f.name)}</div><div class="card-meta">${formatSize(f.size)} &bull; ${escapeHtml(f.folder_name || "Drive")}</div></div>
                    </div>`).join("");
            }
        }
        if (empty) empty.style.display = (files.length === 0 && folders.length === 0) ? "flex" : "none";
    } catch (err) {
        showToast("Trash failed: " + err.message, "error");
    }
}

async function restoreTrashItem(kind, id) {
    try {
        const res = await apiFetch("/api/trash/restore", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ kind, id })
        });
        if (!res.ok) throw new Error("Restore failed");
        showToast("Restored", "success");
        loadTrash();
    } catch (err) {
        showToast(err.message, "error");
    }
}

function purgeTrashItem(kind, id, name) {
    showConfirmDialog({
        title: "Purge Forever",
        message: `"${name}" will be permanently deleted, including its Telegram copy. This cannot be undone.`,
        confirmText: "Purge Forever",
        danger: true,
        onConfirm: async () => {
            try {
                const res = await apiFetch(`/api/trash/${kind}/${id}`, { method: "DELETE" });
                if (!res.ok) throw new Error("Purge failed");
                showToast("Purged permanently", "success");
                loadTrash();
            } catch (err) {
                showToast(err.message, "error");
            }
        }
    });
}

function emptyTrashConfirm() {
    showConfirmDialog({
        title: "Empty Trash",
        message: "Permanently delete everything in Trash, including Telegram copies? This cannot be undone.",
        confirmText: "Empty Trash",
        danger: true,
        onConfirm: async () => {
            try {
                const res = await apiFetch("/api/trash/empty", { method: "POST" });
                if (!res.ok) throw new Error("Empty trash failed");
                showToast("Trash emptied", "success");
                loadTrash();
            } catch (err) {
                showToast(err.message, "error");
            }
        }
    });
}

// ================= Sync Center =================
async function loadStorageCenter() {
    skeletonRows(document.getElementById("sync-runs-body"), 4);
    try {
        const [healthRes, runsRes] = await Promise.all([
            apiFetch("/api/health"),
            apiFetch("/api/sync/runs?limit=20")
        ]);
        const health = await healthRes.json();
        const runs = await runsRes.json() || [];

        // Verified backend state, not just configuration presence.
        const stateInfo = await refreshTelegramState(false);
        const state = (stateInfo && stateInfo.state) || "error";
        const info = tgStateInfo(state);

        const box = document.getElementById("storage-health");
        if (box) {
            const okDot = ok => `<span class="status-dot" style="background: ${ok ? "var(--success)" : "var(--text-muted)"}; animation: none;"></span>`;
            box.innerHTML = `
                <div class="stat-card"><span class="stat-label">Storage Backend</span><span style="display:flex;align-items:center;gap:8px;font-weight:700;"><span class="status-dot" style="background:${info.color};animation:none;"></span>${escapeHtml(info.short)}</span></div>
                <div class="stat-card"><span class="stat-label">Telegram Session</span><span style="display:flex;align-items:center;gap:8px;font-weight:700;">${okDot(state === "connected")} ${state === "connected" ? "Verified" : (health.telegram_authorized ? "Stored, unverified" : "Missing")}</span></div>
                <div class="stat-card"><span class="stat-label">Storage Channel</span><span style="display:flex;align-items:center;gap:8px;font-weight:700;">${okDot(state === "connected" && health.storage_channel)} ${state === "connected" ? (health.storage_channel ? "Bound" : "Unbound") : "Unverified"}</span></div>
                <div class="stat-card"><span class="stat-label">Database</span><span class="stat-value" style="font-size:1.1rem;">${formatSize(health.db_bytes || 0)}</span></div>
                <div class="stat-card"><span class="stat-label">Uptime</span><span class="stat-value" style="font-size:1.1rem;">${formatDuration(health.uptime_seconds || 0)}</span><span class="stat-sub">v${health.version || ""}</span></div>
            `;
        }

        // Surface the honest backend state at the top of the Sync Center so the
        // tab never looks like a healthy empty sync history.
        const notice = document.getElementById("sync-backend-notice");
        if (notice) {
            if (state === "connected") {
                notice.style.display = "none";
                notice.innerHTML = "";
            } else {
                notice.style.display = "";
                notice.innerHTML = `
                    <div class="tg-unavailable">
                        <span class="tg-state-dot" style="background:${info.color};"></span>
                        <div>
                            <div class="tg-unavailable-title">Telegram Storage — ${escapeHtml(info.short)}</div>
                            <p class="tg-unavailable-text">${escapeHtml((stateInfo && stateInfo.message) || "The storage backend could not be verified.")}</p>
                            <p class="tg-unavailable-hint">Sync and Snapshots are unavailable until the backend is connected. Files already catalogued stay browsable.</p>
                        </div>
                    </div>`;
            }
        }

        const tbody = document.getElementById("sync-runs-body");
        const empty = document.getElementById("empty-sync-runs-state");
        if (tbody) {
            if (runs.length === 0) {
                tbody.innerHTML = "";
                if (empty) empty.style.display = "flex";
            } else {
                if (empty) empty.style.display = "none";
                tbody.innerHTML = runs.map(r => `
                    <tr>
                        <td style="font-family: monospace;">#${r.id}</td>
                        <td style="color: var(--text-muted);">${formatDateTime(r.started_at)}</td>
                        <td>${escapeHtml(r.trigger || "-")}</td>
                        <td>${r.scanned}</td>
                        <td>${r.inserted + r.updated}</td>
                        <td>${r.error
                            ? `<span style="color: var(--danger); font-size: 0.78rem;">${escapeHtml(r.error)}</span>`
                            : `<span style="color: var(--success); font-size: 0.8rem; font-weight: 700;">OK</span>`}</td>
                    </tr>`).join("");
            }
        }
    } catch (err) {
        showToast("Sync Center failed: " + err.message, "error");
    }
}

async function syncCenterNow() {
    const btn = document.getElementById("btn-sync-now");
    const txt = document.getElementById("sync-center-text");
    if (btn) btn.disabled = true;
    if (txt) txt.innerText = "Syncing...";
    try {
        const res = await apiFetch("/api/sync", { method: "POST" });
        if (!res.ok) {
            const t = await res.text().catch(() => "");
            throw new Error(t || "Sync failed");
        }
        const data = await res.json();
        showToast(`Sync done: ${data.inserted} recovered, ${data.updated} refreshed.`, "success", 5000);
        await loadStorageCenter();
        if (currentTab === "drive") loadDriveContent();
    } catch (err) {
        showToast("Sync error: " + err.message, "error", 6000);
    } finally {
        if (btn) btn.disabled = false;
        if (txt) txt.innerText = "Sync Now";
    }
}

async function verifyStorageNow() {
    const btn = document.getElementById("btn-verify-now");
    const txt = document.getElementById("verify-text");
    const box = document.getElementById("verify-results");
    if (btn) btn.disabled = true;
    if (txt) txt.innerText = "Verifying...";
    if (box) box.innerHTML = `<p style="font-size:0.8rem;color:var(--text-muted);">Re-resolving recent files against the Storage Channel…</p>`;
    try {
        const res = await apiFetch("/api/storage/verify", { method: "POST" });
        if (!res.ok) {
            const t = await res.text().catch(() => "");
            throw new Error(t || "Verification failed");
        }
        const data = await res.json();
        const fails = data.failed || [];
        if (box) {
            box.innerHTML = `
                <div class="stat-cards">
                    <div class="stat-card"><span class="stat-label">Checked</span><span class="stat-value">${data.checked}</span></div>
                    <div class="stat-card"><span class="stat-label">Healthy</span><span class="stat-value" style="color:var(--success);">${data.ok}</span></div>
                    <div class="stat-card"><span class="stat-label">Broken</span><span class="stat-value" style="color:${fails.length ? "var(--danger)" : "var(--success)"};">${fails.length}</span></div>
                </div>
                ${fails.length ? `<div style="margin-top:10px;font-size:0.8rem;">` + fails.slice(0, 10).map(f => `
                    <div class="version-row"><span style="color:var(--danger);">${getIcon("alert-circle", "icon-sm")}</span>
                    <span style="flex:1;">${escapeHtml(f.name)}</span>
                    <button class="btn btn-secondary" style="padding:4px 10px;font-size:0.72rem;" onclick="openDetailsDrawer('${f.id}')">Details</button>
                    </div>`).join("") + `</div>` : `<p style="font-size:0.8rem;color:var(--success);margin-top:8px;">All checked objects are retrievable.</p>`}`;
        }
        showToast(`Verification: ${data.ok}/${data.checked} healthy`, fails.length ? "info" : "success", 5000);
    } catch (err) {
        if (box) box.innerHTML = "";
        showToast("Verify error: " + err.message, "error", 6000);
    } finally {
        if (btn) btn.disabled = false;
        if (txt) txt.innerText = "Verify Storage";
    }
}

function formatDuration(secs) {
    secs = Math.max(0, Math.floor(secs));
    const d = Math.floor(secs / 86400), h = Math.floor(secs % 86400 / 3600), m = Math.floor(secs % 3600 / 60);
    if (d > 0) return `${d}d ${h}h`;
    if (h > 0) return `${h}h ${m}m`;
    if (m > 0) return `${m}m`;
    return `${secs}s`;
}

// ================= Settings =================
async function loadSettings() {
    loadSessionsList();
    loadTokensList();
    loadAuditLog();
    try {
        const res = await apiFetch("/api/health");
        const h = await res.json();
        const box = document.getElementById("health-box");
        if (box) {
            box.innerHTML = `
                <div class="kv-grid">
                    <dt>Version</dt><dd>tDocs v${h.version || "?"}</dd>
                    <dt>Uptime</dt><dd>${formatDuration(h.uptime_seconds || 0)}</dd>
                    <dt>Database</dt><dd>${formatSize(h.db_bytes || 0)}</dd>
                    <dt>Memory</dt><dd>${formatSize(h.memory_alloc_bytes || 0)}</dd>
                    <dt>Goroutines</dt><dd>${h.goroutines || 0}</dd>
                    <dt>Telegram</dt><dd>${h.telegram_session ? "paired" : "MISSING"} / channel ${h.storage_channel ? "bound" : "UNBOUND"}</dd>
                    <dt>Sessions</dt><dd>${h.sessions || 0} active</dd>
                </div>`;
        }
    } catch (err) {
        showToast("Health failed: " + err.message, "error");
    }
}

async function changeAdminPassword() {
    return withBtnLoading(document.getElementById("password-update-btn"), async () => {
    const cur = document.getElementById("set-cur-pass").value;
    const neu = document.getElementById("set-new-pass").value;
    if (!cur || !neu) {
        showToast("Fill both password fields", "error");
        return;
    }
    try {
        const res = await apiFetch("/api/settings/password", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ current: cur, new: neu })
        });
        if (!res.ok) {
            const t = await res.text().catch(() => "");
            throw new Error(t || "Password change failed");
        }
        document.getElementById("set-cur-pass").value = "";
        document.getElementById("set-new-pass").value = "";
        showToast("Admin password updated", "success");
    } catch (err) {
        showToast(err.message, "error");
    }
    });
}

async function loadSessionsList() {
    try {
        const res = await apiFetch("/api/sessions");
        const list = await res.json() || [];
        const box = document.getElementById("sessions-list");
        if (!box) return;
        box.innerHTML = list.length === 0
            ? `<p style="font-size:0.8rem;color:var(--text-muted);">No sessions.</p>`
            : list.map(s => `
                <div class="session-row">
                    <span style="color: var(--primary);">${getIcon("user", "icon-sm")}</span>
                    <div style="flex:1;min-width:0;">
                        <div style="font-weight:700;font-size:0.8rem;">${s.current ? "This browser" : "Session"} <span style="font-family:monospace;color:var(--text-muted);">${escapeHtml(s.id)}</span></div>
                        <div style="font-size:0.72rem;color:var(--text-muted);">${escapeHtml(s.ip || "")} &bull; last seen ${formatDateTime(s.last_seen)}</div>
                    </div>
                    ${s.current ? "" : `<button class="btn-icon btn-danger" style="width:28px;height:28px;" title="Revoke" onclick="revokeSession('${s.id}')">${getIcon("x", "icon-sm")}</button>`}
                </div>`).join("");
    } catch (err) {
        showToast("Sessions failed: " + err.message, "error");
    }
}

async function revokeSession(id) {
    try {
        const res = await apiFetch(`/api/sessions/${id}`, { method: "DELETE" });
        if (!res.ok) throw new Error("Revoke failed");
        showToast("Session revoked", "success");
        loadSessionsList();
    } catch (err) {
        showToast(err.message, "error");
    }
}

async function loadTokensList() {
    try {
        const res = await apiFetch("/api/tokens");
        const list = await res.json() || [];
        const box = document.getElementById("tokens-list");
        if (!box) return;
        box.innerHTML = list.length === 0
            ? `<p style="font-size:0.8rem;color:var(--text-muted);">No API tokens yet.</p>`
            : list.map(t => `
                <div class="token-row">
                    <span style="color: var(--primary);">${getIcon("shield", "icon-sm")}</span>
                    <div style="flex:1;min-width:0;">
                        <div style="font-weight:700;font-size:0.8rem;">${escapeHtml(t.name)} ${t.revoked_at ? '<span style="color:var(--danger);">(revoked)</span>' : ""}</div>
                        <div style="font-size:0.72rem;color:var(--text-muted);font-family:monospace;">${escapeHtml(t.prefix)}… &bull; last used ${t.last_used_at ? formatDateTime(t.last_used_at) : "never"}</div>
                    </div>
                    ${t.revoked_at ? "" : `<button class="btn btn-secondary" style="padding:4px 10px;font-size:0.72rem;" onclick="revokeToken('${t.id}')">Revoke</button>`}
                    <button class="btn-icon btn-danger" style="width:28px;height:28px;" title="Delete" onclick="deleteToken('${t.id}', '${ja(t.name)}')">${getIcon("trash-2", "icon-sm")}</button>
                </div>`).join("");
    } catch (err) {
        showToast("Tokens failed: " + err.message, "error");
    }
}

async function createApiToken() {
    return withBtnLoading(document.getElementById("token-create-btn"), async () => {
    const nameInput = document.getElementById("new-token-name");
    const name = (nameInput.value || "").trim();
    if (!name) {
        showToast("Give the token a name first", "error");
        return;
    }
    try {
        const res = await apiFetch("/api/tokens", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ name })
        });
        if (!res.ok) {
            const t = await res.text().catch(() => "");
            throw new Error(t || "Token creation failed");
        }
        const data = await res.json();
        document.getElementById("new-token-secret-input").value = data.token;
        document.getElementById("new-token-secret").style.display = "block";
        nameInput.value = "";
        showToast("Token created — copy it now", "success", 6000);
        loadTokensList();
    } catch (err) {
        showToast(err.message, "error");
    }
    });
}

async function revokeToken(id) {
    try {
        const res = await apiFetch(`/api/tokens/${id}`, { method: "DELETE" });
        if (!res.ok) throw new Error("Revoke failed");
        showToast("Token revoked", "success");
        loadTokensList();
    } catch (err) {
        showToast(err.message, "error");
    }
}

function deleteToken(id, name) {
    showConfirmDialog({
        title: "Delete API Token",
        message: `Permanently delete token "${name}"? Scripts using it will break immediately.`,
        confirmText: "Delete Token",
        danger: true,
        onConfirm: async () => {
            try {
                const res = await apiFetch(`/api/tokens/${id}/purge`, { method: "DELETE" });
                if (!res.ok) throw new Error("Delete failed");
                showToast("Token deleted", "success");
                loadTokensList();
            } catch (err) {
                showToast(err.message, "error");
            }
        }
    });
}

async function loadAuditLog() {
    try {
        const filter = document.getElementById("audit-filter");
        const action = filter ? filter.value : "";
        const res = await apiFetch(`/api/audit?limit=100${action ? "&action=" + encodeURIComponent(action) : ""}`);
        const list = await res.json() || [];
        const tbody = document.getElementById("audit-body");
        if (!tbody) return;
        tbody.innerHTML = list.length === 0
            ? `<tr><td colspan="4" style="color:var(--text-muted);">No audit entries.</td></tr>`
            : list.map(e => `
                <tr>
                    <td style="color:var(--text-muted);white-space:nowrap;">${formatDateTime(e.created_at)}</td>
                    <td><span style="font-family:monospace;font-size:0.78rem;">${escapeHtml(e.action)}</span></td>
                    <td style="font-size:0.8rem;word-break:break-word;">${escapeHtml(e.detail || "")}</td>
                    <td style="color:var(--text-muted);">${escapeHtml(e.ip || "")}</td>
                </tr>`).join("");
    } catch (err) {
        showToast("Audit log failed: " + err.message, "error");
    }
}

// ================= Context menu =================
function setupContextMenu() {
    document.addEventListener("click", hideContextMenu);
    document.addEventListener("scroll", hideContextMenu, true);
    window.addEventListener("resize", hideContextMenu);
}

function hideContextMenu() {
    const m = document.getElementById("ctx-menu");
    if (m) m.style.display = "none";
}

function jsq(s) {
    return String(s || "").replace(/\\/g, "\\\\").replace(/'/g, "\\'");
}

function showCtxMenu(e, kind, id, el) {
    e.preventDefault();
    e.stopPropagation();
    const m = document.getElementById("ctx-menu");
    if (!m) return;
    const rawName = (el && el.dataset && el.dataset.name) || id;
    const q = jsq(rawName);
    const items = kind === "folder" ? [
        { label: "Open", icon: "folder", fn: `navigateTo('${id}', '${q}')` },
        { label: "Rename", icon: "edit", fn: `promptRenameFolder('${id}', '${q}')` },
        { label: "Move to Trash", icon: "trash-2", fn: `confirmDeleteFolder('${id}', '${q}')`, danger: true }
    ] : [
        { label: "Preview", icon: "eye", fn: `ctxPreview('${id}')` },
        { label: "Download", icon: "download", fn: `downloadFile('${id}')` },
        { label: "Details", icon: "info", fn: `openDetailsDrawer('${id}')` },
        { label: "Share Link", icon: "share-2", fn: `openShareModal('${id}', '${q}')` },
        { label: "Star / Unstar", icon: "star", fn: `ctxToggleFav('${id}')` },
        { label: "Make a copy", icon: "copy", fn: `copyFile('${id}')` },
        { label: "Rename", icon: "edit", fn: `promptRenameFile('${id}', '${q}')` },
        { label: "Move to Trash", icon: "trash-2", fn: `confirmDeleteFile('${id}', '${q}')`, danger: true }
    ];
    m.innerHTML = `<div class="dropdown-title">${escapeHtml(rawName)}</div>` + items.map(i => `
        <button class="dropdown-item ${i.danger ? "danger" : ""}" onclick="hideContextMenu(); ${i.fn}">${getIcon(i.icon, "icon-sm")}<span>${i.label}</span></button>
    `).join("");
    m.style.display = "block";
    const w = 230, h = items.length * 42 + 40;
    m.style.left = Math.min(e.clientX, window.innerWidth - w - 8) + "px";
    m.style.top = Math.min(e.clientY, window.innerHeight - h - 8) + "px";
}

// ================= Details drawer + versions =================
async function openDetailsDrawer(id) {
    const drawer = document.getElementById("details-drawer");
    const body = document.getElementById("details-body");
    const title = document.getElementById("details-title");
    const sub = document.getElementById("details-sub");
    if (!drawer || !body) return;
    drawer.style.display = "flex";
    // The drawer slides via the .open class (see style.css); display alone
    // would leave it parked off-screen behind its translateX(100%) default.
    drawer.classList.add("open");
    body.innerHTML = `<p style="color:var(--text-muted);font-size:0.85rem;">Loading...</p>`;
    try {
        const res = await apiFetch(`/api/files/${id}/meta`);
        if (!res.ok) throw new Error("Details unavailable");
        const meta = await res.json();
        const f = meta.file;
        if (title) title.innerText = f.name;
        if (sub) sub.innerText = `${formatSize(f.size)} • ${f.mime_type}`;
        const versions = meta.versions || [];
        body.innerHTML = `
            <div style="display:flex;gap:8px;flex-wrap:wrap;">
                <button class="btn btn-primary" style="flex:1;padding:8px;" onclick="downloadFile('${f.id}')">Download</button>
                <button class="btn btn-secondary" style="flex:1;padding:8px;" onclick="openShareModal('${f.id}', '${ja(f.name)}')">Share</button>
                <button class="btn btn-secondary" style="padding:8px 12px;" onclick="toggleFavorite('${f.id}', ${!meta.favorite})" title="Star">${getIcon("star", "icon-sm")}</button>
            </div>
            <dl class="kv-grid">
                <dt>Size</dt><dd>${formatSize(f.size)}</dd>
                <dt>Type</dt><dd>${escapeHtml(f.mime_type)}</dd>
                <dt>Modified</dt><dd>${formatDateTime(f.updated_at || f.created_at)}</dd>
                <dt>SHA-256</dt><dd>${f.sha256 ? escapeHtml(f.sha256) : "—"}</dd>
                <dt>Telegram</dt><dd>msg #${f.telegram_message_id}</dd>
                <dt>Shares</dt><dd>${meta.shares}</dd>
                <dt>Duplicates</dt><dd>${meta.duplicates > 0 ? meta.duplicates + " identical file(s)" : "none"}</dd>
            </dl>
            <div class="section-title">Versions (${versions.length})</div>
            <div id="details-versions">
                ${versions.length === 0
                    ? `<p style="font-size:0.8rem;color:var(--text-muted);">No older versions. Re-uploading a file with the same name archives the previous copy here.</p>`
                    : versions.map(v => `
                        <div class="version-row">
                            <span style="color:var(--primary);">${getIcon("clock", "icon-sm")}</span>
                            <div style="flex:1;min-width:0;">
                                <div style="font-weight:700;">${formatSize(v.size)}</div>
                                <div style="font-size:0.72rem;color:var(--text-muted);">${formatDateTime(v.created_at)}</div>
                            </div>
                            <button class="btn btn-secondary" style="padding:4px 10px;font-size:0.72rem;" onclick="restoreVersion('${f.id}', '${v.id}')">Restore</button>
                            <button class="btn-icon btn-danger" style="width:28px;height:28px;" onclick="deleteVersion('${f.id}', '${v.id}')">${getIcon("trash-2", "icon-sm")}</button>
                        </div>`).join("")}
            </div>`;
    } catch (err) {
        body.innerHTML = `<p style="color:var(--danger);font-size:0.85rem;">${escapeHtml(err.message)}</p>`;
    }
}

function closeDetailsDrawer() {
    const drawer = document.getElementById("details-drawer");
    if (drawer) {
        drawer.classList.remove("open");
        drawer.style.display = "none";
    }
}

async function restoreVersion(fileId, vid) {
    try {
        const res = await apiFetch(`/api/files/${fileId}/versions/${vid}/restore`, { method: "POST" });
        if (!res.ok) throw new Error("Restore failed");
        showToast("Version restored", "success");
        openDetailsDrawer(fileId);
        loadDriveContent();
    } catch (err) {
        showToast(err.message, "error");
    }
}

async function deleteVersion(fileId, vid) {
    try {
        const res = await apiFetch(`/api/files/${fileId}/versions/${vid}`, { method: "DELETE" });
        if (!res.ok) throw new Error("Delete failed");
        showToast("Version deleted", "success");
        openDetailsDrawer(fileId);
    } catch (err) {
        showToast(err.message, "error");
    }
}

// ================= Bulk selection =================
function selectedFileIds() {
    return [...document.querySelectorAll("#drive-content-container .select-check:checked")].map(el => el.dataset.id);
}

function onSelectChange() {
    document.querySelectorAll("#drive-content-container .select-check[data-id]").forEach(el => {
        if (el.checked) selectedIds.add(el.dataset.id);
        else selectedIds.delete(el.dataset.id);
    });
    renderBulkBar();
}

function toggleSelectAll(box) {
    document.querySelectorAll("#drive-content-container .select-check[data-id]").forEach(el => {
        el.checked = box.checked;
        if (box.checked) selectedIds.add(el.dataset.id);
        else selectedIds.delete(el.dataset.id);
    });
    renderBulkBar();
}

function clearSelection() {
    selectedIds.clear();
    document.querySelectorAll("#drive-content-container .select-check").forEach(el => { el.checked = false; });
    renderBulkBar();
}

function renderBulkBar() {
    const bar = document.getElementById("bulk-bar");
    const count = document.getElementById("bulk-count");
    if (!bar) return;
    if (selectedIds.size === 0) {
        bar.style.display = "none";
        return;
    }
    bar.style.display = "flex";
    if (count) count.innerText = `${selectedIds.size} selected`;
}

async function bulkAction(action, folderId = null) {
    const ids = [...selectedIds];
    if (ids.length === 0) return;
    try {
        const res = await apiFetch("/api/files/bulk", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ action, ids, folder_id: folderId })
        });
        if (!res.ok) {
            const t = await res.text().catch(() => "");
            throw new Error(t || "Bulk action failed");
        }
        const data = await res.json();
        showToast(`Bulk ${action}: ${data.done} done${data.failed ? `, ${data.failed} failed` : ""}`, data.failed ? "info" : "success");
        clearSelection();
        loadDriveContent();
    } catch (err) {
        showToast(err.message, "error");
    }
}

function bulkTrash() {
    if (selectedIds.size === 0) return;
    showConfirmDialog({
        title: "Trash Selected",
        message: `Move ${selectedIds.size} file(s) to Trash?`,
        confirmText: "Move to Trash",
        danger: true,
        onConfirm: () => bulkAction("trash")
    });
}

function bulkFavorite() {
    bulkAction("favorite");
}

async function copyFile(id) {
    try {
        const res = await apiFetch(`/api/files/${id}/copy`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({})
        });
        if (!res.ok) {
            const t = await res.text().catch(() => "");
            throw new Error(t || "Copy failed");
        }
        showToast("Copy created", "success");
        if (currentTab === "drive") loadDriveContent();
        else if (currentTab === "media") loadMediaLibrary();
    } catch (err) {
        showToast(err.message, "error");
    }
}

function bulkMovePrompt() {
    showPromptDialog({
        title: "Move Selected Files",
        label: "Target folder ID (empty = Drive root)",
        placeholder: "folder id or empty",
        confirmText: "Move",
        onConfirm: (val) => bulkAction("move", val || null)
    });
}

// ================= Drive filters =================
function driveFilterParams() {
    const mime = (document.getElementById("filter-mime") || {}).value || "";
    const size = (document.getElementById("filter-size") || {}).value || "";
    let minSize = 0, maxSize = 0;
    if (size) {
        const [a, b] = size.split("-").map(Number);
        minSize = a || 0; maxSize = b || 0;
    }
    return { mime, minSize, maxSize };
}

async function applyDriveFilters() {
    const { mime, minSize, maxSize } = driveFilterParams();
    const status = document.getElementById("filter-status");
    if (!mime && !minSize && !maxSize) {
        if (status) status.innerText = "";
        loadDriveContent();
        return;
    }
    try {
        const params = new URLSearchParams();
        if (currentFolderId) params.set("folder_id", currentFolderId);
        if (mime) params.set("mime", mime);
        if (minSize) params.set("min_size", minSize);
        if (maxSize) params.set("max_size", maxSize);
        const res = await apiFetch(`/api/files?${params.toString()}`);
        cachedFiles = await res.json() || [];
        cachedFolders = [];
        if (status) status.innerText = `${cachedFiles.length} match(es)`;
        renderBreadcrumbs();
        renderActiveView();
    } catch (err) {
        showToast("Filter failed: " + err.message, "error");
    }
}

function clearDriveFilters() {
    const m = document.getElementById("filter-mime");
    const s = document.getElementById("filter-size");
    if (m) m.value = "";
    if (s) s.value = "";
    const status = document.getElementById("filter-status");
    if (status) status.innerText = "";
    loadDriveContent();
}

// ================= Notifications + avatar =================
function toggleNotifDropdown(e) {
    if (e) e.stopPropagation();
    closeAvatarMenu();
    const dd = document.getElementById("notif-dropdown");
    if (!dd) return;
    const open = dd.style.display === "block";
    if (open) { dd.style.display = "none"; return; }
    dd.style.display = "block";
    loadNotificationsInto(dd);
}

function closeNotifDropdown() {
    const dd = document.getElementById("notif-dropdown");
    if (dd) dd.style.display = "none";
}

function toggleAvatarMenu(e) {
    if (e) e.stopPropagation();
    closeNotifDropdown();
    const m = document.getElementById("avatar-menu");
    if (!m) return;
    m.style.display = m.style.display === "block" ? "none" : "block";
}

function closeAvatarMenu() {
    const m = document.getElementById("avatar-menu");
    if (m) m.style.display = "none";
}

async function loadNotificationsInto(dd) {
    dd.innerHTML = `<p style="padding:12px;font-size:0.8rem;color:var(--text-muted);">Loading…</p>`;
    try {
        const res = await apiFetch("/api/notifications");
        const list = await res.json() || [];
        if (list.length > 0) {
            localStorage.setItem("tdocs_notif_seen", String(list[0].id));
        }
        updateNotifBadge(0);
        dd.innerHTML = list.length === 0
            ? `<p style="padding:12px;font-size:0.8rem;color:var(--text-muted);">No notifications yet.</p>`
            : `<div class="dropdown-title">Recent activity</div>` + list.map(n => `
                <div class="notif-item">
                    <span class="nkind">${escapeHtml(n.kind)}</span>
                    <div style="font-weight:600;">${escapeHtml(n.title)}</div>
                    ${n.detail ? `<div style="color:var(--text-secondary);word-break:break-word;">${escapeHtml(n.detail)}</div>` : ""}
                    <div class="ntime">${formatDateTime(n.time)}</div>
                </div>`).join("");
    } catch (err) {
        dd.innerHTML = `<p style="padding:12px;font-size:0.8rem;color:var(--danger);">Failed to load.</p>`;
    }
}

async function refreshNotifications() {
    try {
        const res = await apiFetch("/api/notifications");
        if (!res.ok) return;
        const list = await res.json() || [];
        if (list.length === 0) return;
        const seen = parseInt(localStorage.getItem("tdocs_notif_seen") || "0", 10);
        const unread = list.filter(n => n.id > seen).length;
        updateNotifBadge(unread);
        const dd = document.getElementById("notif-dropdown");
        if (dd && dd.style.display === "block") loadNotificationsInto(dd);
    } catch (e) { /* silent */ }
}

function updateNotifBadge(n) {
    const badge = document.getElementById("notif-badge");
    if (!badge) return;
    if (n > 0) {
        badge.style.display = "flex";
        badge.innerText = n > 9 ? "9+" : String(n);
    } else {
        badge.style.display = "none";
    }
}

// ================= Stream warmer =================
// Playback latency was dominated by the backend resolving a file's Telegram
// reference on every request. A single 1-byte range request makes the backend
// resolve once and cache it, so the playhead starts without that round-trip.
const StreamWarmer = {
    warmed: new Set(),
    inflight: new Set(),
    controller: null,

    async warm(id) {
        if (!id || this.warmed.has(id) || this.inflight.has(id)) return;
        this.inflight.add(id);
        try {
            const res = await fetch(`/api/files/${id}/stream`, {
                headers: { Range: "bytes=0-0" },
                cache: "force-cache",
            });
            // Headers only arrive after the backend resolved the reference.
            if (res.ok || res.status === 206) this.warmed.add(id);
            if (res.body && res.body.cancel) { try { await res.body.cancel(); } catch (e) { /* already closed */ } }
        } catch (e) {
            /* warming is best-effort and must never surface to the user */
        } finally {
            this.inflight.delete(id);
        }
    },

    // Warm a whole queue's first entries without blocking the caller.
    warmQueue(files, limit) {
        (files || []).slice(0, limit || 3).forEach(f => { this.warm(f.id); });
    },
};

function warmStream(id) { return StreamWarmer.warm(id); }

// ================= Media Engine (persistent audio + mini player) =================
const MediaEngine = {
    audio: null,
    queue: [],
    index: -1,
    shuffle: false,
    repeatMode: "off", // off | all | one
    current: null,
    lastPosSave: 0,

    init() {
        if (this.audio) return;
        this.audio = new Audio();
        this.audio.preload = "metadata";
        const savedVol = parseFloat(localStorage.getItem("tdocs_vol") || "1");
        this.audio.volume = isNaN(savedVol) ? 1 : Math.min(1, Math.max(0, savedVol));
        const savedSpeed = parseFloat(localStorage.getItem("tdocs_speed") || "1");
        if (!isNaN(savedSpeed)) this.audio.playbackRate = savedSpeed;
        const a = this.audio;
        a.addEventListener("timeupdate", () => this.onTime());
        a.addEventListener("loadedmetadata", () => this.onLoaded());
        a.addEventListener("ended", () => this.onEnded());
        a.addEventListener("play", () => this.paint());
        a.addEventListener("pause", () => this.paint());
        a.addEventListener("error", () => {
            const cur = this.current;
            showToast(`Cannot play ${cur ? cur.name : "audio"}`, "error");
            this.next(true);
        });
        const set = (id, html) => { const el = document.getElementById(id); if (el) el.innerHTML = html; };
        set("mp-shuffle", getIcon("shuffle", "icon-sm"));
        set("mp-repeat", getIcon("repeat", "icon-sm"));
        set("mp-play", getIcon("play", "icon-sm"));
        const vol = document.getElementById("mp-vol");
        if (vol) vol.value = Math.round(this.audio.volume * 100);
        const spd = document.getElementById("mp-speed");
        if (spd) spd.value = String(this.audio.playbackRate);
        // Address controls by id: positional queries silently retargeted when
        // the compact layout reordered buttons.
        set("mp-prev", getIcon("skip-back", "icon-sm"));
        set("mp-next", getIcon("skip-forward", "icon-sm"));
        set("mp-mute", getIcon("volume", "icon-sm"));
        set("mp-expand", getIcon("eye", "icon-sm"));
        set("mp-close", getIcon("x", "icon-sm"));
        set("mp-minimize", getIcon("chevron-down", "icon-sm"));
        set("mp-restore", getIcon("chevron-up", "icon-sm"));
        this.paintMute();
        this.bindShortcuts();
    },

    // Keyboard affordances for the compact pill: Esc minimizes, the info block
    // behaves like a button. Bound once.
    bindShortcuts() {
        if (this._bound) return;
        this._bound = true;
        const info = document.getElementById("mp-info");
        if (info) {
            info.addEventListener("keydown", (e) => {
                if (e.key === "Enter" || e.key === " ") { e.preventDefault(); this.restore(); }
            });
        }
        document.addEventListener("keydown", (e) => {
            const bar = document.getElementById("mini-player");
            if (!bar || bar.style.display === "none") return;
            if (e.key === "Escape" && !bar.classList.contains("mp-min")) {
                // Don't fight modals: only minimize when nothing is layered on top.
                const modalOpen = [...document.querySelectorAll(".modal-overlay")].some(m => getComputedStyle(m).display !== "none");
                if (!modalOpen) this.minimize();
            }
        });
    },

    streamUrl(id) { return `/api/files/${id}/stream`; },

    playQueue(files, idx, fallback) {
        this.init();
        let list = (files || []).filter(f => getFileCategory(f.mime_type, f.name).category === "audio");
        if (list.length === 0 && fallback) list = [fallback];
        if (list.length === 0) {
            showToast("No playable audio", "error");
            return;
        }
        this.queue = list;
        this.playAt(Math.max(0, idx || 0));
    },

    playAt(i) {
        if (i < 0 || i >= this.queue.length) return;
        this.index = i;
        const f = this.queue[i];
        this.current = { id: f.id, name: f.name, mime: f.mime_type, size: f.size, kind: "audio" };
        // The active track is fully buffered; queued neighbours are only
        // resolved, so playlists start instantly without pre-downloading them.
        this.audio.preload = "auto";
        this.audio.src = this.streamUrl(f.id);
        this.audio.play().catch(() => showToast("Playback blocked by browser", "error"));
        this.recordRecent(f);
        this.paint();
        this.prefetchNext();
        const bar = document.getElementById("mini-player");
        if (bar) {
            bar.style.display = "flex";
            this.applyLayout();
        }
    },

    // Resolve the upcoming track ahead of time so "next" starts immediately.
    prefetchNext() {
        if (this.repeatMode === "one") { StreamWarmer.warm(this.current && this.current.id); return; }
        const i = this.shuffle
            ? (this.queue.length > 1 ? (this.index + 1) % this.queue.length : this.index)
            : this.index + 1;
        const n = this.queue[i];
        if (n) StreamWarmer.warm(n.id);
    },

    toggle() {
        this.init();
        if (!this.current) return;
        if (this.audio.paused) this.audio.play().catch(() => {});
        else this.audio.pause();
    },

    next(manual) {
        if (this.queue.length === 0) return;
        if (!manual && this.repeatMode === "one") {
            this.audio.currentTime = 0;
            this.audio.play().catch(() => {});
            return;
        }
        let i = this.index + 1;
        if (this.shuffle && this.queue.length > 1) {
            do { i = Math.floor(Math.random() * this.queue.length); } while (i === this.index);
        } else if (i >= this.queue.length) {
            if (this.repeatMode === "all" || manual) i = 0;
            else return;
        }
        this.playAt(i);
    },

    prev() {
        if (this.queue.length === 0) return;
        if (this.audio.currentTime > 3) {
            this.audio.currentTime = 0;
            return;
        }
        this.playAt((this.index - 1 + this.queue.length) % this.queue.length);
    },

    onTime() {
        const a = this.audio, dur = a.duration || 0;
        const sb = document.getElementById("mp-seekbar");
        if (sb && dur > 0 && document.activeElement !== sb) sb.value = Math.round(a.currentTime / dur * 1000);
        const fill = document.getElementById("mp-progress-fill");
        if (fill) fill.style.width = dur > 0 ? (a.currentTime / dur * 100).toFixed(2) + "%" : "0%";
        const cur = document.getElementById("mp-cur");
        if (cur) cur.innerText = this.fmt(a.currentTime);
        const du = document.getElementById("mp-dur");
        if (du) du.innerText = dur > 0 ? this.fmt(dur) : "0:00";
        if (this.current && Date.now() - this.lastPosSave > 5000 && dur > 0) {
            this.lastPosSave = Date.now();
            savePos(this.current.id, a.currentTime);
        }
    },

    onLoaded() {
        const a = this.audio;
        const du = document.getElementById("mp-dur");
        if (du && a.duration) du.innerText = this.fmt(a.duration);
        if (this.current) {
            const pos = getPos(this.current.id);
            if (pos > 10 && a.duration && pos < a.duration * 0.95) a.currentTime = pos;
        }
        this.paint();
    },

    onEnded() {
        if (this.current) savePos(this.current.id, 0);
        this.next(false);
    },

    seekTo(pct) {
        const a = this.audio;
        if (a.duration) a.currentTime = (pct / 100) * a.duration;
    },

    setVolume(v) {
        this.audio.volume = Math.min(1, Math.max(0, v / 100));
        localStorage.setItem("tdocs_vol", String(this.audio.volume));
        this.paintMute();
    },

    toggleMute() {
        this.audio.muted = !this.audio.muted;
        this.paintMute();
    },

    paintMute() {
        const b = document.getElementById("mp-mute");
        if (b) {
            b.innerHTML = getIcon(this.audio && this.audio.muted ? "volume-x" : "volume", "icon-sm");
            b.classList.toggle("off", !!(this.audio && this.audio.muted));
        }
    },

    setSpeed(v) {
        this.audio.playbackRate = v;
        localStorage.setItem("tdocs_speed", String(v));
    },

    cycleRepeat() {
        this.repeatMode = this.repeatMode === "off" ? "all" : this.repeatMode === "all" ? "one" : "off";
        this.paint();
    },

    toggleShuffle() {
        this.shuffle = !this.shuffle;
        this.paint();
    },

    paint() {
        const a = this.audio;
        const playBtn = document.getElementById("mp-play");
        if (playBtn && a) playBtn.innerHTML = getIcon(a.paused ? "play" : "pause", "icon-sm");
        const bar = document.getElementById("mini-player");
        if (bar && a) bar.classList.toggle("playing", !a.paused);
        const sh = document.getElementById("mp-shuffle");
        if (sh) sh.classList.toggle("off", !this.shuffle);
        const rp = document.getElementById("mp-repeat");
        if (rp) {
            rp.innerHTML = getIcon(this.repeatMode === "one" ? "repeat-1" : "repeat", "icon-sm");
            rp.classList.toggle("off", this.repeatMode === "off");
        }
        const title = document.getElementById("mp-title");
        const sub = document.getElementById("mp-sub");
        const cover = document.getElementById("mp-cover");
        if (this.current) {
            if (title) title.innerText = this.current.name;
            if (sub) sub.innerText = `${formatSize(this.current.size || 0)} • ${this.current.mime || ""}`;
            if (cover) cover.innerHTML = getIcon(this.current.kind === "video" ? "film" : "music", "icon-sm");
        }
    },

    expand() {
        if (!this.current) return;
        if (this.current.kind === "video") {
            openPreview(this.current.id, this.current.name, this.current.mime, this.current.size);
        } else {
            openDetailsDrawer(this.current.id);
        }
    },

    // Compact pill vs full bar. Persisted so the choice survives navigation
    // and reloads; applied by playAt() whenever the bar is shown.
    applyLayout() {
        const bar = document.getElementById("mini-player");
        if (!bar) return;
        const min = localStorage.getItem("tdocs_mp_min") === "1";
        bar.classList.toggle("mp-min", min);
        const btn = document.getElementById("mp-minimize");
        if (btn) btn.title = min ? "Restore player" : "Minimize player";
        // Seed the compact progress line now: timeupdate only fires while the
        // audio actually advances, so a paused/restored track would otherwise
        // render an empty bar.
        const a = this.audio;
        const fill = document.getElementById("mp-progress-fill");
        if (fill && a) {
            const dur = a.duration || 0;
            fill.style.width = dur > 0 ? (a.currentTime / dur * 100).toFixed(2) + "%" : "0%";
        }
    },

    minimize() {
        localStorage.setItem("tdocs_mp_min", "1");
        this.applyLayout();
    },

    restore() {
        localStorage.setItem("tdocs_mp_min", "0");
        this.applyLayout();
    },

    close() {
        if (this.audio) this.audio.pause();
        if (this.current) savePos(this.current.id, this.audio ? this.audio.currentTime : 0);
        const bar = document.getElementById("mini-player");
        if (bar) bar.style.display = "none";
    },

    fmt(s) {
        if (!isFinite(s) || s < 0) s = 0;
        s = Math.floor(s);
        const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60;
        const mm = h > 0 ? String(m).padStart(2, "0") : String(m);
        return (h > 0 ? h + ":" : "") + mm + ":" + String(sec).padStart(2, "0");
    },

    recordRecent(f) {
        try {
            const key = "tdocs_recent_play";
            let list = JSON.parse(localStorage.getItem(key) || "[]");
            list = list.filter(x => x.id !== f.id);
            list.unshift({ id: f.id, name: f.name, mime_type: f.mime_type, size: f.size, at: Date.now() });
            localStorage.setItem(key, JSON.stringify(list.slice(0, 25)));
        } catch (e) { /* ignore */ }
    }
};

function getRecentPlayed() {
    try {
        return JSON.parse(localStorage.getItem("tdocs_recent_play") || "[]");
    } catch (e) {
        return [];
    }
}

function loadPositions() {
    try {
        return JSON.parse(localStorage.getItem("tdocs_pos") || "{}");
    } catch (e) {
        return {};
    }
}

function getPos(id) {
    const all = loadPositions();
    return all[id] || 0;
}

function savePos(id, sec) {
    try {
        const all = loadPositions();
        if (!sec || sec < 5) delete all[id];
        else all[id] = Math.floor(sec);
        const keys = Object.keys(all);
        if (keys.length > 100) delete all[keys[0]];
        localStorage.setItem("tdocs_pos", JSON.stringify(all));
    } catch (e) { /* ignore */ }
}

// ================= Video helpers =================
function pvVideo() {
    return document.getElementById("pv-video");
}

function wireVideoElement(id) {
    const v = pvVideo();
    if (!v) return;
    MediaEngine.current = Object.assign({}, previewState, { kind: "video" });
    MediaEngine.recordRecent(previewState);
    MediaEngine.paint();
    v.addEventListener("loadedmetadata", () => {
        const pos = getPos(id);
        if (pos > 10 && v.duration && pos < v.duration * 0.95) v.currentTime = pos;
        attachSubs(v, previewState.name);
    });
    let lastSave = 0;
    v.addEventListener("timeupdate", () => {
        if (Date.now() - lastSave > 5000) {
            lastSave = Date.now();
            savePos(id, v.currentTime);
        }
    });
    v.addEventListener("pause", () => savePos(id, v.currentTime));
    v.addEventListener("ended", () => {
        savePos(id, 0);
        stepVideo(1);
    });
    // Once PiP closes, drop the parked element so it cannot linger.
    v.addEventListener("leavepictureinpicture", () => {
        const host = document.getElementById("pip-keepalive");
        if (host && v.parentElement === host) host.removeChild(v);
    });
}

// Keyboard control for the media players. Ignored while typing so search and
// form fields keep working.
function mediaKeyTarget(e) {
    const t = e.target;
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable)) return false;
    return true;
}

document.addEventListener("keydown", function (e) {
    if (!mediaKeyTarget(e) || e.ctrlKey || e.metaKey || e.altKey) return;

    const v = document.getElementById("pv-video");
    const previewOpen = v && document.getElementById("preview-modal") && document.getElementById("preview-modal").style.display !== "none";

    if (previewOpen && v) {
        switch (e.key) {
            case " ": case "k":
                e.preventDefault();
                if (v.paused) v.play().catch(() => {}); else v.pause();
                return;
            case "ArrowRight": e.preventDefault(); v.currentTime = Math.min(v.duration || Infinity, v.currentTime + 5); return;
            case "ArrowLeft": e.preventDefault(); v.currentTime = Math.max(0, v.currentTime - 5); return;
            case "l": e.preventDefault(); v.currentTime = Math.min(v.duration || Infinity, v.currentTime + 10); return;
            case "j": e.preventDefault(); v.currentTime = Math.max(0, v.currentTime - 10); return;
            case "ArrowUp": e.preventDefault(); v.volume = Math.min(1, v.volume + 0.05); v.muted = false; return;
            case "ArrowDown": e.preventDefault(); v.volume = Math.max(0, v.volume - 0.05); return;
            case "m": e.preventDefault(); v.muted = !v.muted; return;
            case "f": e.preventDefault(); videoFullscreen(); return;
            case "p": e.preventDefault(); videoPip(); return;
            case "n": e.preventDefault(); stepVideo(1); return;
            case "b": e.preventDefault(); stepVideo(-1); return;
            case "Escape": closePreview(); return;
            default: return;
        }
    }

    // Mini player shortcuts while it is visible.
    const bar = document.getElementById("mini-player");
    if (!bar || bar.style.display === "none" || !MediaEngine.current) return;
    if (e.key === " " || e.key === "k") {
        e.preventDefault();
        MediaEngine.toggle();
    } else if (e.key === "ArrowRight") {
        e.preventDefault();
        MediaEngine.seekTo(Math.min(100, (MediaEngine.audio.currentTime + 5) / (MediaEngine.audio.duration || 1) * 100));
    } else if (e.key === "ArrowLeft") {
        e.preventDefault();
        MediaEngine.seekTo(Math.max(0, (MediaEngine.audio.currentTime - 5) / (MediaEngine.audio.duration || 1) * 100));
    } else if (e.key === "n") {
        e.preventDefault(); MediaEngine.next(true);
    }
}, false);

function stepVideo(d) {
    if (videoQueue.length < 2 || !previewState) return;
    const i = videoQueue.findIndex(f => f.id === previewState.id);
    const n = videoQueue[(i + d + videoQueue.length) % videoQueue.length];
    openPreview(n.id, n.name, n.mime_type, n.size);
}

function setVideoSpeed(v) {
    const el = pvVideo();
    if (el) el.playbackRate = v;
}

function videoPip() {
    const v = pvVideo();
    if (!v) return;
    if (document.pictureInPictureElement) {
        document.exitPictureInPicture().catch(() => {});
    } else if (document.pictureInPictureEnabled) {
        v.requestPictureInPicture().catch(() => showToast("Picture-in-picture unavailable", "error"));
    } else {
        showToast("Picture-in-picture not supported here", "error");
    }
}

function videoFullscreen() {
    const wrap = document.querySelector("#preview-content .video-wrap") || pvVideo();
    if (!wrap) return;
    if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
    else if (wrap.requestFullscreen) wrap.requestFullscreen().catch(() => showToast("Fullscreen unavailable", "error"));
}

async function attachSubs(video, fileName) {
    const badge = document.getElementById("pv-subs");
    try {
        const base = fileName.replace(/\.[^.]+$/, "").toLowerCase();
        const cand = [...cachedFiles].find(f => {
            const n = (f.name || "").toLowerCase();
            return n.replace(/\.[^.]+$/, "") === base && /\.(vtt|srt)$/i.test(n);
        });
        if (!cand) {
            if (badge) badge.innerText = "";
            return;
        }
        const res = await apiFetch(`/api/files/${cand.id}/stream`);
        if (!res.ok) throw new Error("sub fetch failed");
        let txt = await res.text();
        if (/\.srt$/i.test(cand.name)) {
            txt = "WEBVTT\n\n" + txt.replace(/\r/g, "").replace(/(\d{2}:\d{2}:\d{2}),(\d{3})/g, "$1.$2");
        }
        const url = URL.createObjectURL(new Blob([txt], { type: "text/vtt" }));
        const track = document.createElement("track");
        track.kind = "subtitles";
        track.label = cand.name;
        track.srclang = "en";
        track.src = url;
        track.default = true;
        video.appendChild(track);
        if (badge) badge.innerText = "CC: " + cand.name;
    } catch (e) {
        if (badge) badge.innerText = "";
    }
}

// ================= Image gallery =================
function galleryZoom() {
    const img = document.getElementById("pv-img");
    if (!img || !previewState) return;
    previewState.zoom = previewState.zoom >= 3 ? 1 : previewState.zoom + 1;
    img.style.transform = `scale(${previewState.zoom})`;
    img.style.cursor = previewState.zoom > 1 ? "zoom-out" : "zoom-in";
}

function galleryStep(d) {
    if (galleryList.length < 2 || !previewState) return;
    const i = galleryList.findIndex(g => g.id === previewState.id);
    const n = galleryList[(i + d + galleryList.length) % galleryList.length];
    openPreview(n.id, n.name, n.mime_type, n.size);
}

function galleryFullscreen() {
    const wrap = document.getElementById("gallery-wrap") || document.getElementById("preview-content");
    if (!wrap) return;
    if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
    else if (wrap.requestFullscreen) wrap.requestFullscreen().catch(() => showToast("Fullscreen unavailable", "error"));
}

// ================= Media library =================
let mediaKind = "";
let mediaSearch = "";
let mediaFavOnly = false;
let mediaFiles = [];
let mediaDebounce = null;

function setMediaKind(k) {
    mediaKind = k;
    document.querySelectorAll("#media-kind-switch .view-btn").forEach(b => {
        b.classList.toggle("active", (b.dataset.kind || "") === k);
    });
    loadMediaLibrary();
}

function mediaSearchChanged(v) {
    clearTimeout(mediaDebounce);
    mediaDebounce = setTimeout(() => {
        mediaSearch = (v || "").trim();
        loadMediaLibrary();
    }, 300);
}

async function loadMediaLibrary() {
    const grid = document.getElementById("media-grid");
    const empty = document.getElementById("empty-media-state");
    if (grid) skeletonCards(grid, 6);
    try {
        const params = new URLSearchParams();
        if (mediaSearch) params.set("search", mediaSearch);
        if (mediaKind) params.set("mime", mediaKind);
        const res = await apiFetch(`/api/files?${params.toString()}`);
        let files = await res.json() || [];
        files = files.filter(f => ["audio", "video", "image"].includes(getFileCategory(f.mime_type, f.name).category));
        if (mediaFavOnly) files = files.filter(f => favoriteIds.has(f.id));
        mediaFiles = files;

        const recentSec = document.getElementById("media-recent-section");
        const recentList = document.getElementById("media-recent-list");
        const recent = getRecentPlayed().filter(r => !mediaKind || (r.mime_type || "").startsWith(mediaKind));
        if (recentSec && recentList) {
            if (recent.length === 0) {
                recentSec.style.display = "none";
            } else {
                recentSec.style.display = "block";
                recentList.innerHTML = recent.slice(0, 5).map(r => `
                    <div class="recent-row" onclick="playMediaItem('${r.id}')">
                        <span class="type-tile">${getIcon(getFileCategory(r.mime_type, r.name).icon, "icon-sm")}</span>
                        <span class="rname">${escapeHtml(r.name)}</span>
                        <span class="rmeta">${formatSize(r.size || 0)}</span>
                    </div>`).join("");
            }
        }

        if (!grid) return;
        if (files.length === 0) {
            grid.innerHTML = "";
            if (empty) empty.style.display = "flex";
            return;
        }
        if (empty) empty.style.display = "none";
        grid.innerHTML = files.map(f => fileCardHTML(f, { selectable: false })).join("");
    } catch (err) {
        showToast("Media library failed: " + err.message, "error");
    }
}

function playMediaItem(id) {
    const pool = mediaFiles.length ? mediaFiles : cachedFiles;
    const f = pool.find(x => x.id === id);
    if (!f) return;
    const cat = getFileCategory(f.mime_type, f.name).category;
    if (cat === "audio") {
        const audios = pool.filter(x => getFileCategory(x.mime_type, x.name).category === "audio");
        MediaEngine.playQueue(audios, audios.findIndex(x => x.id === id), f);
    } else if (cat === "video") {
        videoQueue = pool.filter(x => getFileCategory(x.mime_type, x.name).category === "video");
        openPreview(f.id, f.name, f.mime_type, f.size);
    } else if (cat === "image") {
        galleryList = pool.filter(x => getFileCategory(x.mime_type, x.name).category === "image");
        openPreview(f.id, f.name, f.mime_type, f.size);
    } else {
        openPreview(f.id, f.name, f.mime_type, f.size);
    }
}

function mediaPlayAll() {
    const audios = mediaFiles.filter(f => getFileCategory(f.mime_type, f.name).category === "audio");
    if (audios.length === 0) {
        showToast("No audio tracks in this view", "info");
        return;
    }
    MediaEngine.playQueue(audios, 0);
}

// ================= Text / Markdown / JSON =================
function renderTextPreview(name, text) {
    if (/\.json$/i.test(name)) {
        try {
            return `<pre class="code-preview">${escapeHtml(JSON.stringify(JSON.parse(text), null, 2))}</pre>`;
        } catch (e) { /* fall through to plain */ }
    }
    if (/\.(md|markdown)$/i.test(name)) {
        return `<div class="code-preview" style="white-space:normal;line-height:1.6;">${renderMarkdown(text)}</div>`;
    }
    return `<pre class="code-preview">${escapeHtml(text)}</pre>`;
}

function renderMarkdown(src) {
    const blocks = [];
    let h = escapeHtml(src);
    h = h.replace(/```(\w*)\n([\s\S]*?)```/g, (m, lang, code) => {
        blocks.push(`<pre style="background:#0b1526;color:#dbe7f5;border-radius:10px;padding:12px;overflow:auto;font-family:var(--font-mono);font-size:0.78rem;white-space:pre-wrap;">${code}</pre>`);
        return `\u0000${blocks.length - 1}\u0000`;
    });
    h = h.replace(/^#### (.*)$/gm, "<h4>$1</h4>").replace(/^### (.*)$/gm, "<h3>$1</h3>")
        .replace(/^## (.*)$/gm, "<h2>$1</h2>").replace(/^# (.*)$/gm, "<h1>$1</h1>");
    h = h.replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>").replace(/(^|\W)\*(.+?)\*/g, "$1<em>$2</em>");
    h = h.replace(/`([^`\n]+)`/g, "<code>$1</code>");
    h = h.replace(/\[([^\]]+)\]\((https?:[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>');
    h = h.split(/\n{2,}/).map(p => {
        if (/^\u0000\d+\u0000$/.test(p.trim())) return p;
        if (/^<(h\d|pre|ul)/.test(p.trim())) return p;
        const items = p.split("\n").filter(l => /^\s*[-*] /.test(l));
        if (items.length > 0) {
            return "<ul>" + items.map(l => `<li>${l.replace(/^\s*[-*] /, "")}</li>`).join("") + "</ul>";
        }
        return "<p>" + p.replace(/\n/g, "<br>") + "</p>";
    }).join("\n");
    h = h.replace(/\x00(\d+) /g, (m, i) => blocks[parseInt(i, 10)] || "");
    return h;
}

// ================= ZIP inspector =================
async function fetchRange(id, start, endExclusive) {
    const res = await apiFetch(`/api/files/${id}/stream`, {
        headers: { Range: `bytes=${start}-${endExclusive - 1}` }
    });
    if (!res.ok && res.status !== 206 && res.status !== 200) return null;
    return await res.arrayBuffer();
}

const ZU16 = (v, o) => v.getUint16(o, true);
const ZU32 = (v, o) => v.getUint32(o, true);

async function inspectZip(id, name, size) {
    const container = document.getElementById("preview-content");
    if (!container) return;
    try {
        const tailN = Math.min(size, 131072);
        const tail = await fetchRange(id, size - tailN, size);
        if (!tail) throw new Error("Cannot read archive tail");
        const bytes = new Uint8Array(tail);
        const view = new DataView(tail);
        const scanFrom = Math.max(0, bytes.length - 65558);
        let eocd = -1;
        for (let i = bytes.length - 22; i >= scanFrom; i--) {
            if (view.getUint32(i, true) === 0x06054b50) { eocd = i; break; }
        }
        if (eocd < 0) throw new Error("End-of-central-directory not found (comment too large?)");
        const totalEntries = ZU16(view, eocd + 10);
        const cdSize = ZU32(view, eocd + 12);
        const cdOffset = ZU32(view, eocd + 16);
        if (totalEntries === 0) throw new Error("Archive is empty");

        let cdBuf = tail, cdBase = size - tailN;
        const wantCD = Math.min(cdSize, 5 * 1024 * 1024);
        if (cdOffset < cdBase || cdOffset + wantCD > size) {
            cdBuf = await fetchRange(id, cdOffset, cdOffset + wantCD);
            if (!cdBuf) throw new Error("Cannot read central directory");
            cdBase = cdOffset;
        }
        const cd = new DataView(cdBuf);
        const entries = [];
        let off = cdOffset - cdBase;
        const maxEntries = Math.min(totalEntries, 2000);
        const dec = new TextDecoder();
        for (let n = 0; n < maxEntries; n++) {
            if (off + 46 > cd.byteLength || cd.getUint32(off, true) !== 0x02014b50) break;
            const flags = ZU16(cd, off + 8);
            const method = ZU16(cd, off + 10);
            const csize = ZU32(cd, off + 20);
            const usize = ZU32(cd, off + 24);
            const nameLen = ZU16(cd, off + 28);
            const extraLen = ZU16(cd, off + 30);
            const commentLen = ZU16(cd, off + 32);
            const localOff = ZU32(cd, off + 42);
            if (off + 46 + nameLen > cd.byteLength) break;
            const ename = dec.decode(new Uint8Array(cdBuf, off + 46, nameLen));
            entries.push({
                name: ename, dir: ename.endsWith("/"), method,
                encrypted: (flags & 0x1) !== 0, unknownSizes: (flags & 0x8) !== 0,
                csize, usize, localOff
            });
            off += 46 + nameLen + extraLen + commentLen;
        }

        const methodLabel = m => m === 0 ? "Stored" : m === 8 ? "Deflated" : "Method " + m;
        container.innerHTML = `
            <div style="padding: 6px 2px 12px; font-size: 0.85rem; color: var(--text-secondary);">
                ${entries.length} entr${entries.length === 1 ? "y" : "ies"}${totalEntries > entries.length ? ` (showing first ${entries.length})` : ""} • ${escapeHtml(name)}
            </div>
            <div style="max-height: 46vh; overflow-y: auto; display: flex; flex-direction: column; gap: 2px;">
                ${entries.map((e, i) => `
                    <div class="zip-row" ${e.dir ? "" : `onclick="previewZipEntry('${id}', ${i})" style="cursor:pointer;"`} title="${e.dir ? "Folder" : "Click to preview"}">
                        <span style="color: var(--primary);">${getIcon(e.dir ? "folder" : getFileCategory("", e.name).icon, "icon-sm")}</span>
                        <span style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="${escapeHtml(e.name)}">${escapeHtml(e.name)}</span>
                        <span style="color:var(--text-muted);font-size:0.72rem;white-space:nowrap;">${e.dir ? "—" : methodLabel(e.method)}</span>
                        <span style="color:var(--text-muted);font-size:0.72rem;white-space:nowrap;">${e.dir || e.unknownSizes ? "" : formatSize(e.usize)}</span>
                    </div>`).join("")}
            </div>
            <div id="zip-entry-preview" style="margin-top: 10px;"></div>
        `;
        container._zipEntries = entries;
        container._zipSize = size;
        container._zipId = id;
    } catch (e) {
        container.innerHTML = `<p style="color:var(--danger);padding:16px;">Archive index failed: ${escapeHtml(e.message)}</p>`;
    }
}

async function previewZipEntry(id, idx) {
    const container = document.getElementById("preview-content");
    const box = document.getElementById("zip-entry-preview");
    if (!container || !box) return;
    const e = (container._zipEntries || [])[idx];
    if (!e || e.dir) return;
    if (e.encrypted) {
        box.innerHTML = `<p style="color:var(--warning);font-size:0.85rem;">Encrypted entry — preview unavailable.</p>`;
        return;
    }
    if (e.unknownSizes) {
        box.innerHTML = `<p style="color:var(--warning);font-size:0.85rem;">Entry uses a data descriptor — sizes unknown, preview unavailable.</p>`;
        return;
    }
    if (e.usize > 8 * 1024 * 1024) {
        box.innerHTML = `<p style="color:var(--warning);font-size:0.85rem;">Entry is larger than 8 MB — download the archive instead.</p>`;
        return;
    }
    if (e.method !== 0 && e.method !== 8) {
        box.innerHTML = `<p style="color:var(--warning);font-size:0.85rem;">Compression method not supported in preview.</p>`;
        return;
    }
    box.innerHTML = `<p style="color:var(--text-muted);font-size:0.85rem;">Extracting ${escapeHtml(e.name)}…</p>`;
    try {
        const lh = await fetchRange(id, e.localOff, e.localOff + 30);
        if (!lh) throw new Error("read failed");
        const lv = new DataView(lh);
        if (lv.getUint32(0, true) !== 0x04034b50) throw new Error("bad local header");
        const nl = ZU16(lv, 26), el = ZU16(lv, 28);
        const dataStart = e.localOff + 30 + nl + el;
        const comp = await fetchRange(id, dataStart, dataStart + e.csize);
        if (!comp) throw new Error("read failed");
        let bytes = new Uint8Array(comp);
        if (e.method === 8) {
            if (typeof DecompressionStream === "undefined") throw new Error("browser cannot inflate");
            const ds = new DecompressionStream("deflate-raw");
            const stream = new Blob([comp]).stream().pipeThrough(ds);
            bytes = new Uint8Array(await new Response(stream).arrayBuffer());
        }
        const lower = e.name.toLowerCase();
        if (/\.(png|jpe?g|gif|webp|svg|bmp)$/.test(lower)) {
            const url = URL.createObjectURL(new Blob([bytes]));
            box.innerHTML = `<img src="${url}" style="max-width:100%;max-height:40vh;object-fit:contain;border-radius:10px;" alt="${escapeHtml(e.name)}">`;
        } else if (/\.(txt|md|json|csv|log|xml|yml|yaml|ini|sh|py|js|ts|go|rs|java|c|h|cpp|css|html)$/.test(lower)) {
            const txt = new TextDecoder().decode(bytes.slice(0, 50000));
            box.innerHTML = renderTextPreview(e.name, txt);
        } else {
            const url = URL.createObjectURL(new Blob([bytes]));
            box.innerHTML = `<a class="btn btn-primary" href="${url}" download="${escapeHtml(e.name.split("/").pop())}">${getIcon("download", "icon-sm")} Download entry (${formatSize(bytes.length)})</a>`;
        }
    } catch (err) {
        box.innerHTML = `<p style="color:var(--danger);font-size:0.85rem;">Extract failed: ${escapeHtml(err.message)}</p>`;
    }
}

// ================= Skeletons / shortcuts / indicators =================
function skeletonCards(el, n = 6) {
    if (!el) return;
    el.innerHTML = Array.from({ length: n }, () => '<div class="skel skel-card"></div>').join("");
}

function skeletonRows(tbody, n = 5) {
    if (!tbody) return;
    tbody.innerHTML = Array.from({ length: n }, () => '<tr><td colspan="6"><div class="skel skel-row"></div></td></tr>').join("");
}

function setupShortcuts() {
    document.addEventListener("keydown", e => {
        const t = e.target;
        const typing = t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable);
        if (e.key === "Escape") {
            closePreview();
            closeDetailsDrawer();
            hideContextMenu();
            closeNotifDropdown();
            closeAvatarMenu();
            closeHelpModal();
            closeShareModal();
            closeQrModal();
            return;
        }
        if (typing || e.ctrlKey || e.metaKey || e.altKey) return;
        if (e.key === "/") {
            e.preventDefault();
            if (currentTab !== "drive") switchTab("drive");
            const box = document.getElementById("search-box");
            if (box) box.focus();
        } else if (e.key === "u") {
            openFilePicker();
        }
    });
}

function updateSortIndicators() {
    document.querySelectorAll("th.sortable").forEach(th => {
        const ind = th.querySelector(".sort-ind");
        if (!ind) return;
        ind.innerText = th.dataset.field === currentSort.field
            ? (currentSort.order === "asc" ? "▲" : "▼")
            : "";
    });
}

// ================= Button loading helper =================
async function withBtnLoading(btn, fn) {
    if (!btn) return fn();
    const old = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = "Working…";
    try {
        return await fn();
    } finally {
        btn.disabled = false;
        btn.innerHTML = old;
    }
}

// ================= Telegram Wizard (browser) =================
const TgWizard = {
    id: null,
    pollHandle: null,

    async open() {
        const modal = document.getElementById("tg-wizard-modal");
        if (modal) modal.style.display = "flex";
        this._setPane("phone");
        document.getElementById("tg-wiz-action-text").textContent = "Send Code";
        const errBox = document.getElementById("tg-wiz-error");
        if (errBox) errBox.style.display = "none";
        this._setStep("phone");
        // Preflight: starting the wizard requires a logged-in admin AND a
        // server-side Telegram backend (API_ID/HASH loaded at startup).
        // Fail fast with an actionable message instead of a generic
        // "Failed to start wizard" after the phone number is entered.
        try {
            const res = await apiFetch("/api/telegram/wizard/needed");
            const data = await res.json().catch(() => ({}));
            if (res.status === 403) {
                this._showError("Please log in as administrator first, then open the Telegram setup again.");
                return;
            }
            if (res.ok && data.configured === false) {
                this._showError("Server is missing Telegram API credentials. On the server run `tdocs setup` (API_ID + API_HASH from my.telegram.org), restart tdocs, then try again.");
                return;
            }
            if (res.ok && data.configured === true && data.client_available === false) {
                this._showError("API credentials are saved but not loaded. Restart the server (`systemctl restart tdocs`), then try again.");
                return;
            }
        } catch (_) {
            // Non-fatal: the start call below surfaces the real error.
        }
        // Permit the user to immediately scroll/click the phone field.
        setTimeout(() => {
            const el = document.getElementById("tg-wiz-phone");
            if (el) el.focus();
        }, 50);
    },

    close() {
        const modal = document.getElementById("tg-wizard-modal");
        if (modal) modal.style.display = "none";
        this._stopPolling();
        if (this.id) {
            apiFetch("/api/telegram/wizard/discard", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ id: this.id })
            }).catch(() => {});
            this.id = null;
        }
    },

    _setPane(name) {
        document.querySelectorAll('.wizard-pane').forEach(p => {
            p.style.display = (p.dataset.pane === name) ? "" : "none";
        });
        const titles = {
            phone:   "Send Code",
            code:    "Verify",
            password:"Continue",
            done:    "Done"
        };
        const el = document.getElementById("tg-wiz-action-text");
        if (el && titles[name]) el.textContent = titles[name];
    },

    _setStep(name) {
        const stepMap = { phone: 0, code: 1, password: 2 };
        const cur = stepMap[name] ?? 0;
        document.querySelectorAll('.wizard-step').forEach((node, idx) => {
            node.classList.remove("active", "done");
            if (idx < cur) node.classList.add("done");
            else if (idx === cur) node.classList.add("active");
        });
        document.querySelectorAll('.wizard-step-line').forEach((line, idx) => {
            line.classList.toggle("done", idx < cur);
        });
    },

    _showError(msg) {
        const errBox = document.getElementById("tg-wiz-error");
        if (!errBox) return;
        if (!msg) {
            errBox.style.display = "none";
            errBox.textContent = "";
            return;
        }
        errBox.style.display = "block";
        errBox.textContent = msg;
    },

    async _startPoll() {
        if (!this.id) return;
        this._stopPolling();
        this.pollHandle = setInterval(async () => {
            try {
                const res = await apiFetch(`/api/telegram/wizard/status?id=${encodeURIComponent(this.id)}`);
                if (!res.ok) return;
                const data = await res.json();
                if (data.success) {
                    this._finish(data.error || "");
                } else if (data.phase === "waiting_code") {
                    this._setStep("code");
                    this._setPane("code");
                    const phone = data.phone || "";
                    document.getElementById("tg-wiz-action-text").textContent = "Verify Code";
                    setTimeout(() => document.getElementById("tg-wiz-code")?.focus(), 50);
                } else if (data.phase === "waiting_password") {
                    this._setStep("password");
                    this._setPane("password");
                    document.getElementById("tg-wiz-action-text").textContent = "Unlock";
                    setTimeout(() => document.getElementById("tg-wiz-password")?.focus(), 50);
                } else if (data.phase === "error") {
                    this._stopPolling();
                    this._showError(data.error || "Wizard failed");
                    // The failed wizard is terminal on the server, so a retry
                    // must start a fresh one from the phone step.
                    this.id = null;
                    this._setStep("phone");
                    this._setPane("phone");
                    document.getElementById("tg-wiz-action-text").textContent = "Try Again";
                }
            } catch (e) { /* keep polling */ }
        }, 1200);
    },

    _stopPolling() {
        if (this.pollHandle) {
            clearInterval(this.pollHandle);
            this.pollHandle = null;
        }
    },

    _finish(errMsg) {
        this._stopPolling();
        document.querySelectorAll('.wizard-step').forEach(n => { n.classList.remove("active"); n.classList.add("done"); });
        document.querySelectorAll('.wizard-step-line').forEach(n => n.classList.add("done"));
        this._setPane("done");
        if (errMsg) {
            // Auth succeeded but the Storage Channel could not be verified.
            // Stay honest: Telegram is NOT connected — offer retry/sync.
            const icon = document.getElementById("tg-wiz-done-icon");
            const title = document.getElementById("tg-wiz-done-title");
            if (icon) icon.innerHTML = getIcon("alert-circle", "icon-xl").replace('class="icon icon-xl"', 'class="icon icon-xl" style="color: var(--warning);"');
            if (title) title.textContent = "Signed in — storage not ready";
            document.getElementById("tg-wiz-done-msg").textContent =
                "Signed in, but storage is not ready: " + errMsg + " Fix this in Settings → Telegram Storage, then Sync.";
            document.getElementById("tg-wiz-action-text").textContent = "Review Storage";
            showToast("Signed in, but Storage Channel is not ready", "error");
        } else {
            const icon = document.getElementById("tg-wiz-done-icon");
            const title = document.getElementById("tg-wiz-done-title");
            if (icon) icon.innerHTML = getIcon("check", "icon-xl").replace('class="icon icon-xl"', 'class="icon icon-xl" style="color: var(--success);"');
            if (title) title.textContent = "Telegram paired!";
            document.getElementById("tg-wiz-done-msg").textContent =
                "Authentication succeeded — Storage Channel bound automatically.";
            document.getElementById("tg-wiz-action-text").textContent = "All Set";
            showToast("Telegram paired successfully", "success");
        }
        // Refresh status / banner / folders
        refreshStatus();
        checkTelegramSetup();
    },

    async _submit(payload) {
        const res = await apiFetch("/api/telegram/wizard/submit", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ id: this.id, ...payload })
        });
        return res.json().catch(() => ({ ok: false, error: "bad response" }));
    },

    async action() {
        // Detect current pane by visibility, fall back to step indicator.
        const panes = Array.from(document.querySelectorAll('.wizard-pane'));
        const visible = panes.find(p => p.style.display !== "none") || panes.find(p => p.dataset.pane === "phone");
        const pane = visible?.dataset?.pane;
        if (!pane) return;
        this._showError("");

        if (pane === "phone") {
            const phone = document.getElementById("tg-wiz-phone").value.trim();
            if (!phone) { this._showError("Enter your Telegram phone number."); return; }
            const btn = document.getElementById("tg-wiz-action");
            if (btn) btn.disabled = true;
            document.getElementById("tg-wiz-action-text").textContent = "Sending…";
            try {
                const res = await apiFetch("/api/telegram/wizard/start", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ phone })
                });
                const raw = await res.text().catch(() => "");
                let data = {};
                try {
                    data = raw ? JSON.parse(raw) : {};
                } catch (_) {
                    data = raw ? { error: raw } : {};
                }
                if (!res.ok || !data.id) {
                    this._showError(data.hint || data.error || data.message || ("Server returned HTTP " + res.status));
                    document.getElementById("tg-wiz-action-text").textContent = "Send Code";
                    if (btn) btn.disabled = false;
                    return;
                }
                this.id = data.id;
                // Do not assume the code pane: the backend may report the
                // wizard already finished (session still valid) or failed
                // before sending anything. Trust the returned phase.
                const phase = data.phase || data.status || "sending_code";
                if (btn) btn.disabled = false;
                if (data.success || phase === "done") {
                    this._finish(data.error || "");
                    return;
                }
                if (phase === "error") {
                    this._showError(data.error || "Wizard failed to start");
                    document.getElementById("tg-wiz-action-text").textContent = "Send Code";
                    return;
                }
                this._setStep("code");
                this._setPane("code");
                document.getElementById("tg-wiz-action-text").textContent = "Verify Code";
                this._startPoll();
            } catch (e) {
                this._showError(e.message || "Network error");
                document.getElementById("tg-wiz-action-text").textContent = "Send Code";
                if (btn) btn.disabled = false;
            }
        } else if (pane === "code") {
            const code = document.getElementById("tg-wiz-code").value.trim();
            if (!code) { this._showError("Enter the verification code from Telegram."); return; }
            const data = await this._submit({ code });
            if (!data.ok) { this._showError(data.error || "Could not submit code"); return; }
            document.getElementById("tg-wiz-code").value = "";
            // Stay on this pane: only the backend knows whether 2FA is needed.
            // Polling advances to "password" when Telegram asks for it.
            this._startPoll();
        } else if (pane === "password") {
            const password = document.getElementById("tg-wiz-password").value;
            if (!password) { this._showError("Enter your 2FA cloud password."); return; }
            const data = await this._submit({ password });
            document.getElementById("tg-wiz-password").value = "";
            if (!data.ok) { this._showError(data.error || "Could not submit password"); return; }
            this._setStep("done");
            this._finish("");
        } else if (pane === "done") {
            this.close();
        }
    }
};

function openTelegramWizard() { TgWizard.open(); }
function closeTelegramWizard() { TgWizard.close(); }
function wizardAction() { TgWizard.action(); }

async function checkTelegramSetup() {
    await refreshTelegramState(false);
}

// ================= Sidebar collapse =================
const SIDEBAR_KEY = "tdocs_sidebar_collapsed";

function collapseSidebar() {
    const shell = document.querySelector(".layout-shell");
    if (!shell) return;
    shell.classList.add("sidebar-collapsed");
    localStorage.setItem(SIDEBAR_KEY, "1");
}

function expandSidebar() {
    const shell = document.querySelector(".layout-shell");
    if (!shell) return;
    shell.classList.remove("sidebar-collapsed");
    localStorage.setItem(SIDEBAR_KEY, "0");
}

function isSidebarCollapsed() {
    return localStorage.getItem(SIDEBAR_KEY) === "1";
}

function applySidebarCollapseState() {
    const shell = document.querySelector(".layout-shell");
    if (!shell) return;
    if (isSidebarCollapsed()) shell.classList.add("sidebar-collapsed");
    else shell.classList.remove("sidebar-collapsed");
}

function initSidebarCollapse() {
    // Inject the reopen button (sits at left margin when sidebar is hidden).
    if (!document.getElementById("sidebar-reopen-btn")) {
        const btn = document.createElement("button");
        btn.id = "sidebar-reopen-btn";
        btn.className = "sidebar-reopen-btn";
        btn.title = "Open sidebar";
        btn.setAttribute("aria-label", "Open sidebar");
        btn.innerHTML = '<svg class="icon" viewBox="0 0 24 24"><line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="18" x2="20" y2="18"/></svg>';
        btn.onclick = expandSidebar;
        // MUST live inside .layout-shell: the visibility rule is
        // `.layout-shell.sidebar-collapsed .sidebar-reopen-btn`, so a button
        // parented to <body> never matched and could never be clicked.
        const shell = document.querySelector(".layout-shell");
        (shell || document.body).appendChild(btn);
    }
    const handle = document.getElementById("sidebar-collapse-handle");
    if (handle) handle.onclick = collapseSidebar;
    applySidebarCollapseState();
}
