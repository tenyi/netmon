/* =========================================================================
   kpi.js — dashboard KPI 的純計算函式
   - 不依賴 DOM,可在 node 直接跑(node --test internal/web/)
   - 瀏覽器端掛到 window.__netmonKpi 供 dashboard.js 使用
   ========================================================================= */
(function (global) {
  "use strict";

  /**
   * 由 stats 桶算出區間的加權平均延遲 / 加權遺失率。
   * 加權以 sample_count 為權重(與後端 aggregateStats 相同口径)。
   * @param {Array<{latency_avg_ms:number, loss_pct:number, sample_count:number}> | null | undefined} stats
   * @returns {{ok:boolean, avgMs?:number, lossPct?:number, samples?:number}}
   *   ok=false 時代表無有效樣本。
   */
  function latencyKpi(stats) {
    const rows = Array.isArray(stats) ? stats : [];
    let samples = 0;
    let latencyWeighted = 0;
    let lossWeighted = 0;
    for (const s of rows) {
      const n = Number(s && s.sample_count) || 0;
      if (n <= 0) continue;
      samples += n;
      latencyWeighted += (Number(s.latency_avg_ms) || 0) * n;
      lossWeighted += (Number(s.loss_pct) || 0) * n;
    }
    if (samples === 0) return { ok: false };
    return {
      ok: true,
      avgMs: latencyWeighted / samples,
      lossPct: lossWeighted / samples,
      samples,
    };
  }

  const api = { latencyKpi };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  if (global) global.__netmonKpi = api;
})(
  typeof globalThis !== "undefined"
    ? globalThis
    : typeof window !== "undefined"
      ? window
      : this
);
