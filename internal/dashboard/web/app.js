const $ = (selector, root = document) => root.querySelector(selector);
const state = { settings: null, stats: null, sessions: [], timer: null, selectedSession: "", toastTimer: null };

async function api(path, options = {}) {
  const response = await fetch(path, { cache: "no-store", ...options });
  if (!response.ok) {
    const message = (await response.text()).trim();
    throw new Error(message || `Erreur HTTP ${response.status}`);
  }
  return response.json();
}

function field(object, lower, upper) { return object?.[lower] ?? object?.[upper]; }
function number(value) { return new Intl.NumberFormat("fr-FR").format(Number(value || 0)); }
function percent(done, total) { return total ? `${(100 * done / total).toFixed(1).replace(".", ",")} %` : "—"; }
function duration(value) {
  const ms = Number(value || 0) / 1e6;
  return ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(2).replace(".", ",")} s`;
}
function date(value, withTime = false) {
  if (!value) return "Date inconnue";
  const parsed = new Date(value);
  return new Intl.DateTimeFormat("fr-FR", withTime ? { dateStyle: "medium", timeStyle: "short" } : { dateStyle: "medium" }).format(parsed);
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
  const titles = { overview: "Vue d’ensemble", sessions: "Conversations", commands: "Commandes" };
  $("#pageTitle").textContent = titles[name] || titles.overview;
  if (name === "sessions") loadSessions();
  if (name === "commands") loadCommands();
}

function renderMetrics(current) {
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
  $("#metricBytes").textContent = `${number(field(current, "bytes_saved", "BytesSaved"))} octets économisés`;

  const commands = field(current, "commands_used", "CommandsUsed") || {};
  const ranked = Object.entries(commands).sort((a, b) => b[1] - a[1]).slice(0, 6);
  const commandRoot = $("#commandStats");
  commandRoot.replaceChildren();
  if (!ranked.length) commandRoot.innerHTML = '<div class="empty-state">Aucune commande enregistrée pour le moment.</div>';
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
    const row = document.createElement("tr"); row.innerHTML = '<td colspan="4" class="empty-state">Les données par modèle apparaîtront après une requête.</td>'; modelRoot.append(row);
  }
  entries.forEach(([id, metrics]) => {
    const row = document.createElement("tr");
    const values = [modelNames.get(id) || id, number(metrics.interactions), percent(metrics.successful, metrics.interactions), duration(metrics.average_response_time)];
    values.forEach(value => { const cell = document.createElement("td"); cell.textContent = value; row.append(cell); });
    modelRoot.append(row);
  });
}

function renderHistory(history) {
  const root = $("#historyChart"); root.replaceChildren();
  const entries = Array.isArray(history) ? history.slice(-12) : [];
  if (!entries.length) {
    const empty = document.createElement("div"); empty.className = "empty-state"; empty.textContent = "Les tendances apparaîtront après l’enregistrement d’une session."; root.append(empty); return;
  }
  const max = Math.max(1, ...entries.map(item => item.messages_total || 0));
  entries.forEach(item => {
    const bar = document.createElement("div"); bar.className = "history-bar";
    const value = document.createElement("b"); value.textContent = number(item.messages_total || 0);
    const fill = document.createElement("i"); fill.style.height = `${Math.max(4, 100 * (item.messages_total || 0) / max)}%`;
    const label = document.createElement("span"); label.textContent = date(item.session_start_time).replace(/\s+\d{4}$/, "");
    bar.append(value, fill, label); root.append(bar);
  });
}

async function refreshStats() {
  try {
    const data = await api("/api/stats");
    state.stats = data;
    renderMetrics(data.current || {});
    renderHistory(data.history || []);
    $("#updatedAt").textContent = `Mis à jour à ${new Intl.DateTimeFormat("fr-FR", { timeStyle: "medium" }).format(new Date(data.updatedAt))}`;
  } catch (error) {
    $("#updatedAt").textContent = "Connexion locale interrompue";
    notify(`Statistiques indisponibles : ${error.message}`);
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
  const button = $("#metricsAnalysis"); button.disabled = true; button.textContent = "Analyse en cours…";
  const result = $("#metricsResult"); result.hidden = false; result.textContent = "Le modèle examine les métriques agrégées…";
  try {
    const response = await api("/api/analysis/metrics", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ model: $("#analysisModel").value }) });
    result.textContent = response.result || "Aucun résultat fourni.";
  } catch (error) { result.textContent = `Analyse impossible : ${error.message}`; }
  finally { button.disabled = false; button.innerHTML = 'Analyser les statistiques <span>→</span>'; }
}

async function previewConversationReport() {
  const button = $("#previewReport"); button.disabled = true;
  try {
    const estimate = await api("/api/analysis/conversations/preview");
    $("#reportEstimate").textContent = `${number(estimate.includedSessions)} sessions · ~${number(estimate.estimatedTokens)} tokens estimés sur ${number(estimate.tokenBudget)}. L’estimation utilise environ 4 caractères par token; le quota réel peut différer.`;
    $("#reportPreview").hidden = false;
  } catch (error) { notify(`Estimation impossible : ${error.message}`); }
  finally { button.disabled = false; }
}

async function runConversationReport() {
  const button = $("#runReport"); button.disabled = true;
  const result = $("#conversationResult"); result.hidden = false; result.textContent = "Rapport en cours…";
  try {
    const response = await api("/api/analysis/conversations", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ model: $("#analysisModel").value }) });
    result.textContent = `${response.includedSessions} sessions · ~${number(response.estimatedTokens)} tokens estimés\n\n${response.result || "Aucun résultat fourni."}`;
  } catch (error) { result.textContent = `Analyse impossible : ${error.message}`; }
  finally { button.disabled = false; }
}

async function loadSessions() {
  if (!state.settings?.showConversations) return;
  const query = $("#sessionSearch").value.trim();
  const root = $("#sessionList");
  root.innerHTML = '<div class="empty-state">Chargement des sessions…</div>';
  try {
    const sessions = await api(`/api/sessions?query=${encodeURIComponent(query)}`);
    state.sessions = sessions;
    root.replaceChildren();
    if (!sessions.length) { const empty = document.createElement("div"); empty.className = "empty-state"; empty.textContent = "Aucune conversation archivée ne correspond."; root.append(empty); return; }
    sessions.forEach(session => {
      const button = document.createElement("button"); button.className = `session-item${state.selectedSession === session.id ? " selected" : ""}`;
      const title = document.createElement("strong"); title.textContent = session.first_message || "Nouvelle conversation";
      const meta = document.createElement("small"); meta.textContent = `${date(session.start_time, true)} · ${session.model || "modèle inconnu"} · ${number(session.message_count)} messages`;
      const id = document.createElement("span"); id.textContent = session.id;
      button.append(title, meta, id); button.addEventListener("click", () => loadSessionDetail(session)); root.append(button);
    });
  } catch (error) { root.innerHTML = `<div class="empty-state">Sessions indisponibles : ${escapeText(error.message)}</div>`; }
}

async function loadSessionDetail(summary) {
  state.selectedSession = summary.id;
  const root = $("#sessionDetail"); root.innerHTML = '<div class="empty-state">Chargement de la conversation…</div>';
  try {
    const session = await api(`/api/sessions/${encodeURIComponent(summary.id)}`);
    root.replaceChildren();
    const head = document.createElement("div"); head.className = "detail-meta";
    const info = document.createElement("div");
    const title = document.createElement("strong"); title.textContent = summary.first_message || "Nouvelle conversation";
    const meta = document.createElement("p"); meta.textContent = `${date(summary.start_time, true)} · ${session.model || "modèle inconnu"} · ${number(summary.message_count)} messages`;
    info.append(title, meta);
    const copy = document.createElement("button"); copy.className = "button secondary"; copy.textContent = "Copier la commande";
    copy.addEventListener("click", async () => {
      try { await navigator.clipboard.writeText(summary.resume_command); notify(`Commande copiée : ${summary.resume_command}`); }
      catch { notify(`Copiez dans le terminal : ${summary.resume_command}`); }
    });
    head.append(info, copy);
    const transcript = document.createElement("div"); transcript.className = "transcript";
    (session.messages || session.Messages || []).forEach(message => {
      const article = document.createElement("div"); article.className = `message ${(message.role || message.Role) === "user" ? "user" : "assistant"}`;
      const role = document.createElement("span"); role.className = "message-role"; role.textContent = (message.role || message.Role) === "user" ? "Vous" : "Assistant";
      const content = document.createElement("span"); content.textContent = message.content || message.Content || "";
      article.append(role, content); transcript.append(article);
    });
    root.append(head, transcript);
    loadSessions();
  } catch (error) { root.textContent = `Conversation indisponible : ${error.message}`; }
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
    if (!commands.length) { root.textContent = "Aucune commande documentée."; return; }
    [...groups.keys()].sort().forEach(category => {
      const group = document.createElement("article"); group.className = "command-group";
      const heading = document.createElement("h3"); heading.textContent = category; group.append(heading);
      groups.get(category).sort((a, b) => String(field(a, "name", "Name")).localeCompare(String(field(b, "name", "Name")))).forEach(command => {
        const card = document.createElement("div"); card.className = "command-card";
        const usage = document.createElement("code"); usage.textContent = field(command, "usage", "Usage") || field(command, "name", "Name");
        const description = document.createElement("p"); description.textContent = field(command, "description", "Description") || "";
        card.append(usage, description);
        const examples = field(command, "examples", "Examples") || [];
        if (examples.length) { const example = document.createElement("small"); example.textContent = `Exemple : ${examples.join(" · ")}`; card.append(example); }
        group.append(card);
      });
      root.append(group);
    });
  } catch (error) { root.innerHTML = `<div class="empty-state">Documentation indisponible : ${escapeText(error.message)}</div>`; }
}

async function start() {
  document.querySelectorAll(".nav-item").forEach(button => button.addEventListener("click", () => showPage(button.dataset.page)));
  $("#metricsAnalysis").addEventListener("click", runMetricsAnalysis);
  $("#previewReport").addEventListener("click", previewConversationReport);
  $("#runReport").addEventListener("click", runConversationReport);
  let searchTimer;
  $("#sessionSearch").addEventListener("input", () => { clearTimeout(searchTimer); searchTimer = setTimeout(loadSessions, 180); });
  try {
    state.settings = await api("/api/settings");
    $("#sessionsNav").hidden = !state.settings.showConversations;
    $("#conversationAnalysis").hidden = !state.settings.allowConversationAnalysis;
    renderModels();
    await Promise.all([refreshStats(), loadCommands()]);
    state.timer = setInterval(refreshStats, Math.max(1, state.settings.refreshIntervalSeconds || 3) * 1000);
  } catch (error) { notify(`Configuration du dashboard indisponible : ${error.message}`); }
}

start();
