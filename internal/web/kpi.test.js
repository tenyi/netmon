"use strict";
/* 用 node:test 跑: node --test internal/web/ */
const test = require("node:test");
const assert = require("node:assert/strict");
const kpi = require("./static/kpi.js");

test("latencyKpi: 空/null 回 ok=false", () => {
  assert.equal(kpi.latencyKpi([]).ok, false);
  assert.equal(kpi.latencyKpi(null).ok, false);
  assert.equal(kpi.latencyKpi(undefined).ok, false);
});

test("latencyKpi: 單一 bucket 原值透傳", () => {
  const r = kpi.latencyKpi([
    { bucket_start: 1, latency_avg_ms: 12.5, loss_pct: 4, sample_count: 20 },
  ]);
  assert.equal(r.ok, true);
  assert.ok(Math.abs(r.avgMs - 12.5) < 1e-9);
  assert.ok(Math.abs(r.lossPct - 4) < 1e-9);
  assert.equal(r.samples, 20);
});

test("latencyKpi: 多 bucket 以 sample_count 加權", () => {
  const r = kpi.latencyKpi([
    { bucket_start: 1, latency_avg_ms: 10, loss_pct: 0, sample_count: 1 },
    { bucket_start: 2, latency_avg_ms: 20, loss_pct: 10, sample_count: 3 },
  ]);
  assert.equal(r.ok, true);
  // avg = (10*1 + 20*3) / 4 = 17.5
  assert.ok(Math.abs(r.avgMs - 17.5) < 1e-9);
  // loss = (0*1 + 10*3) / 4 = 7.5
  assert.ok(Math.abs(r.lossPct - 7.5) < 1e-9);
  assert.equal(r.samples, 4);
});

test("latencyKpi: 全 0 樣數視為無資料", () => {
  const r = kpi.latencyKpi([
    { bucket_start: 1, latency_avg_ms: 5, loss_pct: 1, sample_count: 0 },
  ]);
  assert.equal(r.ok, false);
});

test("latencyKpi: 缺欄位不爆炸", () => {
  const r = kpi.latencyKpi([{ sample_count: 2 }, { latency_avg_ms: 3 }]);
  // 第一筆 2 樣本貢獻 0*2; 第二筆 sample_count 缺 → 跳過
  assert.equal(r.ok, true);
  assert.equal(r.avgMs, 0);
  assert.equal(r.samples, 2);
});
