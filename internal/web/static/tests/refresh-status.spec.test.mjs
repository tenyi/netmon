// spec: refreshStatus 應避免並發 fetch — 第二次呼叫在第一次未完成時應被 skip,
// 避免慢網路下多個 interval 觸發重疊 fetch。
//
// 對應 dashboard.js refreshStatus (dashboard.js:321-332)。
// 紅燈:dashboard.js refreshStatus 沒有 isFetching guard,並發時會 race。
// 綠燈條件:在 refreshStatus 加 isFetching flag (比照 refreshRangeData:319, 335, 359-361)。

import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const kpi = require("../kpi.js");

const { makeGuardedFetch } = kpi;

test("spec: guarded refresh skip 並發第二次呼叫", async () => {
  let releaseFirst;
  const slowFetch = () => new Promise((r) => { releaseFirst = r; });
  const guarded = makeGuardedFetch(slowFetch);

  const p1 = guarded.fetch();
  const p2 = guarded.fetch();

  const r2 = await p2;
  assert.equal(r2.skipped, true);
  releaseFirst();
  const r1 = await p1;
  assert.equal(r1.skipped, false);
});

test("spec: guarded refresh 在第一次完成後可再次呼叫", async () => {
  let n = 0;
  const guarded = makeGuardedFetch(async () => { n++; });

  await guarded.fetch();
  await guarded.fetch();

  assert.equal(n, 2, "第一次完成後第二次不應被 skip");
});

test("spec: guarded refresh 在第一次拋錯後仍釋放 flag", async () => {
  const guarded = makeGuardedFetch(async () => { throw new Error("boom"); });

  await assert.rejects(guarded.fetch(), /boom/);
  // flag 已釋放,第二次呼叫不應被 skip 也不應殘留 inFlight=true。
  let secondCalled = false;
  const guarded2 = makeGuardedFetch(async () => { secondCalled = true; });
  await guarded2.fetch();
  assert.equal(secondCalled, true);
});

test("spec: 多個並發呼叫只有第一個跑,其餘被 skip", async () => {
  let releaseFirst;
  const slowFetch = () => new Promise((r) => { releaseFirst = r; });
  const guarded = makeGuardedFetch(slowFetch);

  const ps = [guarded.fetch(), guarded.fetch(), guarded.fetch(), guarded.fetch()];
  const results = await Promise.all([ps[1], ps[2], ps[3]]);
  for (const r of results) assert.equal(r.skipped, true);

  releaseFirst();
  const r0 = await ps[0];
  assert.equal(r0.skipped, false);
});

test("spec: isFetching getter 在 fetch 中為 true,完成後為 false", async () => {
  let releaseFirst;
  const slowFetch = () => new Promise((r) => { releaseFirst = r; });
  const guarded = makeGuardedFetch(slowFetch);

  assert.equal(guarded.isFetching, false);
  const p = guarded.fetch();
  assert.equal(guarded.isFetching, true);
  releaseFirst();
  await p;
  assert.equal(guarded.isFetching, false);
});
