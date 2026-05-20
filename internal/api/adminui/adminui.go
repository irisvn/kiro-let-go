package adminui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed dist/*
var assets embed.FS

func RegisterRoutes(r *gin.Engine) {
	distSub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic("adminui: failed to create sub filesystem: " + err.Error())
	}

	indexHTML, err := fs.ReadFile(distSub, "index.html")
	if err != nil {
		panic("adminui: index.html not found in dist: " + err.Error())
	}

	fileServer := http.StripPrefix("/admin/ui/", http.FileServer(http.FS(distSub)))

	handler := func(c *gin.Context) {
		path := c.Request.URL.Path
		relPath := strings.TrimPrefix(path, "/admin/ui/")

		if relPath != "" && relPath != "/" {
			if f, err := distSub.Open(relPath); err == nil {
				f.Close()
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}

		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	}

	r.GET("/admin/ui", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/admin/ui/")
	})

	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/admin/ui/") {
			handler(c)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})
}
