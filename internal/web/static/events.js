/* =========================================================================
   events.js — 事件歷史頁邏輯
   - 監聽 netmon:rangechange → 重抓 /api/events
   - 額外支援狀態過濾:全部 / 進行中 / 已恢復 (前端過濾)
   - 表格欄位:狀態 / 開始 / 結束 / 持續 / 原因
   ========================================================================= */
(function () {
  "use strict";

  // ---------- 工具 ----------
  function formatDateTime(ms) {
    if (!ms) return "—";
    return new Date(ms).toLocaleString("zh-TW", {
      year: "numeric", month: "2-digit", day: "2-digit",
      hour: "2-digit", minute: "2-digit", second: "2-digit",
      hour12: false,
    });
  }

  function formatDuration(startMs, endMs) {
    const end = endMs || Date.now();
    let sec = Math.max(0, Math.floor((end - startMs) / 1000));
    if (sec < 60) return `${sec} 秒`;
    const min = Math.floor(sec / 60);
    sec = sec % 60;
    if (min < 60) return `${min} 分 ${sec} 秒`;
    const hr = Math.floor(min / 60);
    const m = min % 60;
    if (hr < 24) return `${hr} 小時 ${m} 分`;
    const day = Math.floor(hr / 24);
    const h = hr % 24;
    return `${day} 天 ${h} 小時`;
  }

  function escapeHtml(str) {
    return String(str ?? "").replace(/[&<>"']/g, (c) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[c]));
  }

  async function fetchJson(url) {
    const res = await fetch(url, { headers: { Accept: "application/json" } });
    if (!res.ok) throw new Error(`${url} → HTTP ${res.status}`);
    return res.json();
  }

  // ---------- 狀態 ----------
  /** @type {{key:string, from:number, to:number, granularity:string, label:string} | null} */
  let currentRange = null;
  let statusFilter = "all";
  let lastFetched = [];

  // ---------- 表格 ----------
  function renderTable(events) {
    const tbody = document.getElementById("events-body");
    if (!tbody) return;
    const filtered = statusFilter === "all"
      ? events
      : statusFilter === "ongoing"
        ? events.filter((e) => !e.ended_at)
        : events.filter((e) => !!e.ended_at);

    if (!filtered.length) {
      const msg = !events.length
        ? "選定區間內沒有事件"
        : "目前篩選條件下沒有事件";
      tbody.innerHTML = `<tr><td colspan="5" class="table-state">${msg}</td></tr>`;
      return;
    }

    tbody.innerHTML = filtered.map((e) => {
      const ongoing = !e.ended_at;
      const badge = ongoing
        ? '<span class="badge badge--offline">進行中</span>'
        : '<span class="badge badge--online">已恢復</span>';
      const endCell = ongoing
        ? '<span class="badge badge--offline">—</span>'
        : escapeHtml(formatDateTime(e.ended_at));
      return `
        <tr>
          <td>${badge}</td>
          <td class="col-time">${escapeHtml(formatDateTime(e.started_at))}</td>
          <td class="col-time">${endCell}</td>
          <td class="col-duration">${escapeHtml(formatDuration(e.started_at, e.ended_at))}</td>
          <td class="col-reason" title="${escapeHtml(e.reason)}">${escapeHtml(e.reason || "—")}</td>
        </tr>
      `;
    }).join("");
  }

  function renderSummary(filteredCount, totalCount) {
    const el = document.getElementById("events-summary");
    if (!el) return;
    const ongoing = lastFetched.filter((e) => !e.ended_at).length;
    const span = lastFetched.reduce((acc, e) => {
      const d = (e.ended_at || Date.now()) - e.started_at;
      return acc + d;
    }, 0);
    const avg = lastFetched.length ? Math.round(span / lastFetched.length / 1000) : 0;
    el.innerHTML = `
      <span class="summary-item">顯示 <span class="summary-num">${filteredCount}</span> / ${totalCount} 筆</span>
      <span class="summary-item">進行中 <span class="summary-num">${ongoing}</span></span>
      <span class="summary-item">平均斷線 <span class="summary-num">${avg}</span> 秒</span>
    `;
  }

  // ---------- 篩選 chips ----------
  function bindStatusChips() {
    const chips = document.querySelectorAll(".chip--status");
    chips.forEach((c) => {
      c.addEventListener("click", () => {
        statusFilter = c.dataset.status;
        chips.forEach((other) => {
          other.setAttribute("aria-pressed", other === c ? "true" : "false");
        });
        renderTable(lastFetched);
        renderSummary(
          statusFilter === "all" ? lastFetched.length : lastFetched.filter(filterFn).length,
          lastFetched.length,
        );
      });
    });
  }

  function filterFn(e) {
    if (statusFilter === "all") return true;
    if (statusFilter === "ongoing") return !e.ended_at;
    return !!e.ended_at;
  }

  // ---------- 抓取 ----------
  let isFetching = false;
  async function refresh() {
    if (!currentRange || isFetching) return;
    isFetching = true;
    try {
      const params = new URLSearchParams({
        from: String(currentRange.from),
        to: String(currentRange.to),
      });
      const events = await fetchJson(`/api/events?${params.toString()}`);
      lastFetched = events || [];
      const filtered = lastFetched.filter(filterFn);
      renderTable(lastFetched);
      renderSummary(filtered.length, lastFetched.length);
      const subtitle = document.getElementById("events-subtitle");
      if (subtitle) subtitle.textContent = currentRange.label;
    } catch (e) {
      console.error("events refresh failed", e);
      const tbody = document.getElementById("events-body");
      if (tbody) tbody.innerHTML = '<tr><td colspan="5" class="table-state">載入失敗,請稍後重試</td></tr>';
    } finally {
      isFetching = false;
    }
  }

  function onRangeChange(ev) {
    currentRange = ev.detail;
    refresh();
  }

  // ---------- 抬頭狀態 (從 /api/status 拿 gateway IP) ----------
  async function paintHeader() {
    try {
      const s = await fetchJson("/api/status");
      const gw = document.getElementById("header-gateway");
      if (gw) gw.textContent = s.gateway_ip || "—";
      const text = document.getElementById("header-status-text");
      if (text) {
        if (s.unknown) text.textContent = "—";
        else text.textContent = s.online ? "連線中" : "斷線中";
      }
      const dot = document.querySelector("#header-status .status-dot");
      if (dot) {
        dot.classList.remove("status-dot--online", "status-dot--offline", "status-dot--unknown");
        const kind = s.unknown ? "unknown" : s.online ? "online" : "offline";
        dot.classList.add(`status-dot--${kind}`);
      }
    } catch (e) {
      // 忽略,抬頭保持預設
    }
  }

  // ---------- 啟動 ----------
  document.addEventListener("DOMContentLoaded", () => {
    bindStatusChips();
    window.addEventListener("netmon:rangechange", onRangeChange);
    if (window.__netmonRange) currentRange = window.__netmonRange;
    paintHeader();
    refresh();
  });
})();
