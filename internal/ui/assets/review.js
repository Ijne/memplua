"use strict";

// Drafts survive status events and saves of other cards. They never change the graph.
const reviewDrafts = new Map();
let reviewTaxonomy = { tag: [] };

async function refreshReview() {
  try {
    const [conspects, tags] = await Promise.all([
      api("/api/v1/review/conspects?limit=100"), api("/api/v1/taxonomy/tag"),
    ]);
    reviewTaxonomy = { tag: tags || [] };
    renderReview(conspects || []);
    setBadge("#reviewBadge", (conspects || []).length);
  } catch (error) { toast(error.message, "error"); }
}

function renderReview(conspects) {
  const container = document.querySelector("#reviewList");
  container.classList.remove("empty-state");
  container.replaceChildren();
  if (!conspects.length) { empty(container, "Нет конспектов, ожидающих решения"); return; }
  for (const conspect of conspects) {
    const article = el("article", "review-set card");
    const header = el("header", "review-set-header");
    const title = el("div");
    title.append(el("h3", "", conspect.topic), el("small", "", formatDate(conspect.created_at)));
    header.append(title, statusBadge(conspect.status));
    article.append(header);
    const editable = ["review_pending", "ready_to_apply"].includes(conspect.status);
    if (!editable) article.append(el("p", "subtle", conspect.status === "failed" ? "Подготовка завершилась ошибкой. Повторите задание в разделе очереди." : "Подготовка сопоставлений…"));
    const items = conspect.items || [];
    const pendingItems = items.filter((item) => item.status !== "resolved" && item.status !== "applied");
    const savedItems = items.filter((item) => item.status === "resolved" || item.status === "applied");
    for (const item of pendingItems) article.append(renderReviewItem(item, conspect, editable));
    if (savedItems.length) {
      const saved = el("details", "saved-review-items");
      saved.append(el("summary", "", `Сохранённые решения: ${savedItems.length}`));
      for (const item of savedItems) saved.append(renderReviewItem(item, conspect, editable));
      article.append(saved);
    }
    const taxonomy = (conspect.taxonomy || []).filter((item) => item.required && !item.resolution);
    if (taxonomy.length) {
      const section = el("section", "taxonomy-review");
      section.append(el("h4", "", "Неизвестные теги"));
      for (const resolution of taxonomy) section.append(renderTaxonomyResolution(resolution, editable));
      article.append(section);
    }
    const footer = el("footer", "review-footer");
    const apply = el("button", "primary", "Apply Conspect");
    apply.dataset.conspectId = conspect.id;
    const unsaved = items.some((item) => reviewDrafts.has(item.id));
    apply.disabled = conspect.status !== "ready_to_apply" || unsaved;
    apply.title = "Сначала сохраните решения всех карточек и разрешите неизвестные теги";
    apply.addEventListener("click", () => reviewMutation(apply, `/api/v1/review/conspects/${encodeURIComponent(conspect.id)}/apply`, "POST", null, () => {
      for (const item of conspect.items || []) reviewDrafts.delete(item.id);
    }));
    footer.append(el("p", "subtle", reviewProgress(conspect, pendingItems.length, taxonomy.length, unsaved)), apply);
    article.append(footer, provenanceDetails(conspect));
    container.append(article);
  }
}

function reviewProgress(conspect, pendingItems, pendingTaxonomy, unsaved) {
  if (unsaved) return "Есть несохранённые изменения в открытых карточках.";
  const remaining = [];
  if (pendingItems) remaining.push(`карточек: ${pendingItems}`);
  if (pendingTaxonomy) remaining.push(`неизвестных тегов: ${pendingTaxonomy}`);
  if (remaining.length) return `Чтобы применить конспект, сохраните решения — ${remaining.join(", ")}.`;
  if (conspect.status === "ready_to_apply") return "Все решения сохранены. Конспект готов к применению.";
  return "Состояние конспекта обновляется…";
}

function valuePreview(kind, value) {
  const section = el("div", "value-preview");
  section.append(el("b", "", kind === "term" ? value.name : value.thesis));
  section.append(el("p", "", (kind === "term" ? value.description : value.body) || "—"));
  const chips = el("div", "review-chips");
  for (const label of value.tags || []) chips.append(el("span", "chip", label));
  section.append(chips);
  return section;
}

function renderReviewItem(item, conspect, editable) {
  const section = el("section", "review-item");
  section.dataset.itemId = item.id;
  const heading = el("div", "operation-heading");
  heading.append(el("h4", "", item.kind === "term" ? "Термин" : "Мысль"), statusBadge(item.status));
  section.append(heading);
  const columns = el("div", "review-columns");
  const incoming = el("div", "review-incoming");
  incoming.append(el("p", "eyebrow", "ВХОДЯЩИЕ ВАРИАНТЫ"));
  const draft = reviewDrafts.get(item.id) || {
    resolution: item.resolution || "create", target: null, value: structuredClone(item.final_value),
  };
  if (!draft.target && item.target_id) draft.target = (item.canonical_matches || []).find((m) => m.id === item.target_id) || {
    id: item.target_id, version: item.target_version, value: item.final_value,
  };
  const markDirty = () => {
    reviewDrafts.set(item.id, draft);
    const apply = document.querySelector(`[data-conspect-id="${conspect.id}"]`);
    if (apply) apply.disabled = true;
  };
  const fields = {};
  const assignValue = (value) => {
    draft.value = structuredClone(value);
    for (const [key, input] of Object.entries(fields)) input.value = Array.isArray(value[key]) ? value[key].join(", ") : (value[key] || "");
  };
  for (const variant of item.incoming_variants || []) {
    const card = valuePreview(item.kind, variant.value);
    card.append(el("small", "provenance-summary", `Основание: ${(variant.evidence_chunk_ids || []).map(shortID).join(", ")} · anchor ${shortID(variant.anchor_chunk_id)}`));
    const use = actionButton("Использовать вариант", () => {
      draft.resolution = "create"; draft.target = null; resolution.value = "create";
      assignValue(variant.value); updateMode(); markDirty();
    });
    use.disabled = !editable; card.append(use);
    if ((item.incoming_variants || []).length > 1) {
      const split = actionButton("Выделить в отдельную карточку", () => reviewMutation(split, `/api/v1/review/items/${encodeURIComponent(item.id)}/split`, "POST", { variant_ids: [variant.candidate_id] }, () => reviewDrafts.delete(item.id)));
      split.disabled = !editable; card.append(split);
    }
    incoming.append(card);
  }
  if (item.kind === "thought") {
    const chips = el("div", "review-chips");
    const refs = new Set((item.incoming_variants || []).flatMap((v) => v.related_term_refs || []));
    incoming.append(el("small", "subtle", "Связанные термины (применяются автоматически):"));
    for (const ref of refs) chips.append(el("span", "chip", (conspect.terms || []).find((t) => t.ref === ref)?.name || ref));
    incoming.append(chips);
  }
  const matches = el("div", "review-canonical");
  matches.append(el("p", "eyebrow", "СУЩЕСТВУЮЩИЕ СУЩНОСТИ"));
  const matchList = el("div", "match-list");
  const target = el("select"); target.setAttribute("aria-label", "Выбранная canonical-сущность");
  target.append(new Option("Выберите существующую сущность", ""));
  const pool = new Map();
  const addMatch = (match) => {
    if (pool.has(match.id)) return;
    pool.set(match.id, match);
    target.append(new Option(`${item.kind === "term" ? match.value.name : match.value.thesis} · v${match.version}`, match.id));
    const card = valuePreview(item.kind, match.value);
    if (match.score != null) card.append(el("small", "subtle", `${match.exact ? "Точное совпадение" : "Похожий вариант"} · ${Math.round(match.score * 100)}%`));
    const select = actionButton("Выбрать", () => { target.value = match.id; selectTarget(); });
    select.disabled = !editable; card.append(select); matchList.append(card);
  };
  for (const match of item.canonical_matches || []) addMatch(match);
  if (draft.target) { addMatch(draft.target); target.value = draft.target.id; }
  const search = el("div", "review-search");
  const query = el("input"); query.placeholder = "Поиск по всему графу"; query.setAttribute("aria-label", "Поиск по всему графу");
  const searchButton = actionButton("Найти", async () => {
    setBusy(searchButton, true);
    try {
      const result = await api(`/api/v1/graph/search?${new URLSearchParams({ kind: item.kind, q: query.value, limit: "100" })}`);
      for (const match of result || []) addMatch(match);
      searchHint.textContent = result.length ? `Найдено: ${result.length}` : "Совпадений нет";
    } catch (error) { toast(error.message, "error"); } finally { setBusy(searchButton, false); }
  });
  const searchHint = el("small", "subtle");
  search.append(query, searchButton); matches.append(search, searchHint, target, matchList);
  if ((item.pending_matches || []).length) {
    matches.append(el("p", "eyebrow", "В ДРУГИХ КОНСПЕКТАХ НА ПРОВЕРКЕ"));
    for (const match of item.pending_matches) matches.append(valuePreview(item.kind, match.value));
    matches.append(el("small", "subtle", "Эти предложения ещё не входят в граф. Примените соответствующий конспект, чтобы выбрать сущность."));
  }
  const form = el("form", "review-final");
  form.append(el("p", "eyebrow", "ИТОГОВЫЙ РЕЗУЛЬТАТ"));
  const resolution = el("select"); resolution.setAttribute("aria-label", "Решение");
  for (const [value, text] of [["create", "Создать"], ["update", "Обновить выбранную"], ["keep_existing", "Оставить существующую"], ["reject", "Отклонить"]]) resolution.append(new Option(text, value));
  resolution.value = draft.resolution; form.append(resolution);
  const definitions = item.kind === "term" ? [["name", "Название", false], ["description", "Описание", true]] : [["thesis", "Тезис", false], ["body", "Текст мысли", true]];
  definitions.push(["tags", "Метки (через запятую)", false]);
  for (const [key, title, multiline] of definitions) {
    const label = el("label", "", title);
    const input = el(multiline ? "textarea" : "input"); input.name = key;
    if (multiline) input.rows = 5;
    if (key === "name" || key === "thesis" || (key === "tags" && item.kind === "term")) input.required = true;
    input.addEventListener("input", () => { draft.value[key] = key === "tags" ? input.value.split(",").map((s) => s.trim()).filter(Boolean) : input.value; markDirty(); });
    fields[key] = input; label.append(input); form.append(label);
  }
  assignValue(draft.value);
  const modeHint = el("p", "subtle"); form.append(modeHint);
  const save = el("button", "primary", "Сохранить решение"); save.type = "submit"; form.append(save);
  const updateMode = () => {
    const needsTarget = ["update", "keep_existing"].includes(draft.resolution);
    target.disabled = !editable;
    for (const input of Object.values(fields)) input.disabled = !editable || ["keep_existing", "reject"].includes(draft.resolution);
    resolution.disabled = !editable;
    save.disabled = !editable || (needsTarget && !draft.target);
    modeHint.textContent = draft.resolution === "update" ? "Форма начинается с текущей версии сущности. Перенесите нужные дополнения вручную." : "";
  };
  const selectTarget = () => {
    draft.target = pool.get(target.value) || null;
    if (draft.target) {
      if (!["update", "keep_existing"].includes(draft.resolution)) { draft.resolution = "update"; resolution.value = "update"; }
      assignValue(draft.target.value);
    }
    updateMode(); markDirty();
  };
  target.addEventListener("change", selectTarget);
  resolution.addEventListener("change", () => {
    draft.resolution = resolution.value;
    if (["update", "keep_existing"].includes(draft.resolution) && draft.target) assignValue(draft.target.value);
    if (draft.resolution === "create") assignValue(item.incoming_variants[0].value);
    updateMode(); markDirty();
  });
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const body = { resolution: draft.resolution };
    if (["update", "keep_existing"].includes(draft.resolution)) { body.target_id = draft.target.id; body.target_version = draft.target.version; }
    if (["create", "update"].includes(draft.resolution)) body.final_value = draft.value;
    reviewMutation(save, `/api/v1/review/items/${encodeURIComponent(item.id)}`, "PUT", body, () => reviewDrafts.delete(item.id));
  });
  updateMode(); columns.append(incoming, matches, form); section.append(columns); return section;
}

function renderTaxonomyResolution(item, editable) {
  const row = el("form", "taxonomy-row");
  row.append(el("b", "", `Метка: ${item.value}`));
  const resolution = el("select"); resolution.setAttribute("aria-label", `Решение для ${item.value}`);
  for (const [value, text] of [["", "Выберите решение"], ["map", "Заменить существующей"], ["create", "Сохранить как новую"], ["remove", "Удалить из конспекта"]]) resolution.append(new Option(text, value));
  resolution.value = item.resolution;
  const target = el("select"); target.setAttribute("aria-label", `Замена для ${item.value}`); target.append(new Option("Выберите метку", ""));
  for (const entry of reviewTaxonomy[item.kind] || []) if (entry.enabled) target.append(new Option(entry.name, entry.id));
  target.value = item.target_id || "";
  const save = el("button", "secondary", item.resolution ? "Сохранено · изменить" : "Сохранить"); save.type = "submit";
  const update = () => { target.disabled = !editable || resolution.value !== "map"; save.disabled = !editable || !resolution.value || (resolution.value === "map" && !target.value); };
  resolution.disabled = !editable; resolution.addEventListener("change", update); target.addEventListener("change", update); update();
  row.append(resolution, target, save);
  row.addEventListener("submit", (event) => { event.preventDefault(); const body = { resolution: resolution.value }; if (body.resolution === "map") body.target_id = target.value; reviewMutation(save, `/api/v1/review/taxonomy/${encodeURIComponent(item.id)}`, "PUT", body); });
  return row;
}

function provenanceDetails(conspect) {
  const details = el("details", "provenance-details"); details.append(el("summary", "", "Исходные фрагменты"));
  details.addEventListener("toggle", async () => {
    if (!details.open || details.dataset.loaded) return;
    try {
      const chunks = await api(`/api/v1/review/conspects/${encodeURIComponent(conspect.id)}/chunks`);
      for (const chunk of chunks || []) { const article = el("article", "source-evidence"); article.append(el("small", "mono", chunk.id), el("p", "", chunk.text)); details.append(article); }
      details.dataset.loaded = "true";
    } catch (error) { toast(error.message, "error"); }
  });
  return details;
}

async function reviewMutation(button, path, method, body, saved = () => {}) {
  setBusy(button, true, "Сохранение…");
  try {
    await api(path, { method, ...(body ? { body: JSON.stringify(body) } : {}) });
    saved(); toast(path.endsWith("/apply") ? "Конспект применён" : "Решение сохранено", "success");
    await Promise.all([refreshReview(), refreshStatus(true), refreshGraph()]);
  } catch (error) {
    toast(error.message, "error");
    if (error.status === 409) { reviewDrafts.clear(); await refreshReview(); }
  } finally { setBusy(button, false); }
}

async function refreshBatches() {
  const container = document.querySelector("#batchList");
  try {
    const batches = await api("/api/v1/developer/analysis-batches?limit=50"); container.replaceChildren();
    for (const batch of batches || []) {
      const details = el("details", "developer-batch"); details.append(el("summary", "", `${shortID(batch.id)} · ${batch.status} · ${formatDate(batch.started_at)}`));
      details.append(payload(batch)); container.append(details);
    }
  } catch (error) { toast(error.message, "error"); }
}
