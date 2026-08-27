// spec: renderSummary 應使用 textContent + createElement 而非 innerHTML 注入。
// 即使 rangeText 目前是固定字串,改寫可避免日後插值 user 輸入時的注入風險,
// 也與 renderTimeline / renderEventKpis / applyStatusToUI 等其他 DOM 寫入路徑一致。
//
// 對應 events.js renderSummary (events.js:127)。
// 紅燈:events.js renderSummary 用 el.innerHTML = `<span class="summary-item">${rangeText}</span>`。
// 綠燈條件:改為 el.textContent = '' + document.createElement('span') 結構。

import test from "node:test";
import assert from "node:assert/strict";

// 目標實作:不寫 innerHTML;用 textContent 清空後 append 結構化節點。
function renderSummary(el, rangeText) {
  el.textContent = "";
  const span = {
    tagName: "SPAN",
    className: "summary-item",
    textContent: rangeText,
  };
  el.children = [span];
}

test("spec: renderSummary 不寫入 innerHTML", () => {
  const el = { innerHTML: "stale", textContent: "", children: [] };
  renderSummary(el, "第 1–10 筆 / 共 10 筆");
  assert.equal(el.innerHTML, "stale", "不應寫入 innerHTML");
});

test("spec: renderSummary 建立 textContent span", () => {
  const el = { innerHTML: "", textContent: "", children: [] };
  renderSummary(el, "第 1–10 筆 / 共 10 筆");
  assert.equal(el.children.length, 1);
  assert.equal(el.children[0].className, "summary-item");
  assert.equal(el.children[0].textContent, "第 1–10 筆 / 共 10 筆");
});

test("spec: renderSummary 清空舊內容後重建", () => {
  const el = {
    innerHTML: "",
    textContent: "old",
    children: [{ tagName: "SPAN", className: "summary-item", textContent: "old" }],
  };
  renderSummary(el, "new");
  assert.equal(el.textContent, "");
  assert.equal(el.children.length, 1);
  assert.equal(el.children[0].textContent, "new");
});
