/* =========================================================================
   dashboard.js — 總覽頁邏輯
   - 監聽 netmon:rangechange → 重抓 events + stats
   - 獨立每 5 秒輪詢 /api/status 來更新 header/KPI 即時狀態
   - 圖表用 Chart.js 繪 latency / loss 折線圖
   ========================================================================= */
(function () {
  "use strict";

  // ---------- 公用工具 ----------
  function formatDateTime(ms) {
    if (!ms) return "—";
    return new Date(ms).toLocaleString("zh-TW", {
      year: "numeric", month: "2-digit", day: "2-digit",
      hour: "2-digit", minute: "2-digit", second: "2-digit",
      hour12: false,
    });
  }

  function formatShortTime(ms) {
    if (!ms) return "—";
    return new Date(ms).toLocaleTimeString("zh-TW", {
      hour: "2-digit", minute: "2-digit", hour12: false,
    });
  }

  function formatShortDateTime(ms) {
    if (!ms) return "—";
    return new Date(ms).toLocaleString("zh-TW", {
      month: "2-digit", day: "2-digit",
      hour: "2-digit", minute: "2-digit", hour12: false,
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

  // ---------- API ----------
  async function fetchJson(url) {
    const res = await fetch(url, { headers: { Accept: "application/json" } });
    if (!res.ok) throw new Error(`${url} → HTTP ${res.status}`);
    return res.json();
  }

  function buildRangeQuery(state) {
    const p = new URLSearchParams();
    p.set("from", String(state.from));
    p.set("to", String(state.to));
    if (state.granularity) p.set("granularity", state.granularity);
    return p.toString();
  }

  // ---------- 狀態 / KPI ----------
  function applyStatusToUI(status) {
    const dot = document.querySelector("#header-status .status-dot");
    const text = document.querySelector("#header-status .status-text");
    const kpiDot = document.getElementById("kpi-status-dot");
    const kpiText = document.getElementById("kpi-status-text");
    const kpiMeta = document.getElementById("kpi-status-meta");
    const headerGw = document.getElementById("header-gateway");

    let kind = "unknown";
    let label = "未知";
    if (!status.unknown) {
      kind = status.online ? "online" : "offline";
      label = status.online ? "連線中" : "斷線中";
    }

    [dot, kpiDot].forEach((el) => {
      if (!el) return;
      el.classList.remove("status-dot--online", "status-dot--offline", "status-dot--unknown");
      el.classList.add(`status-dot--${kind}`);
    });
    if (text) text.textContent = label;
    if (kpiText) kpiText.textContent = label;
    if (headerGw) headerGw.textContent = status.gateway_ip || "—";

    if (kpiMeta) {
      const parts = [`更新於 ${formatShortTime(status.last_check_at)}`];
      if (status.last_latency_ms != null) {
        parts.push(`延遲 ${status.last_latency_ms.toFixed(1)} ms`);
      }
      if (status.open_event) {
        parts.push(`斷線自 ${formatShortTime(status.open_event.started_at)}`);
      }
      kpiMeta.textContent = parts.join(" · ");
    }
  }

  // ---------- 事件時間軸 ----------
  function renderTimeline(events) {
    const container = document.getElementById("events-timeline");
    if (!container) return;
    if (!events || !events.length) {
      container.innerHTML = '<div class="timeline-empty">選定區間內沒有斷線事件 🎉</div>';
      return;
    }
    // 最多顯示 12 筆,最新的在前面 (API 已 order by desc)
    const slice = events.slice(0, 12);
    container.innerHTML = slice.map((e) => {
      const ongoing = !e.ended_at;
      const dotClass = ongoing ? "timeline-dot--ongoing" : "timeline-dot--offline";
      const badge = ongoing
        ? '<span class="badge badge--offline">進行中</span>'
        : '<span class="badge badge--online">已恢復</span>';
      return `
        <div class="timeline-row ${ongoing ? "timeline-row--ongoing" : ""}" role="listitem">
          <span class="timeline-dot ${dotClass}" aria-hidden="true"></span>
          <div class="timeline-main">
            <span class="timeline-when">${escapeHtml(formatShortDateTime(e.started_at))}</span>
            <span class="timeline-reason" title="${escapeHtml(e.reason)}">${escapeHtml(e.reason || "未提供原因")}</span>
          </div>
          <span class="timeline-duration">${escapeHtml(formatDuration(e.started_at, e.ended_at))}</span>
          <span class="timeline-status">${badge}</span>
        </div>
      `;
    }).join("");
    if (events.length > slice.length) {
      container.innerHTML += `<div class="timeline-empty" style="padding:0.5rem">…另外 ${events.length - slice.length} 筆 (請至「事件歷史」檢視)</div>`;
    }
  }

  // ---------- KPI:事件統計 ----------
  function renderEventKpis(events) {
    const count = document.getElementById("kpi-events-count");
    const meta = document.getElementById("kpi-events-meta");
    const longestVal = document.getElementById("kpi-longest-value");
    const longestMeta = document.getElementById("kpi-longest-meta");

    const total = events ? events.length : 0;
    if (count) count.textContent = total;
    if (meta) {
      const ongoing = events ? events.filter((e) => !e.ended_at).length : 0;
      meta.textContent = total === 0
        ? "區間內連線穩定"
        : ongoing > 0
          ? `目前 ${ongoing} 筆進行中`
          : "全部已恢復";
    }

    if (longestVal && longestMeta) {
      if (!total) {
        longestVal.textContent = "—";
        longestMeta.textContent = "區間內無斷線";
      } else {
        const now = Date.now();
        let longest = events[0];
        let longestDur = (longest.ended_at || now) - longest.started_at;
        for (const e of events) {
          const d = (e.ended_at || now) - e.started_at;
          if (d > longestDur) {
            longest = e;
            longestDur = d;
          }
        }
        longestVal.textContent = formatDuration(longest.started_at, longest.ended_at);
        longestMeta.textContent = `發生於 ${formatShortDateTime(longest.started_at)}`;
      }
    }
  }

  // ---------- 圖表 ----------
  const chartTextColor = "#94a3b8";
  const chartGridColor = "rgba(148, 163, 184, 0.12)";

  function chartOptions() {
    return {
      responsive: true,
      maintainAspectRatio: false,
      animation: { duration: 250 },
      interaction: { mode: "index", intersect: false },
      scales: {
        // x scale 用預設的 category 即可(Chart.js 4.x line chart 預設值)。
        // 注意:category scale 的 ticks.callback 收到的是 index (0,1,2...),
        // 不是 labels 陣列中的值,無法在 callback 內從 value 還原時間;
        // 因此 labels 必須在 renderCharts() 預先格式化為字串再餵入。
        x: {
          ticks: { color: chartTextColor, maxTicksLimit: 8, maxRotation: 0 },
          grid: { color: chartGridColor, drawBorder: false },
        },
        y: {
          ticks: { color: chartTextColor },
          grid: { color: chartGridColor, drawBorder: false },
          beginAtZero: true,
        },
      },
      plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: "#0b1220",
          borderColor: "#1f2c44",
          borderWidth: 1,
          titleColor: "#f8fafc",
          bodyColor: "#e5e7eb",
          padding: 10,
        },
      },
    };
  }

  let latencyChart = null;
  let lossChart = null;

  function buildChartConfig(label, values, color, fillColor) {
    return {
      type: "line",
      data: {
        labels: label,
        datasets: [{
          data: values,
          borderColor: color,
          backgroundColor: fillColor,
          borderWidth: 2,
          fill: true,
          tension: 0.25,
          pointRadius: 0,
          pointHoverRadius: 4,
        }],
      },
      options: chartOptions(),
    };
  }

  // 把 bucket_start 預先格式化為顯示字串,直接餵給 category scale 當 labels。
  // (Chart.js 4.x 的 x scale 預設就是 category,而 category scale 的
  //  ticks.callback 收到的是 index 而非 labels 值,無法在 callback 裡
  //  從 value 還原時間;若要在 callback 取得 label 字串,得用
  //  this.getLabelForValue(value),但這需要 function expression 拿到 this,
  //  反而比預先格式化麻煩。)
  function formatBucketLabels(stats, state) {
    const useShortTime = (state.to - state.from) <= 24 * 60 * 60 * 1000;
    return (stats || []).map((s) => {
      if (!s.bucket_start) return "—";
      return useShortTime ? formatShortTime(s.bucket_start) : formatShortDateTime(s.bucket_start);
    });
  }

  function renderCharts(stats, state) {
    const labels = formatBucketLabels(stats, state);
    const latencies = (stats || []).map((s) => s.latency_avg_ms);
    const losses = (stats || []).map((s) => s.loss_pct);
    const empty = !labels.length;

    const opts = chartOptions();
    // 不再設 x ticks.callback:labels 已是格式化好的字串,category scale 直接顯示。
    // maxTicksLimit: 8 仍會讓 Chart.js 等距挑 8 個 tick 顯示,避免 X 軸擠滿文字。
    opts.scales.y.ticks.callback = (v) => v;

    if (!latencyChart) {
      const latEl = document.getElementById("latency-chart");
      const lossEl = document.getElementById("loss-chart");
      if (latEl) latencyChart = new Chart(latEl, buildChartConfig(labels, latencies, "#3b82f6", "rgba(59,130,246,0.18)"));
      if (lossEl) lossChart = new Chart(lossEl, buildChartConfig(labels, losses, "#f59e0b", "rgba(245,158,11,0.18)"));
    }
    if (latencyChart) {
      latencyChart.data.labels = labels;
      latencyChart.data.datasets[0].data = latencies;
      latencyChart.options = opts;
      latencyChart.update();
    }
    if (lossChart) {
      lossChart.data.labels = labels;
      lossChart.data.datasets[0].data = losses;
      lossChart.options = opts;
      lossChart.update();
    }

    // 在圖表上蓋 "無資料" 提示
    document.querySelectorAll(".chart-card").forEach((card) => {
      let hint = card.querySelector(".chart-empty");
      if (empty) {
        if (!hint) {
          hint = document.createElement("div");
          hint.className = "chart-empty";
          hint.textContent = "選定區間內沒有資料";
          card.querySelector(".chart-wrapper").appendChild(hint);
        }
      } else if (hint) {
        hint.remove();
      }
    });
  }

  // ---------- 整合 ----------
  let currentRange = null;
  let isFetching = false;

  async function refreshStatus() {
    try {
      const status = await fetchJson("/api/status");
      applyStatusToUI(status);
    } catch (e) {
      console.error("status refresh failed", e);
    }
  }

  async function refreshRangeData() {
    if (!currentRange || isFetching) return;
    // 自動更新時讓 preset 區間隨時間滑動 (custom 模式由 slideRange 原樣回傳)
    if (window.__netmonRangeSlide) {
      currentRange = window.__netmonRangeSlide(currentRange);
    }
    isFetching = true;
    try {
      const [events, stats] = await Promise.all([
        fetchJson(`/api/events?${buildRangeQuery(currentRange)}`),
        fetchJson(`/api/stats?${buildRangeQuery(currentRange)}`),
      ]);
      renderEventKpis(events);
      renderTimeline(events);
      renderCharts(stats, currentRange);
      const subtitle = document.getElementById("events-subtitle");
      if (subtitle) {
        subtitle.textContent = `${currentRange.label} · 共 ${events.length} 筆`;
      }
      // 同步更新 range.js 的「目前區間」文字 (slide 後 from/to 已改)
      const rangeLabel = document.getElementById("range-current");
      if (rangeLabel) rangeLabel.textContent = currentRange.label;
    } catch (e) {
      console.error("range refresh failed", e);
    } finally {
      isFetching = false;
    }
  }

  function onRangeChange(ev) {
    currentRange = ev.detail;
    refreshRangeData();
  }

  // ---------- 啟動 ----------
  document.addEventListener("DOMContentLoaded", () => {
    window.addEventListener("netmon:rangechange", onRangeChange);
    if (window.__netmonRange) {
      currentRange = window.__netmonRange;
    }
    refreshStatus();
    refreshRangeData();
    setInterval(refreshStatus, 5000);
    // 區間資料也每 5 秒重抓,搭配 slideRange 讓 preset 區間隨時間滑動
    setInterval(refreshRangeData, 5000);
  });
})();
