const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { JSDOM } = require("jsdom");

function setup() {
  const dom = new JSDOM(fs.readFileSync(path.join(__dirname, "assets/index.html"), "utf8"), { runScripts: "outside-only", url: "http://localhost/ui/" });
  const { window } = dom;
  window.structuredClone = structuredClone;
  const requests = [];
  window.mockAPI = async (url, options = {}) => {
    requests.push({ url, method: options.method || "GET", body: options.body ? JSON.parse(options.body) : null });
    if (url.includes("graph/search")) return [{ id: "outside-shortlist", kind: "term", version: 4, value: { name: "Outside", description: "Canonical outside shortlist", tags: ["technology"] } }];
    return [];
  };
  const app = fs.readFileSync(path.join(__dirname, "assets/app.js"), "utf8").replace('document.addEventListener("DOMContentLoaded", init);', "");
  const review = fs.readFileSync(path.join(__dirname, "assets/review.js"), "utf8");
  window.eval(review + "\n" + app + `\napi=window.mockAPI; refreshReview=async()=>{}; refreshStatus=async()=>{}; refreshGraph=async()=>{}; toast=()=>{}; window.testUI={renderReview,renderTaxonomyResolution,reviewDrafts,setTaxonomy:v=>reviewTaxonomy=v};`);
  return { dom, window, document: window.document, requests, ui: window.testUI };
}
function fixture() {
  const value = { name: "Incoming", description: "Incoming addition", tags: ["technology"] };
  const variant = { candidate_id: "candidate-1", value, evidence_chunk_ids: ["chunk-1"], anchor_chunk_id: "chunk-1" };
  return { id: "conspect-1", topic: "Topic", created_at: "2026-09-01T00:00:00Z", status: "review_pending", terms: [{ ref: "term-1", name: "Incoming" }], taxonomy: [], items: [{
    id: "item-1", conspect_id: "conspect-1", kind: "term", incoming_variants: [variant], final_value: value, status: "pending", resolution: "", target_version: 0,
    canonical_matches: [{ id: "canonical-1", kind: "term", version: 2, score: 0.8, exact: false, value: { name: "Current", description: "Existing details", tags: ["natural-sciences"] } }], pending_matches: [],
  }] };
}
const tick = () => new Promise((resolve) => setImmediate(resolve));

test("review uses typed fields; selecting update starts with canonical content and version", async () => {
  const h = setup(); try {
    const c = fixture(); c.items[0].incoming_variants[0].value.description = '<img src=x onerror="alert(1)">'; h.ui.renderReview([c]);
    assert.equal(h.document.querySelector(".review-item img"), null);
    assert.deepEqual([...h.document.querySelectorAll(".review-final input,.review-final textarea")].map((e) => e.name), ["name", "description", "tags"]);
    const target = h.document.querySelector('[aria-label="Выбранная canonical-сущность"]'); target.value = "canonical-1"; target.dispatchEvent(new h.window.Event("change"));
    const body = h.document.querySelector('[name="description"]'); assert.equal(body.value, "Existing details");
    body.value += " plus user addition"; body.dispatchEvent(new h.window.Event("input"));
    h.document.querySelector(".review-final").dispatchEvent(new h.window.Event("submit", { cancelable: true })); await tick();
    assert.equal(h.requests[0].method, "PUT"); assert.equal(h.requests[0].body.target_version, 2);
    assert.equal(h.requests[0].body.final_value.description, "Existing details plus user addition");
    assert.equal(h.ui.reviewDrafts.has("item-1"), false);
    assert.equal(h.requests.some((r) => r.url.endsWith("/apply")), false);
  } finally { h.dom.window.close(); }
});

test("apply is gated by saved decisions and preserves drafts across rerenders", async () => {
  const h = setup(); try {
    const c = fixture(); c.status = "ready_to_apply"; c.items[0].status = "resolved"; c.items[0].resolution = "create";
    h.ui.renderReview([c]); assert.equal(h.document.querySelector('[data-conspect-id]').disabled, false);
    assert.equal(h.document.querySelector(".saved-review-items").open, false);
    assert.match(h.document.querySelector(".review-footer p").textContent, /готов/);
    const name = h.document.querySelector('[name="name"]'); name.value = "My edit"; name.dispatchEvent(new h.window.Event("input"));
    assert.equal(h.document.querySelector('[data-conspect-id]').disabled, true);
    h.ui.renderReview([c]); assert.equal(h.document.querySelector('[name="name"]').value, "My edit");
    h.ui.reviewDrafts.clear(); h.ui.renderReview([c]); h.document.querySelector('[data-conspect-id]').click(); await tick();
    assert.equal(h.requests[0].url, "/api/v1/review/conspects/conspect-1/apply"); assert.equal(h.requests[0].method, "POST");
  } finally { h.dom.window.close(); }
});

test("graph search can select a target outside the shortlist", async () => {
  const h = setup(); try {
    h.ui.renderReview([fixture()]); h.document.querySelector(".review-search input").value = "Outside"; h.document.querySelector(".review-search button").click(); await tick();
    assert.match(h.requests[0].url, /graph\/search\?kind=term&q=Outside/);
    const target = h.document.querySelector('[aria-label="Выбранная canonical-сущность"]'); target.value = "outside-shortlist"; target.dispatchEvent(new h.window.Event("change"));
    assert.equal(h.document.querySelector('[name="description"]').value, "Canonical outside shortlist");
  } finally { h.dom.window.close(); }
});

test("variants can be split, and related terms are read-only chips", async () => {
  const h = setup(); try {
    const c = fixture(); c.items[0].incoming_variants.push({ ...c.items[0].incoming_variants[0], candidate_id: "candidate-2" });
    h.ui.renderReview([c]); [...h.document.querySelectorAll("button")].find((b) => b.textContent === "Выделить в отдельную карточку").click(); await tick();
    assert.equal(h.requests[0].url, "/api/v1/review/items/item-1/split"); assert.deepEqual(h.requests[0].body.variant_ids, ["candidate-1"]);
    c.items[0].kind = "thought"; c.items[0].incoming_variants = [{ candidate_id: "thought", value: { thesis: "Claim", body: "Body" }, related_term_refs: ["term-1"], evidence_chunk_ids: ["chunk"], anchor_chunk_id: "chunk" }];
    c.items[0].final_value = { thesis: "Claim", body: "Body" }; c.items[0].canonical_matches = []; h.ui.renderReview([c]);
    assert.equal(h.document.querySelectorAll(".review-item").length, 1); assert.match(h.document.querySelector(".review-incoming").textContent, /Incoming/);
    assert.equal(h.document.querySelector('[name="related_term_refs"]'), null);
  } finally { h.dom.window.close(); }
});

test("taxonomy map requires a target; create/remove do not send target IDs", async () => {
  const h = setup(); try {
    h.ui.setTaxonomy({ tag: [{ id: "science", name: "natural-sciences", enabled: true }] });
    const row = h.ui.renderTaxonomyResolution({ id: "tax", kind: "tag", value: "Unknown", resolution: "" }, true); h.document.body.append(row);
    const [resolution, target] = row.querySelectorAll("select"); resolution.value = "map"; resolution.dispatchEvent(new h.window.Event("change")); assert.equal(row.querySelector("button").disabled, true);
    target.value = "science"; target.dispatchEvent(new h.window.Event("change")); row.dispatchEvent(new h.window.Event("submit", { cancelable: true })); await tick(); assert.equal(h.requests[0].body.target_id, "science");
    resolution.value = "remove"; resolution.dispatchEvent(new h.window.Event("change")); row.dispatchEvent(new h.window.Event("submit", { cancelable: true })); await tick(); assert.deepEqual(h.requests[1].body, { resolution: "remove" });
  } finally { h.dom.window.close(); }
});
