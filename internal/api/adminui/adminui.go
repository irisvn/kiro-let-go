package adminui

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed templates/* static/*
var assets embed.FS

// RegisterRoutes adds admin UI asset routes to the given gin engine.
// These routes are intentionally public (no auth middleware) so the
// login page can render before the user has provided an API key.
func RegisterRoutes(r *gin.Engine) {
	r.GET("/admin/ui", serveIndex)
	r.GET("/admin/ui/", serveIndex)

	staticSub, err := fs.Sub(assets, "static")
	if err != nil {
		panic("adminui: failed to create sub filesystem: " + err.Error())
	}
	staticServer := http.StripPrefix("/admin/ui/static", http.FileServer(http.FS(staticSub)))
	r.GET("/admin/ui/static/*filepath", func(c *gin.Context) {
		staticServer.ServeHTTP(c.Writer, c.Request)
	})
}

func serveIndex(c *gin.Context) {
	data, err := assets.ReadFile("templates/index.html")
	if err != nil {
		c.String(http.StatusInternalServerError, "admin UI template not found")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}