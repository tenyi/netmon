/* =========================================================================
   events.js — 事件歷史頁邏輯
   - 監聽 netmon:rangechange → 重抓 /api/events
   - 額外支援狀態過濾:全部 / 進行中 / 已恢復 (前端過濾,僅作用於目前頁)
   - 分頁:每頁 25 筆,從 X-Total-Count 讀總數;僅一頁時隱藏分頁器
   - 切換日期區間或狀態時自動回到第 1 頁
   - 自動更新開關:預設 ON,啟用時每 5 秒重抓 events + 抬頭 status;
     偏好寫到 localStorage,重整保留
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

  /**
   * 抓取分頁事件,回傳 { events, total }。
   * total 來自後端 X-Total-Count header;若後端沒回 header(未指定 limit),
   * 則以 events.length 作為 total(代表未分頁)。
   */
  async function fetchEventsPage(from, to, limit, offset) {
    const params = new URLSearchParams({ from: String(from), to: String(to) });
    if (limit > 0) {
      params.set("limit", String(limit));
      params.set("offset", String(offset));
    }
    const res = await fetch(`/api/events?${params.toString()}`, {
      headers: { Accept: "application/json" },
    });
    if (!res.ok) throw new Error(`/api/events → HTTP ${res.status}`);
    const events = await res.json();
    const headerVal = res.headers.get("X-Total-Count");
    const total = headerVal != null ? parseInt(headerVal, 10) : (events || []).length;
    return { events: events || [], total: Number.isFinite(total) ? total : 0 };
  }

  // ---------- 狀態 ----------
  /** @type {{key:string, from:number, to:number, granularity:string, label:string} | null} */
  let currentRange = null;
  let statusFilter = "all";
  const PAGE_SIZE = 25;
  const AUTO_REFRESH_MS = 5000;
  const STALE_THRESHOLD_MS = AUTO_REFRESH_MS * 3; // 3 個週期沒更新視為過時
  let currentPage = 1;
  let totalCount = 0;
  let lastUpdatedAt = 0;
  let autoRefreshEnabled = true;
  let autoRefreshTimer = null;
  let staleTimer = null;

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
      const msg = totalCount === 0
        ? "選定區間內沒有事件"
        : (currentPage > 1)
          ? "目前頁次沒有資料,請回到上一頁"
          : "目前頁次在篩選條件下沒有事件";
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

  function renderSummary() {
    const el = document.getElementById("events-summary");
    if (!el) return;
    const totalPages = Math.max(1, Math.ceil(totalCount / PAGE_SIZE));
    const from = (currentPage - 1) * PAGE_SIZE + 1;
    const to = Math.min(currentPage * PAGE_SIZE, totalCount);
    const rangeText = totalCount === 0
      ? "0 筆"
      : `第 ${from}–${to} 筆 / 共 ${totalCount} 筆`;
    el.innerHTML = `<span class="summary-item">${rangeText}</span>`;
  }

  // ---------- 分頁器 ----------
  function renderPagination() {
    const nav = document.getElementById("events-pagination");
    const info = document.getElementById("page-info");
    const firstBtn = document.getElementById("page-first");
    const prevBtn = document.getElementById("page-prev");
    const nextBtn = document.getElementById("page-next");
    const lastBtn = document.getElementById("page-last");
    if (!nav) return;

    const totalPages = Math.max(1, Math.ceil(totalCount / PAGE_SIZE));

    // 僅一頁時隱藏整個分頁器
    if (totalPages <= 1) {
      nav.hidden = true;
      return;
    }
    nav.hidden = false;

    if (info) info.textContent = `第 ${currentPage} 頁 / 共 ${totalPages} 頁`;
    if (firstBtn) firstBtn.disabled = currentPage <= 1;
    if (prevBtn) prevBtn.disabled = currentPage <= 1;
    if (nextBtn) nextBtn.disabled = currentPage >= totalPages;
    if (lastBtn) lastBtn.disabled = currentPage >= totalPages;
  }

  function goToPage(page) {
    const totalPages = Math.max(1, Math.ceil(totalCount / PAGE_SIZE));
    const next = Math.min(Math.max(1, page), totalPages);
    if (next === currentPage) return;
    currentPage = next;
    refresh();
    // 捲回表格頂端,讓使用者看到分頁後的資料
    const card = document.getElementById("events-body");
    if (card) card.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  function bindPagination() {
    const firstBtn = document.getElementById("page-first");
    const prevBtn = document.getElementById("page-prev");
    const nextBtn = document.getElementById("page-next");
    const lastBtn = document.getElementById("page-last");
    if (firstBtn) firstBtn.addEventListener("click", () => goToPage(1));
    if (prevBtn) prevBtn.addEventListener("click", () => goToPage(currentPage - 1));
    if (nextBtn) nextBtn.addEventListener("click", () => goToPage(currentPage + 1));
    if (lastBtn) {
      lastBtn.addEventListener("click", () => {
        const totalPages = Math.max(1, Math.ceil(totalCount / PAGE_SIZE));
        goToPage(totalPages);
      });
    }
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
        // 切換狀態篩選不重抓 (只過濾當前頁),但仍更新摘要與分頁狀態
        refreshSummaryOnly();
      });
    });
  }

  function refreshSummaryOnly() {
    // 純前端過濾:重畫表格與摘要,不重新 fetch
    // (注意:過濾後筆數可能變少,但分頁器仍依 totalCount 與 totalPages 判斷是否顯示)
    const tbody = document.getElementById("events-body");
    if (tbody) {
      // 取出目前 tbody 的 events cache
      const cache = currentPageEvents;
      renderTable(cache);
    }
    renderSummary();
    renderPagination();
  }

  // ---------- 抓取 ----------
  let isFetching = false;
  let currentPageEvents = [];

  async function refresh() {
    if (!currentRange || isFetching) return;
    // 自動更新時讓 preset 區間隨時間滑動 (custom 模式由 slideRange 原樣回傳)
    if (window.__netmonRangeSlide) {
      currentRange = window.__netmonRangeSlide(currentRange);
    }
    isFetching = true;
    try {
      const offset = (currentPage - 1) * PAGE_SIZE;
      const [page, status] = await Promise.all([
        fetchEventsPage(currentRange.from, currentRange.to, PAGE_SIZE, offset),
        paintHeader(),
      ]);
      currentPageEvents = page.events;
      totalCount = page.total;
      renderTable(page.events);
      renderSummary();
      renderPagination();
      markUpdated();
      const subtitle = document.getElementById("events-subtitle");
      if (subtitle) subtitle.textContent = currentRange.label;
      // 同步更新 range.js 的「目前區間」文字 (slide 後 from/to 已改)
      const rangeLabel = document.getElementById("range-current");
      if (rangeLabel) rangeLabel.textContent = currentRange.label;
      // 確保 paintHeader 沒有被優化掉
      void status;
    } catch (e) {
      console.error("events refresh failed", e);
      const tbody = document.getElementById("events-body");
      if (tbody) tbody.innerHTML = '<tr><td colspan="5" class="table-state">載入失敗,請稍後重試</td></tr>';
      totalCount = 0;
      renderPagination();
    } finally {
      isFetching = false;
    }
  }

  function onRangeChange(ev) {
    currentRange = ev.detail;
    // 切換日期區間時回到第 1 頁
    currentPage = 1;
    refresh();
  }

  // ---------- 抬頭狀態 (從 /api/status 拿 gateway IP) ----------
  async function paintHeader() {
    try {
      const res = await fetch("/api/status", { headers: { Accept: "application/json" } });
      if (!res.ok) return null;
      const s = await res.json();
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
      return s;
    } catch (e) {
      // 忽略,抬頭保持預設
      return null;
    }
  }

  // ---------- 「最後更新」時間標記 ----------
  function markUpdated() {
    lastUpdatedAt = Date.now();
    const el = document.getElementById("last-updated");
    if (!el) return;
    el.classList.remove("is-stale");
    el.textContent = `最後更新:剛剛`;
    el.hidden = false;

    if (staleTimer) clearTimeout(staleTimer);
    staleTimer = setTimeout(() => {
      const age = Date.now() - lastUpdatedAt;
      if (age >= STALE_THRESHOLD_MS) el.classList.add("is-stale");
    }, STALE_THRESHOLD_MS);
  }

  function renderLastUpdated() {
    const el = document.getElementById("last-updated");
    if (!el || !lastUpdatedAt) return;
    const ageSec = Math.max(0, Math.floor((Date.now() - lastUpdatedAt) / 1000));
    const text = ageSec < 5
      ? "剛剛"
      : ageSec < 60
        ? `${ageSec} 秒前`
        : `${Math.floor(ageSec / 60)} 分 ${ageSec % 60} 秒前`;
    el.textContent = `最後更新:${text}`;
    if (ageSec * 1000 >= STALE_THRESHOLD_MS) el.classList.add("is-stale");
    else el.classList.remove("is-stale");
  }

  // ---------- 自動更新開關 ----------
  const AUTO_REFRESH_KEY = "netmon:autoRefresh:events";

  function loadAutoRefreshPref() {
    try {
      const v = localStorage.getItem(AUTO_REFRESH_KEY);
      if (v === "0") return false;
    } catch (_) {}
    return true; // 預設 ON
  }

  function saveAutoRefreshPref(enabled) {
    try {
      localStorage.setItem(AUTO_REFRESH_KEY, enabled ? "1" : "0");
    } catch (_) {}
  }

  function startAutoRefresh() {
    stopAutoRefresh();
    if (!autoRefreshEnabled) return;
    autoRefreshTimer = setInterval(() => {
      // 不在使用者操作當下打斷 (goToPage / rangechange / status filter 都會設 isFetching)
      refresh();
    }, AUTO_REFRESH_MS);
  }

  function stopAutoRefresh() {
    if (autoRefreshTimer) {
      clearInterval(autoRefreshTimer);
      autoRefreshTimer = null;
    }
  }

  function bindAutoRefresh() {
    const checkbox = document.getElementById("auto-refresh");
    if (!checkbox) return;
    autoRefreshEnabled = loadAutoRefreshPref();
    checkbox.checked = autoRefreshEnabled;
    checkbox.addEventListener("change", () => {
      autoRefreshEnabled = checkbox.checked;
      saveAutoRefreshPref(autoRefreshEnabled);
      if (autoRefreshEnabled) {
        // 立即抓一次,別等下個週期
        refresh();
        startAutoRefresh();
      } else {
        stopAutoRefresh();
      }
    });
  }

  // ---------- 啟動 ----------
  document.addEventListener("DOMContentLoaded", () => {
    bindStatusChips();
    bindPagination();
    bindAutoRefresh();
    window.addEventListener("netmon:rangechange", onRangeChange);
    if (window.__netmonRange) currentRange = window.__netmonRange;
    refresh(); // 這個 refresh() 內部已包含 paintHeader,並啟動 markUpdated
    startAutoRefresh();
    // 「最後更新」文字每 1 秒重算 (讓「N 秒前」即時更新)
    setInterval(renderLastUpdated, 1000);
  });
})();
