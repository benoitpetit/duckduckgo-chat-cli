const $ = (selector, root = document) => root.querySelector(selector);
const state = { settings: null, stats: null, statsScope: "all", sessions: [], timer: null, selectedSession: "", toastTimer: null, modelSignature: "", activitySource: null, activityEvents: [], activityIds: new Set(), activeOperations: new Map(), activityVisited: false };

async function api(path, options = {}) {
  const response = await fetch(path, { cache: "no-store", ...options });
  if (!response.ok) {
    const message = (await response.text()).trim();
    throw new Error(message || `HTTP error ${response.status}`);
  }
  return response.json();
}

function field(object, lower, upper) { return object?.[lower] ?? object?.[upper]; }
function number(value) { return new Intl.NumberFormat("en-US").format(Number(value || 0)); }
function percent(done, total) { return total ? `${(100 * done / total).toFixed(1)}%` : "—"; }
function duration(value) {
  const ms = Number(value || 0) / 1e6;
  return ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(2)} s`;
}
function date(value, withTime = false) {
  if (!value) return "Unknown date";
  const parsed = new Date(value);
  return new Intl.DateTimeFormat("en-US", withTime ? { dateStyle: "medium", timeStyle: "short" } : { dateStyle: "medium" }).format(parsed);
}
function notify(message) {
  const toast = $("#toast");
  toast.textContent = message;
  toast.classList.add("show");
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(() => toast.classList.remove("show"), 3200);
}

function showPage(name) {
  if (name === "sessions" && !state.settings?.showConversations) name = "overview";
  document.querySelectorAll(".page").forEach(page => page.classList.toggle("active", page.id === `page-${name}`));
  document.querySelectorAll(".nav-item").forEach(button => button.classList.toggle("active", button.dataset.page === name));
  const titles = { overview: "Overview", sessions: "Conversations", commands: "Commands", activity: "Activity" };
  $("#pageTitle").textContent = titles[name] || titles.overview;
  if (name === "sessions") loadSessions();
  if (name === "commands") loadCommands();
  if (name === "activity" && !state.activityVisited) {
    const root = $("#activityConsole");
    root.scrollTop = root.scrollHeight;
    state.activityVisited = true;
  }
}

function setActivityConnection(label, status) {
  const text = $("#activityConnection");
  if (!text) return;
  text.textContent = label;
  $("#activityConnectionDot").dataset.state = status;
}

function activityAtBottom(root) {
  return root.scrollHeight - root.scrollTop - root.clientHeight < 48;
}

function redactActivityContent() {
  state.activityEvents = state.activityEvents.map(event => ({
    ...event,
    prompt: "",
    response: "",
    summary: event.category === "conversation" ? (event.status === "prompt" ? "User prompt submitted (hidden)" : "Assistant response received (hidden)") : event.summary,
  }));
  renderActivityEvents(false);
}

function updateActivityCurrent(event) {
  if (event.operationId && event.status === "started") state.activeOperations.set(event.operationId, event.summary);
  if (event.operationId && ["completed", "failed", "cancelled"].includes(event.status)) state.activeOperations.delete(event.operationId);
  const operations = [...state.activeOperations.values()];
  $("#activityCurrent").textContent = operations.at(-1) || "Idle";
}

function appendActivity(event) {
  if (event.category === "privacy" && event.status === "redacted") redactActivityContent();
  if (!state.activityIds.has(event.id)) {
    state.activityIds.add(event.id);
    state.activityEvents.push(event);
    if (state.activityEvents.length > 200) {
      const removed = state.activityEvents.shift();
      state.activityIds.delete(removed.id);
    }
  }
  updateActivityCurrent(event);
  renderActivityEvents(true);
}

function renderActivityEvents(allowFollow) {
  const root = $("#activityConsole");
  if (!root) return;
  const follow = activityAtBottom(root);
  const previousScrollTop = root.scrollTop;
  root.replaceChildren();
  if (!state.activityEvents.length) {
    const empty = document.createElement("div"); empty.className = "empty-state"; empty.textContent = "Activity events will appear here."; root.append(empty);
  }
  state.activityEvents.forEach(event => {
    const row = document.createElement("article"); row.className = "activity-entry"; row.dataset.category = event.category || "event";
    const meta = document.createElement("div"); meta.className = "activity-meta";
    const timestamp = document.createElement("time"); timestamp.dateTime = event.timestamp || "";
    timestamp.textContent = event.timestamp ? new Intl.DateTimeFormat("en-US", { timeStyle: "medium" }).format(new Date(event.timestamp)) : "--:--:--";
    const category = document.createElement("span"); category.className = "activity-category"; category.textContent = (event.category || "event").toUpperCase();
    const status = document.createElement("span"); status.className = "activity-status"; status.dataset.state = event.status || "info"; status.textContent = event.status || "info";
    meta.append(timestamp, category, status);
    const summary = document.createElement("p"); summary.className = "activity-summary"; summary.textContent = event.summary || "Activity";
    row.append(meta, summary);
    if (event.model) { const model = document.createElement("span"); model.className = "activity-model"; model.textContent = event.model; row.append(model); }
    if (state.settings?.showConversationContent && event.prompt) {
      const prompt = document.createElement("pre"); prompt.className = "activity-content"; prompt.textContent = `Prompt\n${event.prompt}`; row.append(prompt);
    }
    if (state.settings?.showConversationContent && event.response) {
      const response = document.createElement("pre"); response.className = "activity-content"; response.textContent = `Response\n${event.response}`; row.append(response);
    }
    root.append(row);
  });
  $("#activityCount").textContent = `${state.activityEvents.length} recent event${state.activityEvents.length === 1 ? "" : "s"}`;
  const button = $("#activityLatest");
  button.hidden = follow;
  if (allowFollow && follow) root.scrollTop = root.scrollHeight;
  else if (!follow) root.scrollTop = previousScrollTop;
}

function connectActivity() {
  if (state.activitySource) return;
  const source = new EventSource("/api/activity/stream");
  state.activitySource = source;
  source.onopen = () => setActivityConnection("Live", "live");
  source.onerror = () => setActivityConnection(source.readyState === EventSource.CLOSED ? "Disconnected" : "Reconnecting…", source.readyState === EventSource.CLOSED ? "offline" : "reconnecting");
  source.addEventListener("activity", message => {
    try { appendActivity(JSON.parse(message.data)); }
    catch { setActivityConnection("Invalid activity event", "offline"); }
  });
  source.addEventListener("state", message => {
    try { $("#activityCurrent").textContent = JSON.parse(message.data).current || "Idle"; }
    catch { $("#activityCurrent").textContent = "Idle"; }
  });
}

function renderMetrics(current, history = [], sessionCount = 0) {
  const interactions = field(current, "chat_interactions_total", "ChatInteractionsTotal") || 0;
  const successful = field(current, "chat_interactions_successful", "ChatInteractionsSuccessful") || 0;
  const failed = field(current, "chat_interactions_failed", "ChatInteractionsFailed") || 0;
  const total = interactions || successful + failed;
  $("#metricInteractions").textContent = number(total);
  $("#metricSuccess").textContent = percent(successful, total);
  $("#metricLatency").textContent = total ? duration(field(current, "average_chat_response_time", "AverageChatResponseTime")) : "—";
  $("#metricMessages").textContent = number(field(current, "messages_total", "MessagesTotal"));
  $("#metricTokens").textContent = `~${number(field(current, "total_tokens_estimate", "TotalTokensEstimate"))}`;
  $("#metricOptimizations").textContent = number(field(current, "context_optimizations", "ContextOptimizations"));
  $("#metricBytes").textContent = `${number(field(current, "bytes_saved", "BytesSaved"))} bytes saved`;
  $("#metricErrors").textContent = number(field(current, "chat_interactions_failed", "ChatInteractionsFailed"));
  $("#metricErrorRate").textContent = `${percent(field(current, "chat_interactions_failed", "ChatInteractionsFailed"), total)} of requests`;
  $("#metricSearches").textContent = number(field(current, "searches_performed", "SearchesPerformed"));
  $("#metricFiles").textContent = number(field(current, "files_processed", "FilesProcessed"));
  $("#metricURLs").textContent = number(field(current, "urls_processed", "URLsProcessed"));
  const all = state.statsScope === "all";
  const scope = all ? "across retained sessions" : "in this session";
  $("#statsScopeDescription").textContent = all
    ? `Combined metrics across ${number(sessionCount)} retained session${sessionCount === 1 ? "" : "s"}, including the current session.`
    : "Live metrics for the current session.";
  $("#commandScopeDescription").textContent = `Most used in ${all ? "retained sessions" : "the current session"}.`;
  $("#modelScopeDescription").textContent = `Based on responses observed ${scope}.`;
  const labels = {
    metricInteractions: all ? "requests across sessions" : "requests sent",
    metricSuccess: "responses received",
    metricLatency: "average response time",
    metricMessages: all ? "messages across sessions" : "messages in this session",
    metricTokens: all ? "estimated tokens across sessions" : "local estimate",
    metricSearches: all ? "searches across sessions" : "searches performed",
    metricFiles: all ? "files across sessions" : "files processed",
    metricURLs: all ? "URLs across sessions" : "pages added to context",
  };
  Object.entries(labels).forEach(([id, label]) => $(`#${id}`).parentElement.querySelector("small").textContent = label);
  renderMetricSparklines(state.stats?.current || current, history);

  const commands = field(current, "commands_used", "CommandsUsed") || {};
  const ranked = Object.entries(commands).sort((a, b) => b[1] - a[1]).slice(0, 6);
  const commandRoot = $("#commandStats");
  commandRoot.replaceChildren();
  if (!ranked.length) commandRoot.innerHTML = '<div class="empty-state">No commands recorded yet.</div>';
  ranked.forEach(([name, count], index) => {
    const row = document.createElement("div"); row.className = "rank-row";
    const rank = document.createElement("span"); rank.className = "rank"; rank.textContent = String(index + 1).padStart(2, "0");
    const label = document.createElement("code"); label.textContent = name;
    const amount = document.createElement("span"); amount.className = "count"; amount.textContent = number(count);
    row.append(rank, label, amount); commandRoot.append(row);
  });

  const models = field(current, "by_model", "ByModel") || {};
  const modelNames = new Map((state.settings?.models || []).map(item => [item.id, item.name]));
  const modelRoot = $("#modelStats"); modelRoot.replaceChildren();
  const entries = Object.entries(models).sort((a, b) => b[1].interactions - a[1].interactions);
  if (!entries.length) {
    const row = document.createElement("tr"); row.innerHTML = '<td colspan="4" class="empty-state">Per-model data will appear after a request.</td>'; modelRoot.append(row);
  }
  entries.forEach(([id, metrics]) => {
    const row = document.createElement("tr");
    const values = [modelNames.get(id) || id, number(metrics.interactions), percent(metrics.successful, metrics.interactions), duration(metrics.average_response_time)];
    values.forEach(value => { const cell = document.createElement("td"); cell.textContent = value; row.append(cell); });
    modelRoot.append(row);
  });
}

function renderActivityGrid(days) {
  const grid = $("#contributionGrid");
  if (!grid) return;
  grid.replaceChildren();
  const rows = Array.isArray(days) ? days : [];
  if (!rows.length) {
    const body = document.createElement("tbody"); const row = document.createElement("tr"); const cell = document.createElement("td");
    cell.colSpan = 2; cell.className = "empty-state"; cell.textContent = "No activity history available."; row.append(cell); body.append(row); grid.append(body); return;
  }
  const firstDate = new Date(`${rows[0].date}T12:00:00`);
  const leading = firstDate.getDay();
  const columns = Math.ceil((leading + rows.length) / 7);
  const weeks = Array.from({ length: 7 }, () => Array(columns).fill(null));
  const monthLabels = Array(columns).fill("");
  let previousMonth = "";
  rows.forEach((day, index) => {
    const parsed = new Date(`${day.date}T12:00:00`);
    const month = `${parsed.getFullYear()}-${parsed.getMonth()}`;
    const column = Math.floor((leading + index) / 7);
    weeks[parsed.getDay()][column] = day;
    if (month !== previousMonth) {
      monthLabels[column] = new Intl.DateTimeFormat("en-US", { month: "short" }).format(parsed);
      previousMonth = month;
    }
  });
  const maximum = Math.max(1, ...rows.map(day => Number(day.userMessages || 0)));
  const head = document.createElement("thead"); const header = document.createElement("tr");
  const corner = document.createElement("th"); corner.scope = "col"; corner.textContent = "Day"; header.append(corner);
  monthLabels.forEach(label => { const cell = document.createElement("th"); cell.scope = "col"; cell.textContent = label; header.append(cell); });
  head.append(header); grid.append(head);
  const body = document.createElement("tbody");
  const weekdays = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
  weeks.forEach((week, weekday) => {
    const row = document.createElement("tr"); const heading = document.createElement("th"); heading.scope = "row"; heading.textContent = weekdays[weekday].slice(0, 3); heading.title = weekdays[weekday]; row.append(heading);
    week.forEach(day => {
      const cell = document.createElement("td");
      if (day) {
        const count = Number(day.userMessages || 0);
        const level = day.available ? (count ? Math.max(1, Math.ceil(count * 4 / maximum)) : 0) : "unavailable";
        const label = `${day.date}: ${day.available ? `${number(count)} user message${count === 1 ? "" : "s"}` : "activity not tracked"}`;
        const square = document.createElement("span"); square.className = "activity-cell"; square.dataset.level = String(level); square.title = label;
        const accessibleLabel = document.createElement("span"); accessibleLabel.className = "visually-hidden"; accessibleLabel.textContent = label; square.append(accessibleLabel); cell.append(square);
      }
      row.append(cell);
    });
    body.append(row);
  });
  grid.append(body);
}

function renderTokenComposition(current) {
  const root = $("#tokenComposition"); if (!root) return;
  const parts = [
    ["User", Number(field(current, "user_tokens_estimate", "UserTokensEstimate") || 0)],
    ["Assistant", Number(field(current, "assistant_tokens_estimate", "AssistantTokensEstimate") || 0)],
    ["Context", Number(field(current, "context_tokens_estimate", "ContextTokensEstimate") || 0)],
  ];
  const max = Math.max(1, ...parts.map(([, value]) => value));
  root.replaceChildren();
  parts.forEach(([label, value]) => {
    const row = document.createElement("div"); row.className = "stat-chart-row";
    const heading = document.createElement("div"); heading.className = "stat-chart-heading";
    const name = document.createElement("span"); name.textContent = label;
    const amount = document.createElement("strong"); amount.textContent = `~${number(value)}`; heading.append(name, amount);
    const track = document.createElement("div"); track.className = "stat-bar-track";
    const fill = document.createElement("i"); fill.style.width = `${100 * value / max}%`; track.append(fill);
    row.append(heading, track); root.append(row);
  });
}

function renderModelComparison(current) {
  const root = $("#modelComparison"); if (!root) return;
  const models = field(current, "by_model", "ByModel") || {};
  const entries = Object.entries(models).sort((a, b) => b[1].interactions - a[1].interactions);
  root.replaceChildren();
  if (!entries.length) { const empty = document.createElement("div"); empty.className = "empty-state"; empty.textContent = "Model comparisons appear after a request."; root.append(empty); return; }
  const names = new Map((state.settings?.models || []).map(item => [item.id, item.name]));
  const max = Math.max(1, ...entries.map(([, metrics]) => Number(metrics.interactions || 0)));
  entries.forEach(([id, metrics]) => {
    const row = document.createElement("div"); row.className = "stat-chart-row";
    const heading = document.createElement("div"); heading.className = "stat-chart-heading";
    const name = document.createElement("span"); name.textContent = names.get(id) || id;
    const amount = document.createElement("strong"); amount.textContent = `${number(metrics.interactions)} · ${percent(metrics.successful, metrics.interactions)}`; heading.append(name, amount);
    const track = document.createElement("div"); track.className = "stat-bar-track";
    const fill = document.createElement("i"); fill.style.width = `${100 * Number(metrics.interactions || 0) / max}%`; track.append(fill);
    const latency = document.createElement("small"); latency.textContent = `Average response ${duration(metrics.average_response_time)}`;
    row.append(heading, track, latency); root.append(row);
  });
}

function renderRecoveryChart(current) {
  const root = $("#recoveryChart"); if (!root) return;
  const rows = [
    ["Failed requests", Number(field(current, "chat_interactions_failed", "ChatInteractionsFailed") || 0)],
    ["VQD refreshes", Number(field(current, "vqd_refresh_count", "VQDRefreshCount") || 0)],
    ["Header refreshes", Number(field(current, "header_refresh_count", "HeaderRefreshCount") || 0)],
  ];
  const max = Math.max(1, ...rows.map(([, value]) => value)); root.replaceChildren();
  rows.forEach(([label, value]) => {
    const row = document.createElement("div"); row.className = "stat-chart-row";
    const heading = document.createElement("div"); heading.className = "stat-chart-heading";
    const name = document.createElement("span"); name.textContent = label;
    const amount = document.createElement("strong"); amount.textContent = number(value); heading.append(name, amount);
    const track = document.createElement("div"); track.className = "stat-bar-track"; track.dataset.tone = label === "Failed requests" ? "danger" : "orange";
    const fill = document.createElement("i"); fill.style.width = `${100 * value / max}%`; track.append(fill);
    row.append(heading, track); root.append(row);
  });
}

function renderMetricSparklines(current, history) {
  const totalFor = snapshot => Number(field(snapshot, "chat_interactions_total", "ChatInteractionsTotal")) ||
    (Number(field(snapshot, "chat_interactions_successful", "ChatInteractionsSuccessful")) || 0) +
    (Number(field(snapshot, "chat_interactions_failed", "ChatInteractionsFailed")) || 0);
  const successfulFor = snapshot => Number(field(snapshot, "chat_interactions_successful", "ChatInteractionsSuccessful")) || 0;
  const metrics = [
    { id: "chartInteractions", label: "Interactions", tone: "orange", read: totalFor },
    { id: "chartSuccess", label: "Success rate", tone: "green", read: snapshot => totalFor(snapshot) ? successfulFor(snapshot) * 100 / totalFor(snapshot) : 0 },
    { id: "chartLatency", label: "Average latency", tone: "blue", read: snapshot => Number(field(snapshot, "average_chat_response_time", "AverageChatResponseTime")) || 0 },
    { id: "chartMessages", label: "Messages", tone: "orange", read: snapshot => Number(field(snapshot, "messages_total", "MessagesTotal")) || 0 },
    { id: "chartTokens", label: "Estimated tokens", tone: "gold", read: snapshot => Number(field(snapshot, "total_tokens_estimate", "TotalTokensEstimate")) || 0 },
    { id: "chartOptimizations", label: "Context optimizations", tone: "green", read: snapshot => Number(field(snapshot, "context_optimizations", "ContextOptimizations")) || 0 },
    { id: "chartErrors", label: "Errors", tone: "danger", read: snapshot => Number(field(snapshot, "chat_interactions_failed", "ChatInteractionsFailed")) || 0 },
    { id: "chartSearches", label: "Searches", tone: "blue", read: snapshot => Number(field(snapshot, "searches_performed", "SearchesPerformed")) || 0 },
    { id: "chartFiles", label: "Fichiers", tone: "orange", read: snapshot => Number(field(snapshot, "files_processed", "FilesProcessed")) || 0 },
    { id: "chartURLs", label: "URL", tone: "gold", read: snapshot => Number(field(snapshot, "urls_processed", "URLsProcessed")) || 0 },
  ];
  const currentStart = field(current, "session_start_time", "SessionStartTime");
  const priorSessions = Array.isArray(history) ? history.filter(snapshot => field(snapshot, "session_start_time", "SessionStartTime") !== currentStart).slice(-7) : [];
  const sessions = priorSessions.concat(current || {});
  metrics.forEach(metric => drawSparkline($(`#${metric.id}`), sessions.map(metric.read), metric.label, metric.tone));
}

function drawSparkline(root, samples, label, tone) {
  if (!root) return;
  root.replaceChildren();
  root.dataset.tone = tone;
  root.setAttribute("role", "img");
  root.setAttribute("aria-label", `${label} trend across ${samples.length} session${samples.length === 1 ? "" : "s"}`);

  const svgNS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(svgNS, "svg");
  svg.setAttribute("viewBox", "0 0 120 28");
  svg.setAttribute("preserveAspectRatio", "none");
  svg.setAttribute("focusable", "false");
  if (samples.length === 1) {
    const point = document.createElementNS(svgNS, "circle");
    point.setAttribute("cx", "116"); point.setAttribute("cy", "14"); point.setAttribute("r", "2.5");
    point.setAttribute("class", "sparkline-point"); svg.append(point);
  } else {
    const min = Math.min(...samples); const max = Math.max(...samples); const range = max - min || 1;
    const points = samples.map((value, index) => {
      const x = 2 + (index * 116 / (samples.length - 1));
      const y = 24 - ((value - min) * 19 / range);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    const line = document.createElementNS(svgNS, "polyline");
    line.setAttribute("points", points.join(" ")); line.setAttribute("class", "sparkline-line");
    svg.append(line);
    const [x, y] = points[points.length - 1].split(",");
    const point = document.createElementNS(svgNS, "circle");
    point.setAttribute("cx", x); point.setAttribute("cy", y); point.setAttribute("r", "2.5");
    point.setAttribute("class", "sparkline-point"); svg.append(point);
  }
  root.append(svg);
}

function renderHistory(history) {
  const root = $("#historyChart"); root.replaceChildren();
  const entries = Array.isArray(history) ? history.slice(-12) : [];
  if (!entries.length) {
    const empty = document.createElement("div"); empty.className = "empty-state"; empty.textContent = "Trends will appear after a session is recorded."; root.append(empty); return;
  }
  const max = Math.max(1, ...entries.map(item => item.messages_total || 0));
  entries.forEach(item => {
    const bar = document.createElement("div"); bar.className = "history-bar";
    const value = document.createElement("b"); value.textContent = number(item.messages_total || 0);
    const fill = document.createElement("i"); fill.style.height = `${Math.max(4, 100 * (item.messages_total || 0) / max)}%`;
    const label = document.createElement("span"); label.textContent = date(item.session_start_time).replace(/,?\s+\d{4}$/, "");
    bar.append(value, fill, label); root.append(bar);
  });
}

async function refreshStats() {
  try {
    await refreshSettings();
    const data = await api("/api/stats");
    state.stats = data;
    renderSelectedStats();
    renderHistory(data.history || []);
    const activityDays = data.dailyActivity || [];
    $("#activityGridWindow").textContent = `LAST ${activityDays.length || Number(state.settings?.retentionDays) || 90} DAYS`;
    renderActivityGrid(activityDays);
    $("#updatedAt").textContent = `Updated at ${new Intl.DateTimeFormat("en-US", { timeStyle: "medium" }).format(new Date(data.updatedAt))}`;
  } catch (error) {
    $("#updatedAt").textContent = "Local connection interrupted";
    notify(`Usage stats unavailable: ${error.message}`);
  }
}

function renderSelectedStats() {
  if (!state.stats) return;
  const snapshot = state.statsScope === "all" ? state.stats.allSessions || {} : state.stats.current || {};
  renderMetrics(snapshot, state.stats.history || [], Number(state.stats.sessionCount || 0));
  renderTokenComposition(snapshot);
  renderModelComparison(snapshot);
  renderRecoveryChart(snapshot);
  document.querySelectorAll("[data-stats-scope]").forEach(button => {
    const selected = button.dataset.statsScope === state.statsScope;
    button.classList.toggle("active", selected);
    button.setAttribute("aria-pressed", String(selected));
  });
}

async function refreshSettings() {
  const settings = await api("/api/settings");
  const modelSignature = JSON.stringify([settings.currentModel, settings.models || []]);
  const hadConversationContent = Boolean(state.settings?.showConversationContent);
  state.settings = settings;
  if (hadConversationContent && !settings.showConversationContent) redactActivityContent();
  if (!settings.showConversationContent) state.activityEvents = state.activityEvents.map(event => ({ ...event, prompt: "", response: "" }));
  const showConversations = Boolean(settings.showConversations);
  const allowConversationAnalysis = Boolean(settings.allowConversationAnalysis);
  $("#sessionsNav").hidden = !showConversations;
  $("#conversationAnalysis").hidden = !allowConversationAnalysis;
  if (!showConversations) {
    state.sessions = [];
    state.selectedSession = "";
    $("#sessionList").replaceChildren();
    $("#sessionDetail").textContent = "Conversation browsing is disabled in dashboard settings.";
    if (document.querySelector("#page-sessions.active")) showPage("overview");
  }
  if (!allowConversationAnalysis) {
    $("#reportPreview").hidden = true;
    $("#conversationResult").hidden = true;
    $("#conversationResult").textContent = "";
  }
  if (modelSignature !== state.modelSignature) {
    state.modelSignature = modelSignature;
    renderModels();
  }
}

function renderModels() {
  const select = $("#analysisModel"); select.replaceChildren();
  (state.settings?.models || []).forEach(model => {
    const option = document.createElement("option"); option.value = model.id; option.textContent = model.name; option.title = model.description; select.append(option);
  });
  if (state.settings?.currentModel) select.value = state.settings.currentModel;
}

async function runMetricsAnalysis() {
  const button = $("#metricsAnalysis"); button.disabled = true; button.textContent = "Analyzing…";
  const result = $("#metricsResult"); result.hidden = false; result.textContent = "The model is reviewing aggregated metrics…";
  try {
    const response = await api("/api/analysis/metrics", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ model: $("#analysisModel").value }) });
    result.textContent = response.result || "No result returned.";
  } catch (error) { result.textContent = `Analysis failed: ${error.message}`; }
  finally { button.disabled = false; button.innerHTML = 'Analyze usage <span>→</span>'; }
}

async function previewConversationReport() {
  const button = $("#previewReport"); button.disabled = true;
  try {
    const estimate = await api("/api/analysis/conversations/preview");
    $("#reportEstimate").textContent = `${number(estimate.includedSessions)} sessions · ~${number(estimate.estimatedTokens)} estimated tokens of ${number(estimate.tokenBudget)}. This estimate assumes about 4 characters per token; actual usage may differ.`;
    $("#reportPreview").hidden = false;
  } catch (error) { notify(`Could not estimate report: ${error.message}`); }
  finally { button.disabled = false; }
}

async function runConversationReport() {
  const button = $("#runReport"); button.disabled = true;
  const result = $("#conversationResult"); result.hidden = false; result.textContent = "Generating report…";
  try {
    const response = await api("/api/analysis/conversations", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ model: $("#analysisModel").value }) });
    result.textContent = `${response.includedSessions} sessions · ~${number(response.estimatedTokens)} estimated tokens\n\n${response.result || "No result returned."}`;
  } catch (error) { result.textContent = `Analysis failed: ${error.message}`; }
  finally { button.disabled = false; }
}

async function loadSessions() {
  if (!state.settings?.showConversations) return;
  const query = $("#sessionSearch").value.trim();
  const root = $("#sessionList");
  root.innerHTML = '<div class="empty-state">Loading sessions…</div>';
  try {
    const sessions = await api(`/api/sessions?query=${encodeURIComponent(query)}`);
    state.sessions = sessions;
    root.replaceChildren();
    if (!sessions.length) { const empty = document.createElement("div"); empty.className = "empty-state"; empty.textContent = "No archived conversations match your search."; root.append(empty); return; }
    sessions.forEach(session => {
      const button = document.createElement("button"); button.className = `session-item${state.selectedSession === session.id ? " selected" : ""}`;
      const title = document.createElement("strong"); title.textContent = session.first_message || "New conversation";
      const meta = document.createElement("small"); meta.textContent = `${date(session.start_time, true)} · ${session.model || "Unknown model"} · ${number(session.message_count)} messages`;
      const id = document.createElement("span"); id.textContent = session.id;
      button.append(title, meta, id); button.addEventListener("click", () => loadSessionDetail(session)); root.append(button);
    });
  } catch (error) { root.innerHTML = `<div class="empty-state">Sessions unavailable: ${escapeText(error.message)}</div>`; }
}

async function loadSessionDetail(summary) {
  state.selectedSession = summary.id;
  const root = $("#sessionDetail"); root.innerHTML = '<div class="empty-state">Loading conversation…</div>';
  try {
    const session = await api(`/api/sessions/${encodeURIComponent(summary.id)}`);
    root.replaceChildren();
    const head = document.createElement("div"); head.className = "detail-meta";
    const info = document.createElement("div");
    const title = document.createElement("strong"); title.textContent = summary.first_message || "New conversation";
    const meta = document.createElement("p"); meta.textContent = `${date(summary.start_time, true)} · ${session.model || "Unknown model"} · ${number(summary.message_count)} messages`;
    info.append(title, meta);
    const copy = document.createElement("button"); copy.className = "button secondary"; copy.textContent = "Copy resume command";
    copy.addEventListener("click", async () => {
      try { await navigator.clipboard.writeText(summary.resume_command); notify(`Command copied: ${summary.resume_command}`); }
      catch { notify(`Copy this into the terminal: ${summary.resume_command}`); }
    });
    head.append(info, copy);
    const transcript = document.createElement("div"); transcript.className = "transcript";
    (session.messages || session.Messages || []).forEach(message => {
      const article = document.createElement("div"); article.className = `message ${(message.role || message.Role) === "user" ? "user" : "assistant"}`;
      const role = document.createElement("span"); role.className = "message-role"; role.textContent = (message.role || message.Role) === "user" ? "You" : "Assistant";
      const content = document.createElement("span"); content.textContent = message.content || message.Content || "";
      article.append(role, content); transcript.append(article);
    });
    root.append(head, transcript);
    loadSessions();
  } catch (error) { root.textContent = `Conversation unavailable: ${error.message}`; }
}

function escapeText(value) {
  const node = document.createElement("span"); node.textContent = value; return node.innerHTML;
}

async function loadCommands() {
  const root = $("#commandDocs");
  try {
    const data = await api("/api/commands");
    const commands = data.commands || [];
    const groups = new Map();
    commands.forEach(command => {
      const category = field(command, "category", "Category") || "other";
      if (!groups.has(category)) groups.set(category, []);
      groups.get(category).push(command);
    });
    root.replaceChildren();
    if (!commands.length) { root.textContent = "No documented commands."; return; }
    [...groups.keys()].sort().forEach(category => {
      const group = document.createElement("article"); group.className = "command-group";
      const heading = document.createElement("h3"); heading.textContent = category; group.append(heading);
      groups.get(category).sort((a, b) => String(field(a, "name", "Name")).localeCompare(String(field(b, "name", "Name")))).forEach(command => {
        const card = document.createElement("div"); card.className = "command-card";
        const usage = document.createElement("code"); usage.textContent = field(command, "usage", "Usage") || field(command, "name", "Name");
        const description = document.createElement("p"); description.textContent = field(command, "description", "Description") || "";
        card.append(usage, description);
        const examples = field(command, "examples", "Examples") || [];
        if (examples.length) { const example = document.createElement("small"); example.textContent = `Example: ${examples.join(" · ")}`; card.append(example); }
        group.append(card);
      });
      root.append(group);
    });
  } catch (error) { root.innerHTML = `<div class="empty-state">Documentation unavailable: ${escapeText(error.message)}</div>`; }
}

async function start() {
  document.querySelectorAll(".nav-item").forEach(button => button.addEventListener("click", () => showPage(button.dataset.page)));
  document.querySelectorAll("[data-stats-scope]").forEach(button => button.addEventListener("click", () => {
    state.statsScope = button.dataset.statsScope;
    renderSelectedStats();
  }));
  $("#logoutButton").addEventListener("click", async () => {
    try {
      const response = await fetch("/auth/logout", { method: "POST", cache: "no-store" });
      if (!response.ok) throw new Error("Sign-out failed");
      window.location.assign("/login");
    } catch (error) {
      notify(error.message || "Sign-out failed");
    }
  });
  $("#metricsAnalysis").addEventListener("click", runMetricsAnalysis);
  $("#previewReport").addEventListener("click", previewConversationReport);
  $("#runReport").addEventListener("click", runConversationReport);
  $("#activityLatest").addEventListener("click", () => { const root = $("#activityConsole"); root.scrollTop = root.scrollHeight; $("#activityLatest").hidden = true; });
  $("#activityConsole").addEventListener("scroll", () => { $("#activityLatest").hidden = activityAtBottom($("#activityConsole")); });
  let searchTimer;
  $("#sessionSearch").addEventListener("input", () => { clearTimeout(searchTimer); searchTimer = setTimeout(loadSessions, 180); });
  try {
    await refreshSettings();
    connectActivity();
    await Promise.all([refreshStats(), loadCommands()]);
    scheduleRefresh();
  } catch (error) { notify(`Dashboard configuration unavailable: ${error.message}`); }
}

function scheduleRefresh() {
  clearTimeout(state.timer);
  state.timer = setTimeout(async () => {
    await refreshStats();
    scheduleRefresh();
  }, Math.max(1, state.settings?.refreshIntervalSeconds || 3) * 1000);
}

start();
