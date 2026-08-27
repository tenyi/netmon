/* =========================================================================
   kpi.js — dashboard / events 共用前端 helpers
   - 不依賴 DOM,可在 node 直接跑(node --test internal/web/)
   - 瀏覽器端掛到 window.__netmonKpi 供 dashboard.js / events.js 使用
   - longestDisconnection:最長斷線事件計算,過濾 clock skew (started_at > now)。
   - makeGuardedFetch:並發 reentrancy guard,慢網路下第二次呼叫回傳 skipped=true。
   - buildSummaryItem:回傳結構化節點描述,呼叫端用 createElement + textContent
     而非 innerHTML 寫入 DOM,符合 events.js renderSummary spec。
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

  /**
   * 從事件清單中挑出「最長的斷線」,忽略 clock skew 與資料錯誤。
   * - started_at > now 視為整個事件在未來,跳過 (避免時鐘回撥或時區誤判污染)。
   * - ended_at < started_at 視為資料錯誤,跳過。
   * - ongoing (ended_at 為 null) 用 now 結算 duration。
   * @param {Array<{started_at:number, ended_at?:number|null}> | null | undefined} events
   * @param {number} now 結算 ongoing 事件的時間點 (ms)
   * @returns {object | null} 最長斷線事件;空清單或全為 invalid 時回 null。
   */
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

  /**
   * 並發 reentrancy guard 包裝工廠。
   * 第二次呼叫在第一次未完成時回傳 { skipped: true },不執行 fetchFn,
   * 避免慢網路下 interval 觸發多個 fetch 重疊。
   * fetchFn 拋錯時 finally 仍釋放 flag,確保下一次能繼續。
   * @template T
   * @param {() => Promise<T>} fetchFn 實際執行 fetch 的 async 函式
   * @returns {{ fetch: () => Promise<{skipped: true} | {skipped: false, data: T}>, get isFetching(): boolean }}
   */
  function makeGuardedFetch(fetchFn) {
    let inFlight = false;
    async function guarded() {
      if (inFlight) return { skipped: true };
      inFlight = true;
      try {
        const data = await fetchFn();
        return { skipped: false, data };
      } finally {
        inFlight = false;
      }
    }
    return { fetch: guarded, get isFetching() { return inFlight; } };
  }

  /**
   * 為 events 頁 summary 區段回傳結構化節點描述。
   * 呼叫端用 createElement + textContent 寫入,而非 innerHTML 注入,
   * 避免日後插值 user 輸入時的 XSS 風險,並與其他 render* 函式 pattern 一致。
   * @param {string} rangeText 要顯示的字串
   * @returns {{tag: string, className: string, textContent: string}}
   */
  function buildSummaryItem(rangeText) {
    return { tag: "span", className: "summary-item", textContent: rangeText };
  }

  const api = { latencyKpi, longestDisconnection, makeGuardedFetch, buildSummaryItem };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  if (global) global.__netmonKpi = api;
})(
  typeof globalThis !== "undefined"
    ? globalThis
    : typeof window !== "undefined"
      ? window
      : this
);
