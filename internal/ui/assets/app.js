"use strict";

const state = {
  token: "",
  currentView: "overview",
  status: null,
  graph: null,
  events: [],
  bootstrap: {},
  streamGeneration: 0,
  streamController: null,
  lastEventID: "",
};

const viewTitles = {
  overview: "Обзор",
  sources: "Источники",
  review: "Review",
  knowledge: "Граф знаний",
  jobs: "Очередь",
  events: "События",
  settings: "Настройки",
};

const refreshers = {
  overview: refreshStatus,
  sources: refreshSources,
  review: refreshReview,
  knowledge: refreshGraph,
  jobs: refreshJobs,
  events: refreshEvents,
  settings: refreshSettings,
};

document.addEventListener("DOMContentLoaded", init);

async function init() {
  bindNavigation();
  bindActions();
  try {
    const response = await fetch("/ui/bootstrap.json", { cache: "no-store" });
    state.bootstrap = await response.json();
    document.querySelector("#uiVersion").textContent = `memplua ${state.bootstrap.version || ""}`;
    document.querySelector("#tokenHint").textContent = state.bootstrap.token_file
      ? `Файл token: ${state.bootstrap.token_file}`
      : "Путь к token отображается в терминале при запуске.";
  } catch (error) {
    document.querySelector("#tokenHint").textContent = "Не удалось получить сведения о token-файле.";
  }

  const savedToken = sessionStorage.getItem("memplua.apiToken");
  if (savedToken) {
    document.querySelector("#tokenInput").value = savedToken;
    await connect(savedToken);
  } else {
    showAuth();
  }

  window.setInterval(() => {
    if (state.token) refreshStatus(true);
  }, 5000);
}

function bindNavigation() {
  document.querySelectorAll("[data-view]").forEach((button) => {
    button.addEventListener("click", () => switchView(button.dataset.view));
  });
  document.querySelectorAll("[data-go]").forEach((button) => {
    button.addEventListener("click", () => switchView(button.dataset.go));
  });
  document.querySelector("#refreshCurrent").addEventListener("click", () => refreshCurrent());
}

function bindActions() {
  document.querySelector("#authForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const button = event.currentTarget.querySelector("button[type=submit]");
    setBusy(button, true, "Подключение…");
    document.querySelector("#authError").textContent = "";
    await connect(document.querySelector("#tokenInput").value);
    setBusy(button, false);
  });
  document.querySelector("#changeToken").addEventListener("click", () => {
    disconnect();
    document.querySelector("#tokenInput").value = "";
    showAuth();
  });
  document.querySelector("#manualText").addEventListener("input", (event) => {
    document.querySelector("#manualCount").textContent = `${event.target.value.length.toLocaleString("ru-RU")} символов`;
  });
  document.querySelector("#manualTextForm").addEventListener("submit", submitManualText);
  document.querySelector("#refreshSources").addEventListener("click", refreshSources);
  document.querySelector("#refreshReview").addEventListener("click", refreshReview);
  document.querySelector("#refreshGraph").addEventListener("click", refreshGraph);
  document.querySelector("#refreshEvents").addEventListener("click", refreshEvents);
  document.querySelector("#jobStatus").addEventListener("change", refreshJobs);
  document.querySelector("#developerBatches").addEventListener("toggle", (event) => { if (event.target.open) refreshBatches(); });
  document.querySelector("#settingsForm").addEventListener("submit", saveSettings);
  document.querySelector("#exportNow").addEventListener("click", exportObsidian);
  document.querySelector("#shutdownApp").addEventListener("click", shutdownApplication);
}

function switchView(view) {
  if (!viewTitles[view]) return;
  state.currentView = view;
  document.querySelectorAll(".nav-item").forEach((item) => item.classList.toggle("is-active", item.dataset.view === view));
  document.querySelectorAll(".view").forEach((panel) => panel.classList.toggle("is-active", panel.id === `view-${view}`));
  document.querySelector("#viewTitle").textContent = viewTitles[view];
  refreshCurrent();
}

async function refreshCurrent() {
  if (!state.token) return;
  const refresh = refreshers[state.currentView] || refreshStatus;
  await refresh();
}

async function connect(token) {
  token = token.trim();
  if (!token) {
    document.querySelector("#authError").textContent = "Введите API token.";
    showAuth();
    return;
  }
  state.token = token;
  try {
    const status = await api("/api/v1/status");
    sessionStorage.setItem("memplua.apiToken", token);
    setConnected(true);
    applyStatus(status);
    const dialog = document.querySelector("#authDialog");
    if (dialog.open) dialog.close();
    await Promise.allSettled([refreshSources(), refreshReview(), refreshEvents()]);
    startEventStream();
  } catch (error) {
    state.token = "";
    sessionStorage.removeItem("memplua.apiToken");
    setConnected(false);
    document.querySelector("#authError").textContent = error.message || "Не удалось подключиться.";
    showAuth();
  }
}

function disconnect() {
  state.token = "";
  sessionStorage.removeItem("memplua.apiToken");
  state.streamGeneration += 1;
  if (state.streamController) state.streamController.abort();
  state.streamController = null;
  setConnected(false);
}

function showAuth() {
  const dialog = document.querySelector("#authDialog");
  if (!dialog.open) dialog.showModal();
  window.setTimeout(() => document.querySelector("#tokenInput").focus(), 50);
}

function setConnected(connected) {
  document.querySelector("#connectionDot").className = `status-dot ${connected ? "online" : "offline"}`;
  document.querySelector("#connectionText").textContent = connected ? "Подключено" : "Не подключено";
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${state.token}`);
  if (options.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  const response = await fetch(path, { ...options, headers });
  if (response.status === 401) {
    disconnect();
    showAuth();
    throw new Error("API token отклонён приложением.");
  }
  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`;
    try {
      const payload = await response.json();
      if (payload.error) message = payload.error;
    } catch (_) {
      // Keep the HTTP status when a response has no JSON body.
    }
    const error = new Error(message); error.status = response.status; throw error;
  }
  if (response.status === 204) return null;
  return response.json();
}

async function refreshStatus(quiet = false) {
  try {
    applyStatus(await api("/api/v1/status"));
  } catch (error) {
    if (!quiet) toast(error.message, "error");
  }
}

function applyStatus(status) {
  state.status = status;
  setConnected(true);
  const appState = document.querySelector("#appState");
  appState.textContent = status.state || "unknown";
  appState.className = `state-pill ${safeClass(status.state)}`;
  const queues = status.queues || {};
  setText("#metricQueued", queues.queued ?? 0);
  setText("#metricRunning", queues.running ?? 0);
  setText("#metricRetry", queues.retry ?? 0);
  setText("#metricFailed", queues.failed ?? 0);
  setText("#metricReview", status.pending_review_conspects ?? 0);
  setBadge("#reviewBadge", status.pending_review_conspects || 0);
  setText("#runID", status.run_id ? `run ${shortID(status.run_id)}` : "");
  renderComponents(status.components || {});
}

function renderComponents(components) {
  const container = document.querySelector("#componentList");
  container.classList.remove("empty-state");
  container.replaceChildren();
  const entries = Object.entries(components);
  if (!entries.length) {
    empty(container, "Нет компонентов с health-индикатором");
    return;
  }
  entries.sort(([left], [right]) => left.localeCompare(right)).forEach(([name, health]) => {
    const row = el("div", "component-row");
    const description = el("div");
    description.append(el("b", "", name), el("small", "", componentSummary(health)));
    const componentState = health && typeof health === "object" ? health.state : "ready";
    row.append(description, statusBadge(componentState || "ready"));
    container.append(row);
  });
}

function componentSummary(value) {
  if (value === null || value === undefined) return "running";
  if (typeof value !== "object") return String(value);
  const useful = Object.entries(value).filter(([key]) => key !== "state").slice(0, 2);
  return useful.length ? useful.map(([key, item]) => `${key}: ${String(item)}`).join(" · ") : "component health";
}

async function refreshSources() {
  try {
    const payload = await api("/api/v1/sources");
    renderSources(payload.sources || []);
    renderSessions(payload.sessions || []);
  } catch (error) {
    toast(error.message, "error");
  }
}

function renderSources(sources) {
  const container = document.querySelector("#sourceList");
  container.classList.remove("empty-state");
  container.replaceChildren();
  if (!sources.length) {
    empty(container, "Источники не зарегистрированы");
    return;
  }
  sources.forEach((source) => {
    const row = el("div", "source-row");
    const info = el("div");
    info.append(el("b", "", source.id), el("small", "", source.available ? source.kind : source.reason || "Недоступен"));
    const button = el("button", source.available ? "primary" : "secondary", source.available ? "Запустить" : "Недоступен");
    button.disabled = !source.available;
    button.addEventListener("click", () => sourceAction(button, `/api/v1/sources/${encodeURIComponent(source.id)}/start`, "Источник запускается"));
    row.append(info, button);
    container.append(row);
  });
}

function renderSessions(sessions) {
  const body = document.querySelector("#sessionRows");
  body.replaceChildren();
  if (!sessions.length) {
    body.append(emptyTableRow(5, "Сессий пока нет"));
    return;
  }
  sessions.slice(0, 100).forEach((session) => {
    const row = document.createElement("tr");
    row.append(
      cell(session.source_id),
      cell(shortID(session.id), "mono"),
      cellNode(statusBadge(session.state)),
      cell(formatDate(session.started_at)),
      sessionActions(session),
    );
    if (session.last_error) row.title = session.last_error;
    body.append(row);
  });
}

function sessionActions(session) {
  const td = el("td", "row-actions");
  if (session.state === "running" || session.state === "starting") {
    td.append(
      actionButton("Finalize", () => sourceAction(null, `/api/v1/source-sessions/${encodeURIComponent(session.id)}/finalize`, "Текущий блок отправлен на анализ")),
      actionButton("Пауза", () => sourceAction(null, `/api/v1/source-sessions/${encodeURIComponent(session.id)}/pause`, "Источник приостановлен")),
      actionButton("Стоп", () => sourceAction(null, `/api/v1/source-sessions/${encodeURIComponent(session.id)}/stop`, "Источник остановлен")),
    );
  } else if (session.state === "paused") {
    td.append(actionButton("Продолжить", () => sourceAction(null, `/api/v1/source-sessions/${encodeURIComponent(session.id)}/resume`, "Источник возобновляется")));
  }
  return td;
}

async function sourceAction(button, path, message) {
  if (button) setBusy(button, true, "Запуск…");
  try {
    await api(path, { method: "POST" });
    toast(message, "success");
    await Promise.all([refreshSources(), refreshStatus(true)]);
  } catch (error) {
    toast(error.message, "error");
  } finally {
    if (button) setBusy(button, false);
  }
}

async function submitManualText(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector("button[type=submit]");
  const textarea = document.querySelector("#manualText");
  if (!textarea.value.trim()) return;
  setBusy(button, true, "Отправка…");
  try {
    const submission = await api("/api/v1/ingest/text", { method: "POST", body: JSON.stringify({ text: textarea.value }) });
    textarea.value = "";
    textarea.dispatchEvent(new Event("input"));
    toast(`Chunk ${shortID(submission.chunk_id)} поставлен в очередь`, "success");
    await Promise.all([refreshSources(), refreshJobs(), refreshStatus(true)]);
  } catch (error) {
    toast(error.message, "error");
  } finally {
    setBusy(button, false);
  }
}

async function refreshGraph() {
  try {
    state.graph = await api("/api/v1/graph");
    renderGraph(state.graph);
  } catch (error) {
    toast(error.message, "error");
  }
}

function renderGraph(graph) {
  const terms = graph.terms || [];
  const thoughts = graph.thoughts || [];
  const links = graph.links || [];
  setText("#termCount", terms.length);
  setText("#thoughtCount", thoughts.length);
  setText("#graphSummary", `Ревизия ${graph.revision || 0} · ${terms.length} терминов · ${thoughts.length} мыслей · ${links.length} связей`);
  renderEntityList("#termList", terms, (term) => [term.name, term.description]);
  renderEntityList("#thoughtList", thoughts, (thought) => [thought.thesis, thought.body]);
  drawGraph(terms, thoughts, links);
}

function drawGraph(allTerms, allThoughts, allLinks) {
  const svg = document.querySelector("#knowledgeGraph");
  svg.replaceChildren();
  const terms = allTerms.slice(0, 60);
  const thoughts = allThoughts.slice(0, 60);
  const termIDs = new Set(terms.map((item) => item.id));
  const thoughtIDs = new Set(thoughts.map((item) => item.id));
  const links = allLinks.filter((link) => termIDs.has(link.term_id) && thoughtIDs.has(link.thought_id));
  const clipped = terms.length < allTerms.length || thoughts.length < allThoughts.length;
  setText("#graphLimitNotice", clipped ? "Показаны первые 60 узлов каждого типа" : "");
  if (!terms.length && !thoughts.length) {
    svg.setAttribute("viewBox", "0 0 900 430");
    const text = svgElement("text", { x: 450, y: 215, "text-anchor": "middle", fill: "#616a61" });
    text.textContent = "Граф пока пуст — добавьте текст и одобрите предложения";
    svg.append(text);
    return;
  }

  const width = 900;
  const height = Math.max(430, Math.max(terms.length, thoughts.length) * 58 + 70);
  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  svg.style.height = `${Math.min(height, 1500)}px`;
  const positions = new Map();
  placeColumn(terms, 190, height, positions);
  placeColumn(thoughts, 700, height, positions);
  links.forEach((link) => {
    const from = positions.get(link.term_id);
    const to = positions.get(link.thought_id);
    if (!from || !to) return;
    svg.append(svgElement("line", { x1: from.x + 70, y1: from.y, x2: to.x - 110, y2: to.y, class: "graph-link" }));
  });
  terms.forEach((term) => svg.append(graphNode(term, "term", positions.get(term.id))));
  thoughts.forEach((thought) => svg.append(graphNode(thought, "thought", positions.get(thought.id))));
}

function placeColumn(items, x, height, positions) {
  const step = height / (items.length + 1);
  items.forEach((item, index) => positions.set(item.id, { x, y: step * (index + 1) }));
}

function graphNode(entity, kind, position) {
  const group = svgElement("g", { class: "graph-node", transform: `translate(${position.x} ${position.y})`, tabindex: "0", role: "button" });
  const isTerm = kind === "term";
  const shape = isTerm
    ? svgElement("rect", { x: -72, y: -20, width: 144, height: 40, rx: 20, class: "term-shape" })
    : svgElement("rect", { x: -112, y: -23, width: 224, height: 46, rx: 10, class: "thought-shape" });
  const label = svgElement("text", { x: 0, y: 4, "text-anchor": "middle" });
  label.textContent = truncate(isTerm ? entity.name : entity.thesis, isTerm ? 21 : 31);
  const select = () => {
    document.querySelectorAll(".graph-node").forEach((node) => node.classList.remove("is-selected"));
    group.classList.add("is-selected");
    showNodeDetails(entity, kind);
  };
  group.addEventListener("click", select);
  group.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === " ") select();
  });
  group.append(shape, label);
  return group;
}

function showNodeDetails(entity, kind) {
  const details = document.querySelector("#nodeDetails");
  details.replaceChildren();
  details.append(el("p", "eyebrow", kind === "term" ? "ТЕРМИН" : "МЫСЛЬ"));
  details.append(el("h3", "", kind === "term" ? entity.name : entity.thesis));
  details.append(el("p", "", kind === "term" ? entity.description : entity.body));
  const values = entity.tags || [];
  if (values.length) {
    const row = el("div", "tag-row");
    values.forEach((value) => row.append(el("span", "tag", value)));
    details.append(row);
  }
}

function renderEntityList(selector, values, fields) {
  const container = document.querySelector(selector);
  container.replaceChildren();
  if (!values.length) {
    empty(container, "Пока пусто");
    return;
  }
  values.slice(0, 100).forEach((value) => {
    const [title, description] = fields(value);
    const row = el("article", "entity-row");
    row.append(el("b", "", title || "Без названия"), el("p", "", description || "Нет описания"));
    container.append(row);
  });
}

async function refreshJobs() {
  try {
    const status = document.querySelector("#jobStatus").value;
    const query = new URLSearchParams({ limit: "200" });
    if (status) query.set("status", status);
    renderJobs((await api(`/api/v1/jobs?${query}`)) || []);
    if (document.querySelector("#developerBatches").open) await refreshBatches();
  } catch (error) {
    toast(error.message, "error");
  }
}

function renderJobs(jobs) {
  const body = document.querySelector("#jobRows");
  body.replaceChildren();
  if (!jobs.length) {
    body.append(emptyTableRow(6, "Заданий нет"));
    return;
  }
  jobs.forEach((job) => {
    const row = document.createElement("tr");
    const actions = el("td", "row-actions");
    if (job.status === "failed") {
      actions.append(actionButton("Retry", () => retryJob(job.id)));
    }
    row.append(
      cell(job.kind),
      cell(shortID(job.id), "mono"),
      cellNode(statusBadge(job.status)),
      cell(job.attempts ?? 0),
      cell(formatDate(job.available_at)),
      actions,
    );
    if (job.last_error) row.title = job.last_error;
    body.append(row);
  });
}

async function retryJob(jobID) {
  try {
    await api(`/api/v1/jobs/${encodeURIComponent(jobID)}/retry`, { method: "POST" });
    toast("Задание возвращено в очередь", "success");
    await Promise.all([refreshJobs(), refreshStatus(true)]);
  } catch (error) {
    toast(error.message, "error");
  }
}

async function refreshEvents() {
  try {
    const events = await api("/api/v1/notifications?limit=500");
    state.events = dedupeEvents(events || []);
    renderEvents();
  } catch (error) {
    toast(error.message, "error");
  }
}

function renderEvents() {
  const container = document.querySelector("#eventList");
  container.classList.remove("empty-state");
  container.replaceChildren();
  const sorted = [...state.events].sort((left, right) => new Date(right.time) - new Date(left.time));
  const unread = sorted.filter((event) => !event.read).length;
  setBadge("#eventBadge", unread);
  if (!sorted.length) {
    empty(container, "Событий пока нет");
    return;
  }
  sorted.forEach((event) => {
    const article = el("article", `event ${safeClass(event.severity)} ${event.read ? "" : "unread"}`);
    article.append(el("time", "", formatDate(event.time)));
    const message = el("div");
    message.append(el("h3", "", humanEventType(event.type)), el("p", "", event.message || event.type));
    article.append(message);
    if (!event.read) {
      const button = el("button", "", "Прочитано");
      button.addEventListener("click", () => markEventRead(event, button));
      article.append(button);
    } else {
      article.append(document.createElement("span"));
    }
    container.append(article);
  });
}

async function markEventRead(event, button) {
  button.disabled = true;
  try {
    await api(`/api/v1/notifications/${encodeURIComponent(event.id)}/read`, { method: "POST" });
    event.read = true;
    renderEvents();
  } catch (error) {
    button.disabled = false;
    toast(error.message, "error");
  }
}

async function startEventStream() {
  const generation = ++state.streamGeneration;
  if (state.streamController) state.streamController.abort();
  state.streamController = new AbortController();
  while (state.token && generation === state.streamGeneration) {
    try {
      const headers = { Authorization: `Bearer ${state.token}` };
      if (state.lastEventID) headers["Last-Event-ID"] = state.lastEventID;
      const response = await fetch("/api/v1/events", { headers, signal: state.streamController.signal });
      if (response.status === 401) {
        disconnect();
        showAuth();
        return;
      }
      if (!response.ok || !response.body) throw new Error(`SSE: ${response.status}`);
      await readEventStream(response.body, generation);
    } catch (error) {
      if (generation !== state.streamGeneration || !state.token) return;
      setConnected(false);
      await delay(1800);
    }
  }
}

async function readEventStream(body, generation) {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (generation === state.streamGeneration) {
    const { value, done } = await reader.read();
    if (done) return;
    buffer += decoder.decode(value, { stream: true }).replaceAll("\r\n", "\n");
    let boundary;
    while ((boundary = buffer.indexOf("\n\n")) >= 0) {
      const frame = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      consumeEventFrame(frame);
    }
  }
}

function consumeEventFrame(frame) {
  let id = "";
  const data = [];
  frame.split("\n").forEach((line) => {
    if (line.startsWith("id:")) id = line.slice(3).trim();
    if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  });
  if (!data.length) return;
  try {
    const event = JSON.parse(data.join("\n"));
    if (id) state.lastEventID = id;
    state.events = dedupeEvents([...state.events, event]);
    renderEvents();
    refreshStatus(true);
    if (event.type && event.type.startsWith("conspect.") && reviewDrafts.size === 0) refreshReview();
    if (event.type && event.type.startsWith("source.")) refreshSources();
  } catch (_) {
    // Ignore a malformed frame and reconnect from the last valid event id.
  }
}

function dedupeEvents(events) {
  const byID = new Map();
  events.forEach((event) => {
    if (event && event.id) byID.set(event.id, event);
  });
  return [...byID.values()];
}

async function refreshSettings() {
  try {
    const settings = await api("/api/v1/settings");
    const form = document.querySelector("#settingsForm");
    setFormValue(form, "language", settings.language);
    setFormValue(form, "log_level", settings.logging?.level);
    setFormValue(form, "processing_limit", settings.pipeline?.processing_limit);
    setFormValue(form, "review_limit", settings.pipeline?.review_limit);
    setFormValue(form, "models_managed", settings.models?.managed);
    setFormValue(form, "llama_binary", settings.models?.llama_binary);
    setFormValue(form, "llm_model", settings.models?.llm_model);
    setFormValue(form, "llm_url", settings.models?.llm_url);
    setFormValue(form, "model_parallel", settings.models?.parallel);
    setFormValue(form, "llama_server_args", JSON.stringify(settings.models?.server_args || [], null, 2));
    setFormValue(form, "whisper_model", settings.models?.whisper_model);
    setFormValue(form, "silero_model", settings.models?.silero_model);
    setFormValue(form, "onnx_runtime", settings.models?.onnx_runtime);
    setFormValue(form, "obsidian_directory", settings.export?.obsidian_directory);
    setFormValue(form, "auto_export", settings.export?.auto);
    setText("#settingsHint", `Data: ${settings.data_dir || "—"}`);
  } catch (error) {
    toast(error.message, "error");
  }
}

async function saveSettings(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector("button[type=submit]");
  const value = (name) => form.elements[name].value.trim();
  let serverArgs;
  try {
    serverArgs = JSON.parse(value("llama_server_args") || "[]");
    if (!Array.isArray(serverArgs) || serverArgs.some((argument) => typeof argument !== "string")) {
      throw new Error("ожидается JSON-массив строк");
    }
  } catch (error) {
    toast(`Аргументы llama-server: ${error.message}`, "error");
    return;
  }
  const patch = {
    language: value("language"),
    log_level: value("log_level"),
    processing_limit: Number(value("processing_limit")),
    review_limit: Number(value("review_limit")),
    models_managed: form.elements.models_managed.checked,
    llama_binary: value("llama_binary"),
    llm_model: value("llm_model"),
    llm_url: value("llm_url"),
    model_parallel: Number(value("model_parallel")),
    llama_server_args: serverArgs,
    whisper_model: value("whisper_model"),
    silero_model: value("silero_model"),
    onnx_runtime: value("onnx_runtime"),
    obsidian_directory: value("obsidian_directory"),
    auto_export: form.elements.auto_export.checked,
  };
  setBusy(button, true, "Сохранение…");
  try {
    const result = await api("/api/v1/settings", { method: "PATCH", body: JSON.stringify(patch) });
    toast(result.restart_required ? "Настройки сохранены. Перезапустите приложение." : "Настройки сохранены", "success");
  } catch (error) {
    toast(error.message, "error");
  } finally {
    setBusy(button, false);
  }
}

async function exportObsidian() {
  const button = document.querySelector("#exportNow");
  setBusy(button, true, "Постановка в очередь…");
  try {
    await api("/api/v1/exports/obsidian", { method: "POST" });
    toast("Экспорт поставлен в очередь", "success");
    await refreshJobs();
  } catch (error) {
    toast(error.message, "error");
  } finally {
    setBusy(button, false);
  }
}

async function shutdownApplication() {
  if (!(await confirmAction("Остановить memplua?", "Новые данные перестанут приниматься, текущие durable jobs освободят lease."))) return;
  try {
    await api("/api/v1/application/shutdown", { method: "POST" });
    toast("Приложение останавливается", "success");
    setConnected(false);
  } catch (error) {
    toast(error.message, "error");
  }
}

function confirmAction(title, message) {
  const dialog = document.querySelector("#confirmDialog");
  document.querySelector("#confirmTitle").textContent = title;
  document.querySelector("#confirmText").textContent = message;
  dialog.showModal();
  return new Promise((resolve) => {
    dialog.addEventListener("close", () => resolve(dialog.returnValue === "confirm"), { once: true });
  });
}

function setFormValue(form, name, value) {
  const input = form.elements[name];
  if (!input || value === undefined || value === null) return;
  if (input.type === "checkbox") input.checked = Boolean(value);
  else input.value = value;
}

function setBusy(button, busy, label = "Загрузка…") {
  if (!button) return;
  if (busy) {
    button.dataset.originalText = button.textContent;
    button.textContent = label;
    button.disabled = true;
  } else {
    button.textContent = button.dataset.originalText || button.textContent;
    button.disabled = false;
    delete button.dataset.originalText;
  }
}

function toast(message, kind = "") {
  const region = document.querySelector("#toastRegion");
  const item = el("div", `toast ${kind}`, message);
  region.append(item);
  window.setTimeout(() => item.remove(), 4200);
}

function statusBadge(value) {
  return el("span", `state-badge ${safeClass(value)}`, value || "unknown");
}

function setBadge(selector, count) {
  const badge = document.querySelector(selector);
  badge.textContent = count > 99 ? "99+" : String(count);
  badge.classList.toggle("is-hidden", !count);
}

function payload(value) {
  const pre = el("pre", "payload");
  pre.textContent = pretty(value);
  return pre;
}

function pretty(value) {
  if (value === undefined || value === null || value === "") return "—";
  if (typeof value === "string") {
    try { return JSON.stringify(JSON.parse(value), null, 2); } catch (_) { return value; }
  }
  return JSON.stringify(value, null, 2);
}

function humanObjectKind(kind) {
  return ({ term: "Термин", thought: "Мысль", thought_term: "Связь", taxonomy: "Таксономия" })[kind] || kind;
}

function humanAction(action) {
  return ({ create: "создание", update: "обновление", noop: "без изменений" })[action] || action;
}

function humanEventType(type) {
  const known = {
    "application.started": "Приложение запущено",
    "application.stopped": "Приложение остановлено",
    "application.failed": "Ошибка приложения",
    "ingest.text_submitted": "Текст принят",
    "analysis_batch.closed": "Блок отправлен на анализ",
    "analysis_batch.extracted": "Извлечение завершено",
    "conspect.review_ready": "Конспект готов к проверке",
    "conspect.apply_conflict": "Требуется повторное сопоставление",
    "conspect.applied": "Конспект применён",
    "job.failed": "Ошибка обработки",
    "job.retry_scheduled": "Запланирован retry",
    "export.completed": "Экспорт завершён",
    "export.failed": "Ошибка экспорта",
  };
  return known[type] || type?.replaceAll(".", " · ") || "Событие";
}

function safeClass(value) {
  return String(value || "neutral").toLowerCase().replace(/[^a-z0-9_-]/g, "-");
}

function formatDate(value) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return new Intl.DateTimeFormat("ru-RU", { dateStyle: "short", timeStyle: "medium" }).format(date);
}

function shortID(value) {
  if (!value) return "—";
  return value.length > 13 ? `${value.slice(0, 8)}…${value.slice(-4)}` : value;
}

function truncate(value, length) {
  value = String(value || "Без названия");
  return value.length > length ? `${value.slice(0, length - 1)}…` : value;
}

function setText(selector, value) {
  document.querySelector(selector).textContent = value;
}

function el(tag, className = "", text = undefined) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = String(text);
  return node;
}

function svgElement(tag, attributes) {
  const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
  Object.entries(attributes).forEach(([name, value]) => node.setAttribute(name, value));
  return node;
}

function cell(value, className = "") {
  return el("td", className, value);
}

function cellNode(node) {
  const td = document.createElement("td");
  td.append(node);
  return td;
}

function emptyTableRow(columns, message) {
  const row = document.createElement("tr");
  const td = cell(message, "subtle");
  td.colSpan = columns;
  row.append(td);
  return row;
}

function empty(container, message) {
  container.classList.add("empty-state");
  container.append(el("span", "", message));
}

function actionButton(label, handler) {
  const button = el("button", "", label);
  button.addEventListener("click", handler);
  return button;
}

function delay(milliseconds) {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds));
}
