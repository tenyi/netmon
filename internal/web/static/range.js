/* =========================================================================
   range.js — 共用日期區間選擇器
   - 支援快捷 chip (1h/6h/24h/7d/30d/自訂)
   - 自訂模式:兩個 datetime-local 輸入 + 套用
   - 透過 CustomEvent("netmon:rangechange") 通知其他腳本
   - 預設值由 query string ?range=24h&from=ms&to=ms 帶入
   - 持久化到 sessionStorage (per-page)
   ========================================================================= */
(function () {
  "use strict";

  // 預設值設為 24h,實際由 init() 從 query string 讀取
  const PRESETS = {
    "1h": 60 * 60 * 1000,
    "6h": 6 * 60 * 60 * 1000,
    "24h": 24 * 60 * 60 * 1000,
    "7d": 7 * 24 * 60 * 60 * 1000,
    "30d": 30 * 24 * 60 * 60 * 1000,
  };

  /** 由 duration(ms) 決定 stats 用的 aggregation bucket */
  function pickGranularity(rangeMs) {
    const hour = 60 * 60 * 1000;
    const day = 24 * hour;
    if (rangeMs <= 6 * hour) return ""; // 原始 1m bucket
    if (rangeMs <= day) return "5m";
    if (rangeMs <= 3 * day) return "15m";
    if (rangeMs <= 7 * day) return "1h";
    return "4h";
  }

  /** 將 ms 範圍化為 "yyyy-MM-dd HH:mm" 字串 */
  function formatRange(fromMs, toMs) {
    const f = new Date(fromMs);
    const t = new Date(toMs);
    const pad = (n) => String(n).padStart(2, "0");
    const fmt = (d) =>
      `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
      `${pad(d.getHours())}:${pad(d.getMinutes())}`;
    return `${fmt(f)} → ${fmt(t)}`;
  }

  /** Date → "yyyy-MM-ddTHH:mm" (本地時區,給 datetime-local) */
  function toLocalInput(ms) {
    const d = new Date(ms);
    const pad = (n) => String(n).padStart(2, "0");
    return (
      `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T` +
      `${pad(d.getHours())}:${pad(d.getMinutes())}`
    );
  }

  /** "yyyy-MM-ddTHH:mm" → ms (本地時區) */
  function fromLocalInput(str) {
    if (!str) return null;
    const d = new Date(str);
    if (isNaN(d.getTime())) return null;
    return d.getTime();
  }

  /**
   * 將 state 滑動到「現在」(sliding window)。
   * - preset (1h/6h/24h/7d/30d):to 設為 Date.now(),from 重新算為 to - span,
   *   granularity / label 同步重算。
   * - custom 模式:保持原樣不動,讓使用者自己決定的時點持續生效。
   * - 找不到對應 preset (例如 state.key 是未知字串) 時,維持原 state。
   */
  function slideRange(state) {
    if (!state || state.key === "custom") return state;
    const span = PRESETS[state.key];
    if (!span) return state;
    const to = Date.now();
    const from = to - span;
    return {
      key: state.key,
      from,
      to,
      granularity: pickGranularity(span),
      label: formatRange(from, to),
    };
  }

  // 對外暴露 slideRange 供其他腳本 (events.js 等) 在自動更新週期內使用
  window.__netmonRangeSlide = slideRange;

  function emit(state) {
    const ev = new CustomEvent("netmon:rangechange", { detail: state });
    window.dispatchEvent(ev);
  }

  function init(pageKey) {
    const root = document.body;
    const chips = root.querySelectorAll(".chip[data-range]");
    const customBox = document.getElementById("range-custom");
    const fromInput = document.getElementById("range-from");
    const toInput = document.getElementById("range-to");
    const applyBtn = document.getElementById("range-apply");
    const currentLabel = document.getElementById("range-current");
    if (!chips.length) return;

    // 從 query string 讀初始值
    const params = new URLSearchParams(window.location.search);
    const storageKey = `netmon:range:${pageKey}`;

    /** @type {{key:string, from:number, to:number, granularity:string, label:string}} */
    let state;
    const urlRange = params.get("range");
    const urlFrom = params.get("from");
    const urlTo = params.get("to");

    if (urlFrom && urlTo) {
      const from = parseInt(urlFrom, 10);
      const to = parseInt(urlTo, 10);
      if (!isNaN(from) && !isNaN(to) && from < to) {
        state = makeCustomState(from, to);
        // 自訂模式必須展開
        showCustom(from, to);
        setChipActive("custom");
      } else {
        state = makePresetState(urlRange && PRESETS[urlRange] ? urlRange : "24h");
      }
    } else {
      const initRange = urlRange || "24h";
      if (PRESETS[initRange]) {
        state = makePresetState(initRange);
      } else {
        state = makePresetState("24h");
      }
    }

    function makePresetState(key) {
      const span = PRESETS[key];
      const to = Date.now();
      const from = to - span;
      return {
        key,
        from,
        to,
        granularity: pickGranularity(span),
        label: formatRange(from, to),
      };
    }

    function makeCustomState(from, to) {
      return {
        key: "custom",
        from,
        to,
        granularity: pickGranularity(to - from),
        label: formatRange(from, to),
      };
    }

    function setChipActive(key) {
      chips.forEach((c) => {
        c.setAttribute("aria-pressed", c.dataset.range === key ? "true" : "false");
      });
    }

    function showCustom(from, to) {
      if (!customBox) return;
      customBox.hidden = false;
      if (fromInput) fromInput.value = from ? toLocalInput(from) : "";
      if (toInput) toInput.value = to ? toLocalInput(to) : "";
    }

    function hideCustom() {
      if (customBox) customBox.hidden = true;
    }

    function updateLabel() {
      if (currentLabel) currentLabel.textContent = state.label;
    }

    function syncUrl(replace) {
      const u = new URL(window.location.href);
      u.searchParams.set("range", state.key);
      if (state.key === "custom") {
        u.searchParams.set("from", String(state.from));
        u.searchParams.set("to", String(state.to));
      } else {
        u.searchParams.delete("from");
        u.searchParams.delete("to");
      }
      const method = replace ? "replaceState" : "pushState";
      window.history[method]({}, "", u);
    }

    function apply(newState, opts) {
      state = newState;
      updateLabel();
      try {
        sessionStorage.setItem(storageKey, JSON.stringify(state));
      } catch (_) {}
      syncUrl(!!(opts && opts.replace));
      emit(state);
    }

    // 註冊 chip 點擊
    chips.forEach((chip) => {
      chip.addEventListener("click", () => {
        const key = chip.dataset.range;
        if (key === "custom") {
          const now = Date.now();
          const fromDefault = now - 24 * 60 * 60 * 1000;
          showCustom(fromDefault, now);
          setChipActive("custom");
          // 不立即套用,等使用者按 "套用"
          return;
        }
        hideCustom();
        setChipActive(key);
        apply(makePresetState(key), { replace: false });
      });
    });

    if (applyBtn) {
      applyBtn.addEventListener("click", () => {
        const from = fromLocalInput(fromInput && fromInput.value);
        const to = fromLocalInput(toInput && toInput.value);
        if (from == null || to == null || from >= to) {
          alert("請輸入有效的日期區間 (從需早於到)");
          return;
        }
        setChipActive("custom");
        apply(makeCustomState(from, to), { replace: false });
      });
    }

    // 初次繪製
    updateLabel();
    setChipActive(state.key);
    if (state.key === "custom") showCustom(state.from, state.to);

    // 對外暴露狀態 (供其他腳本首次載入使用)
    window.__netmonRange = state;

    // 等下一個 microtask 再 emit,讓子頁面的監聽器先註冊
    Promise.resolve().then(() => emit(state));
  }

  // 自動 init
  document.addEventListener("DOMContentLoaded", () => {
    const page = document.body.dataset.page || "default";
    init(page);
  });
})();
