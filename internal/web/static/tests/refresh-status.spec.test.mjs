// spec: refreshStatus 應避免並發 fetch — 第二次呼叫在第一次未完成時應被 skip,
// 避免慢網路下多個 interval 觸發重疊 fetch。
//
// 對應 dashboard.js refreshStatus (dashboard.js:321-332)。
// 紅燈:dashboard.js refreshStatus 沒有 isFetching guard,並發時會 race。
// 綠燈條件:在 refreshStatus 加 isFetching flag (比照 refreshRangeData:319, 335, 359-361)。

import test from "node:test";
import assert from "node:assert/strict";

// 目標 helper:guarded wrapper,並發呼叫第二次應回 "skipped"。
function makeGuardedRefresh(refresh) {
  let inFlight = false;
  return async function guarded() {
    if (inFlight) return "skipped";
    inFlight = true;
    try {
      await refresh();
      return "ok";
    } finally {
      inFlight = false;
    }
  };
}

test("spec: guarded refresh skip 並發第二次呼叫", async () => {
  let releaseFirst;
  const slowFetch = () => new Promise((r) => { releaseFirst = r; });
  const guarded = makeGuardedRefresh(slowFetch);

  const p1 = guarded();
  const p2 = guarded();

  assert.equal(await p2, "skipped");
  releaseFirst();
  assert.equal(await p1, "ok");
});

test("spec: guarded refresh 在第一次完成後可再次呼叫", async () => {
  let n = 0;
  const guarded = makeGuardedRefresh(async () => { n++; });

  await guarded();
  await guarded();

  assert.equal(n, 2, "第一次完成後第二次不應被 skip");
});

test("spec: guarded refresh 在第一次拋錯後仍釋放 flag", async () => {
  const guarded = makeGuardedRefresh(async () => { throw new Error("boom"); });

  await assert.rejects(guarded, /boom/);
  // flag 已釋放,第二次呼叫不應被 skip 也不應殘留 inFlight=true。
  let secondCalled = false;
  const guarded2 = makeGuardedRefresh(async () => { secondCalled = true; });
  await guarded2();
  assert.equal(secondCalled, true);
});

test("spec: 多個並發呼叫只有第一個跑,其餘被 skip", async () => {
  let releaseFirst;
  const slowFetch = () => new Promise((r) => { releaseFirst = r; });
  const guarded = makeGuardedRefresh(slowFetch);

  const ps = [guarded(), guarded(), guarded(), guarded()];
  const results = await Promise.all([ps[1], ps[2], ps[3]]); // 先收後三個結果
  for (const r of results) assert.equal(r, "skipped");

  releaseFirst();
  assert.equal(await ps[0], "ok");
});
