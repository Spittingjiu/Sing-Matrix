package api

import (
	"embed"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed web/*
var webFiles embed.FS

type pageData struct {
	Title string
}

var appTemplate = template.Must(template.ParseFS(webFiles, "web/index.html"))

func MountStatic(r *gin.Engine) {
	assets := http.FileServer(http.FS(webFiles))
	r.GET("/", serveApp)
	r.GET("/login", serveApp)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "api route not found"})
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/web/") {
			assets.ServeHTTP(c.Writer, c.Request)
			return
		}
		serveApp(c)
	})
}

func serveApp(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := appTemplate.Execute(c.Writer, pageData{Title: "S-Matrix"}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
