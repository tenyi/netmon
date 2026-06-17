package web

import (
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tenyi/netmon/internal/storage"
)

type handler struct {
	deps Deps
	tmpl *template.Template
}

func (h *handler) dashboard(c *gin.Context) {
	h.render(c, "dashboard.html", gin.H{
		"Title":     "Dashboard",
		"ActiveNav": "dashboard",
	})
}

func (h *handler) eventsPage(c *gin.Context) {
	h.render(c, "events.html", gin.H{
		"Title":     "事件歷史",
		"ActiveNav": "events",
	})
}

func (h *handler) render(c *gin.Context, name string, data gin.H) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(c.Writer, name, data); err != nil {
		c.String(http.StatusInternalServerError, "渲染頁面失敗")
	}
}

func (h *handler) apiStatus(c *gin.Context) {
	c.JSON(http.StatusOK, h.deps.Status.Status())
}

// eventsPageSize 上限,避免惡意 query 拉走過多資料。
const eventsPageSizeMax = 200

func (h *handler) apiEvents(c *gin.Context) {
	from, to, err := parseTimeRange(c, 24*time.Hour)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	limit, offset, err := parsePagination(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if limit > 0 {
		events, err := h.deps.Events.ListPage(ctx, from, to, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查詢事件失敗"})
			return
		}
		total, err := h.deps.Events.Count(ctx, from, to)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查詢事件總數失敗"})
			return
		}
		c.Header("X-Total-Count", strconv.FormatInt(total, 10))
		c.JSON(http.StatusOK, events)
		return
	}

	// 未指定 limit 時,回傳全部事件 (dashboard KPI 用)
	events, err := h.deps.Events.List(ctx, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查詢事件失敗"})
		return
	}
	c.JSON(http.StatusOK, events)
}

// parsePagination 解析 limit 與 offset 查詢參數。
// limit 為 0 或未指定時表示不分頁 (回傳全部);
// limit 上限為 eventsPageSizeMax,避免單次回應過大。
func parsePagination(c *gin.Context) (int, int, error) {
	limit := 0
	offset := 0
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, 0, errInvalidParam("limit")
		}
		if n > eventsPageSizeMax {
			n = eventsPageSizeMax
		}
		limit = n
	}
	if v := c.Query("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, 0, errInvalidParam("offset")
		}
		offset = n
	}
	return limit, offset, nil
}

func (h *handler) apiStats(c *gin.Context) {
	from, to, err := parseTimeRange(c, time.Hour)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stats, err := h.deps.Stats.List(c.Request.Context(), from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查詢統計失敗"})
		return
	}

	granularity := c.Query("granularity")
	if granularity != "" {
		d, err := time.ParseDuration(granularity)
		if err != nil || d <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "granularity 格式無效"})
			return
		}
		stats = aggregateStats(stats, d)
	}

	c.JSON(http.StatusOK, stats)
}

func parseTimeRange(c *gin.Context, defaultRange time.Duration) (int64, int64, error) {
	now := time.Now().UnixMilli()
	from := now - defaultRange.Milliseconds()
	to := now

	if v := c.Query("from"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, 0, errInvalidParam("from")
		}
		from = n
	}
	if v := c.Query("to"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, 0, errInvalidParam("to")
		}
		to = n
	}
	if from > to {
		return 0, 0, errInvalidParam("from/to")
	}
	return from, to, nil
}

type paramError string

func (e paramError) Error() string {
	return "參數 " + string(e) + " 無效"
}

func errInvalidParam(name string) error {
	return paramError(name)
}

func aggregateStats(stats []storage.Stat, granularity time.Duration) []storage.Stat {
	if len(stats) == 0 {
		return stats
	}

	granMs := granularity.Milliseconds()
	if granMs <= 0 {
		return stats
	}

	type bucket struct {
		start        int64
		latencySum   float64
		lossWeighted float64
		samples      int
	}

	buckets := make(map[int64]*bucket)
	order := make([]int64, 0)

	for _, s := range stats {
		key := (s.BucketStart / granMs) * granMs
		b, ok := buckets[key]
		if !ok {
			b = &bucket{start: key}
			buckets[key] = b
			order = append(order, key)
		}
		b.latencySum += s.LatencyAvgMs * float64(s.SampleCount)
		b.lossWeighted += s.LossPct * float64(s.SampleCount)
		b.samples += s.SampleCount
	}

	out := make([]storage.Stat, 0, len(order))
	for _, key := range order {
		b := buckets[key]
		avgLatency := 0.0
		lossPct := 0.0
		if b.samples > 0 {
			avgLatency = b.latencySum / float64(b.samples)
			lossPct = b.lossWeighted / float64(b.samples)
		}
		out = append(out, storage.Stat{
			BucketStart:  b.start,
			LatencyAvgMs: avgLatency,
			LossPct:      lossPct,
			SampleCount:  b.samples,
		})
	}
	return out
}
