package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tenyi/netmon/internal/config"
	"github.com/tenyi/netmon/internal/monitor"
	"github.com/tenyi/netmon/internal/storage"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Deps 為 web 層所需依賴。
type Deps struct {
	Config *config.Config
	Events *storage.EventRepo
	Stats  *storage.StatsRepo
	Status monitor.StatusProvider
}

// New 建立 Gin engine 並註冊路由。
func New(deps Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	tmpl := template.Must(template.ParseFS(templateFS, "templates/*.html"))
	staticSub, _ := fs.Sub(staticFS, "static")

	h := &handler{deps: deps, tmpl: tmpl}

	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())

	r.GET("/", h.dashboard)
	r.GET("/events", h.eventsPage)
	r.GET("/api/status", h.apiStatus)
	r.GET("/api/events", h.apiEvents)
	r.GET("/api/stats", h.apiStats)
	r.StaticFS("/static", http.FS(staticSub))

	return r
}
