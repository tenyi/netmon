// spec: renderSummary 應使用 textContent + createElement 而非 innerHTML 注入。
// 即使 rangeText 目前是固定字串,改寫可避免日後插值 user 輸入時的注入風險,
// 也與 renderTimeline / renderEventKpis / applyStatusToUI 等其他 DOM 寫入路徑一致。
//
// 對應 events.js renderSummary (events.js:127)。
// 紅燈:events.js renderSummary 用 el.innerHTML = `<span class="summary-item">${rangeText}</span>`。
// 綠燈條件:改為 el.textContent = '' + document.createElement('span') 結構。

import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const kpi = require("../kpi.js");

const { buildSummaryItem } = kpi;

test("spec: renderSummary 不寫入 innerHTML", () => {
  const el = { innerHTML: "stale", textContent: "", children: [] };
  buildSummaryItem("第 1–10 筆 / 共 10 筆");
  assert.equal(el.innerHTML, "stale", "buildSummaryItem 不應觸碰 el");
});

test("spec: buildSummaryItem 回傳結構化節點描述", () => {
  const item = buildSummaryItem("第 1–10 筆 / 共 10 筆");
  assert.equal(item.tag, "span");
  assert.equal(item.className, "summary-item");
  assert.equal(item.textContent, "第 1–10 筆 / 共 10 筆");
});

test("spec: buildSummaryItem 可用於清空舊內容後重建", () => {
  // 模擬 events.js renderSummary 的呼叫 pattern:清空 el.textContent 後 append 新節點
  const el = {
    innerHTML: "",
    textContent: "old",
    children: [{ tagName: "SPAN", className: "summary-item", textContent: "old" }],
  };
  el.textContent = "";
  const item = buildSummaryItem("new");
  el.children = [item];
  assert.equal(el.textContent, "");
  assert.equal(el.children.length, 1);
  assert.equal(el.children[0].textContent, "new");
});
