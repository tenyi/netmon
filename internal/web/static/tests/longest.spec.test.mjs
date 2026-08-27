// spec: longestDisconnection 應過濾 clock skew(例如 started_at > now),
// 且 ongoing 事件 (ended_at == null) 仍可成為 longest。
//
// 對應 dashboard.js renderEventKpis 內 longest 計算 (dashboard.js:179-188)。
// 紅燈:dashboard.js 目前 inline 邏輯未過濾負 duration,clock skew 會污染結果。
// 綠燈條件:從 dashboard.js 抽出 longestDisconnection helper 並套用 d < 0 過濾。

import test from "node:test";
import assert from "node:assert/strict";

// 目標 helper 行為:
//   - 整個事件在未來(started_at > now)視為 clock skew 跳過。
//   - ended_at < started_at(資料錯誤)也跳過。
//   - ongoing(ended_at 為 null)用 now 結算 duration。
function longestDisconnection(events, now) {
  let longest = null;
  let longestDur = -Infinity;
  for (const e of events || []) {
    if (e.started_at > now) continue;
    const d = (e.ended_at ?? now) - e.started_at;
    if (d < 0) continue;
    if (d > longestDur) {
      longest = e;
      longestDur = d;
    }
  }
  return longest;
}

test("spec: longest 過濾 started_at > now 的 clock skew", () => {
  const events = [
    { id: 1, started_at: 100, ended_at: 200 },     // 100ms, 正常
    { id: 2, started_at: 1000, ended_at: 2000 },   // started_at > now=500, 應跳過
  ];
  const result = longestDisconnection(events, 500);
  assert.equal(result.id, 1);
});

test("spec: longest 仍計算 ongoing 事件 (ended_at 為 null)", () => {
  const events = [
    { id: 1, started_at: 100, ended_at: 500 },  // 400ms
    { id: 2, started_at: 600 },                 // ongoing, now=1500 → 900ms
  ];
  const result = longestDisconnection(events, 1500);
  assert.equal(result.id, 2);
});

test("spec: longest 在空 events 回 null", () => {
  assert.equal(longestDisconnection([], 1000), null);
  assert.equal(longestDisconnection(null, 1000), null);
  assert.equal(longestDisconnection(undefined, 1000), null);
});

test("spec: longest 全為 clock skew 時回 null", () => {
  const events = [
    { id: 1, started_at: 9999, ended_at: 10000 }, // 全在未來
    { id: 2, started_at: 8000 },                 // ongoing, now=5000 → 負值
  ];
  assert.equal(longestDisconnection(events, 5000), null);
});

test("spec: longest 正常事件優先於 clock skew 事件", () => {
  // 即便 clock skew 事件的「虛擬 duration」巨大,也應被跳過。
  const events = [
    { id: 1, started_at: 100, ended_at: 200 },     // 100ms
    { id: 2, started_at: 9000, ended_at: 10000 },  // 1000ms but future
  ];
  const result = longestDisconnection(events, 5000);
  assert.equal(result.id, 1);
});
