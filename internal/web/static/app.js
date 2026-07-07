// pfwebd dashboard - polls the REST API and renders PF monitoring data.

const REFRESH_MS = 5000;
let timer = null;
let paused = false;
let statesCache = [];
let tablesCache = [];
let selectedTable = null;
let selectedDetail = null;

const $ = (sel) => document.querySelector(sel);

// Escape untrusted text before inserting it into innerHTML templates.
function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

function formatBytes(n) {
  if (n < 1024) return n + " B";
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < units.length - 1);
  return n.toFixed(1) + " " + units[i];
}

function formatNumber(n) {
  return n.toLocaleString("en-US");
}

async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) {
    let msg = res.status + " " + res.statusText;
    try { msg = (await res.json()).error || msg; } catch (_) {}
    throw new Error(url + ": " + msg);
  }
  return res.json();
}

function showError(err) {
  const bar = $("#error-bar");
  bar.textContent = "Error: " + err.message;
  bar.classList.remove("hidden");
}

function clearError() {
  $("#error-bar").classList.add("hidden");
}

function renderStatus(info) {
  const badge = $("#pf-badge");
  badge.textContent = info.enabled ? "PF enabled" : "PF disabled";
  badge.className = "badge " + (info.enabled ? "on" : "off");
  $("#pf-uptime").textContent = info.since ? "up " + info.since : "";

  const stat = (list, name) => (list || []).find((s) => s.name === name);

  const current = stat(info.stateTable, "current entries");
  $("#c-states").textContent = current ? formatNumber(current.total) : "–";

  const searches = stat(info.stateTable, "searches");
  $("#c-searches").textContent = searches ? searches.rate.toFixed(1) : "–";

  const match = stat(info.counters, "match");
  $("#c-match").textContent = match ? formatNumber(match.total) : "–";

  const tbody = $("#counters-table tbody");
  tbody.innerHTML = "";
  for (const s of info.counters || []) {
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${esc(s.name)}</td>` +
      `<td class="num">${formatNumber(s.total)}</td>` +
      `<td class="num">${s.rate.toFixed(1)}/s</td>`;
    tbody.appendChild(tr);
  }
}

function renderStates() {
  const filter = $("#state-filter").value.trim().toLowerCase();
  const tbody = $("#states-table tbody");
  tbody.innerHTML = "";
  for (const s of statesCache) {
    const line = [s.interface, s.proto, s.source, s.destination, s.state]
      .join(" ").toLowerCase();
    if (filter && !line.includes(filter)) continue;
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${esc(s.interface)}</td>` +
      `<td>${esc(s.proto)}</td>` +
      `<td>${esc(s.source)}</td>` +
      `<td class="dir">${esc(s.direction)}</td>` +
      `<td>${esc(s.destination)}</td>` +
      `<td>${esc(s.state)}</td>`;
    tbody.appendChild(tr);
  }
}

function ruleClass(text) {
  if (text.startsWith("block")) return "rule-block";
  if (text.startsWith("pass")) return "rule-pass";
  return "rule-other";
}

function renderRules(rules) {
  $("#c-rules").textContent = formatNumber(rules.length);
  const tbody = $("#rules-table tbody");
  tbody.innerHTML = "";
  for (const r of rules) {
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td class="${ruleClass(r.text)}">${esc(r.text)}</td>` +
      `<td class="num">${formatNumber(r.evaluations)}</td>` +
      `<td class="num">${formatNumber(r.packets)}</td>` +
      `<td class="num">${formatBytes(r.bytes)}</td>` +
      `<td class="num">${formatNumber(r.states)}</td>`;
    tbody.appendChild(tr);
  }
}

// --- PF tables management ---

function tableMsg(text, cls) {
  const el = $("#table-msg");
  el.textContent = text || "";
  el.className = "table-msg" + (cls ? " " + cls : "");
}

function renderTablesList() {
  const ul = $("#tables-list");
  ul.innerHTML = "";
  if (tablesCache.length === 0) {
    ul.innerHTML = `<li class="empty">no tables</li>`;
    return;
  }
  for (const t of tablesCache) {
    const li = document.createElement("li");
    if (t.name === selectedTable) li.classList.add("active");
    li.innerHTML = `<span>${esc(t.name)}</span>` +
      (t.writable ? "" : `<span class="ro">read-only</span>`);
    li.addEventListener("click", () => selectTable(t.name));
    ul.appendChild(li);
  }
}

function renderTableDetail() {
  const ul = $("#addr-list");
  ul.innerHTML = "";
  if (!selectedDetail) {
    ul.innerHTML = `<li class="empty">select a table</li>`;
    return;
  }
  if (selectedDetail.addresses.length === 0) {
    ul.innerHTML = `<li class="empty">empty table</li>`;
  }
  for (const a of selectedDetail.addresses) {
    const li = document.createElement("li");
    const span = document.createElement("span");
    span.textContent = a;
    li.appendChild(span);
    ul.appendChild(li);
  }
}

async function selectTable(name) {
  selectedTable = name;
  tableMsg("");
  try {
    selectedDetail = await fetchJSON("/api/tables/" + encodeURIComponent(name));
  } catch (err) {
    selectedDetail = null;
    tableMsg(err.message, "err");
  }
  renderTablesList();
  renderTableDetail();
}

async function refreshTables() {
  tablesCache = await fetchJSON("/api/tables");
  if (!selectedTable && tablesCache.length > 0) {
    const first = tablesCache.find((t) => t.writable) || tablesCache[0];
    return selectTable(first.name);
  }
  renderTablesList();
  if (selectedTable) {
    try {
      selectedDetail = await fetchJSON(
        "/api/tables/" + encodeURIComponent(selectedTable));
      renderTableDetail();
    } catch (_) { /* keep last known detail during refresh */ }
  }
}

// --- Anchor ruleset (read-only display of the current rules) ---

let anchorState = null;

function renderAnchor() {
  if (!anchorState) return;
  $("#anchor-live").textContent = anchorState.live || "(anchor is empty)";
}

async function refreshAnchor() {
  anchorState = await fetchJSON("/api/anchor");
  renderAnchor();
}

// --- pflog stream (SSE) ---

const LOG_MAX = 500;
let logBuffer = [];
let logPaused = false;

function logLineClass(line) {
  if (line.includes(" block ")) return "log-line block";
  if (line.includes(" pass ")) return "log-line pass";
  return "log-line";
}

function logMatches(line) {
  const f = $("#log-filter").value.trim().toLowerCase();
  return !f || line.toLowerCase().includes(f);
}

function makeLogDiv(line) {
  const div = document.createElement("div");
  div.className = logLineClass(line);
  div.textContent = line;
  return div;
}

function appendLogLine(line) {
  logBuffer.push(line);
  if (logBuffer.length > LOG_MAX) logBuffer.shift();
  if (logPaused || !logMatches(line)) return;
  const view = $("#log-view");
  const atBottom = view.scrollHeight - view.scrollTop - view.clientHeight < 40;
  view.appendChild(makeLogDiv(line));
  while (view.childElementCount > LOG_MAX) view.removeChild(view.firstChild);
  if (atBottom) view.scrollTop = view.scrollHeight;
}

function renderLogs() {
  const view = $("#log-view");
  view.innerHTML = "";
  for (const line of logBuffer) {
    if (logMatches(line)) view.appendChild(makeLogDiv(line));
  }
  view.scrollTop = view.scrollHeight;
}

function connectLogs() {
  // EventSource reconnects automatically on error.
  const es = new EventSource("/api/logs/stream");
  es.onmessage = (e) => appendLogLine(e.data);
  es.onopen = () => {
    const first = $("#log-view").firstChild;
    if (first && first.classList.contains("muted")) first.remove();
  };
}

function setLogPaused(p) {
  logPaused = p;
  $("#log-pause").textContent = logPaused ? "▶" : "⏸";
  $("#log-pause").title = logPaused ? "Resume display" : "Pause display";
  if (!logPaused) renderLogs();
}

// --- Theme ---

function applyTheme(theme) {
  document.documentElement.dataset.theme = theme;
  localStorage.setItem("pfwebd-theme", theme);
  $("#theme-toggle").textContent = theme === "light" ? "🌙" : "☀️";
}

function initTheme() {
  const saved = localStorage.getItem("pfwebd-theme");
  const theme = saved ||
    (matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark");
  applyTheme(theme);
}

// --- Main refresh loop ---

async function refresh() {
  try {
    const [info, states, rules, ifaces] = await Promise.all([
      fetchJSON("/api/status"),
      fetchJSON("/api/states"),
      fetchJSON("/api/rules"),
      fetchJSON("/api/interfaces"),
      refreshTables(),
      refreshAnchor(),
    ]);
    clearError();
    renderStatus(info);
    statesCache = states;
    renderStates();
    renderRules(rules);
    $("#interfaces-raw").textContent = ifaces.raw || "(empty)";
  } catch (err) {
    showError(err);
  }
}

function setPaused(p) {
  paused = p;
  $("#refresh-toggle").textContent = paused ? "▶ Resume" : "⏸ Pause";
  if (paused) {
    clearInterval(timer);
    timer = null;
  } else {
    refresh();
    timer = setInterval(refresh, REFRESH_MS);
  }
}

$("#refresh-toggle").addEventListener("click", () => setPaused(!paused));
$("#state-filter").addEventListener("input", renderStates);
$("#theme-toggle").addEventListener("click", () => {
  const cur = document.documentElement.dataset.theme || "dark";
  applyTheme(cur === "dark" ? "light" : "dark");
});
$("#log-filter").addEventListener("input", renderLogs);
$("#log-pause").addEventListener("click", () => setLogPaused(!logPaused));
$("#log-clear").addEventListener("click", () => { logBuffer = []; renderLogs(); });

initTheme();
setPaused(false);
connectLogs();
