// Package entry 提供前端静态资源嵌入与托管。
//
// Vite 构建产物位于 ./dist（见 web/vite.config.ts 的 outDir），
// 通过 //go:embed 将整个目录嵌入二进制，启动时由 ServeFrontend 托管。
//
// 路由策略：
//   - /assets/*         静态资源（JS/CSS/图片），长缓存
//   - /index.html        入口 HTML
//   - 其余非 API 路径    返回 index.html（SPA 前端路由由 React 处理）
package entry

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed dist/*
var distFS embed.FS

// ServeFrontend 注册前端静态资源 + SPA fallback。
// 必须在 /v1、/api 等业务路由之后注册（兜底）。
func ServeFrontend(r *gin.Engine) {
	// 取出 dist 子目录
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// 编译时若 dist 不存在，//go:embed 会在构建期报错，理论上不会走到这里
		r.NoRoute(func(c *gin.Context) {
			c.String(http.StatusNotFound, "前端资源未构建：请在 web/ 目录执行 npm run build")
		})
		return
	}

	// 静态资源路由（assets 目录 + favicon 等）
	r.GET("/assets/*filepath", func(c *gin.Context) {
		http.FileServer(http.FS(sub)).ServeHTTP(c.Writer, c.Request)
	})

	// 根路径返回 index.html
	r.GET("/", func(c *gin.Context) {
		serveIndex(c, sub)
	})

	// 兜底：未匹配 API 的路径回退到 SPA index.html
	r.NoRoute(func(c *gin.Context) {
		// 不回退 API 路径（/v1、/api 前缀）
		p := c.Request.URL.Path
		if len(p) >= 4 && (p[:4] == "/v1/" || p[:4] == "/api" || p == "/v1") {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "endpoint not found"}})
			return
		}
		serveIndex(c, sub)
	})
}

// serveIndex 读取并返回 index.html。
func serveIndex(c *gin.Context, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		c.String(http.StatusNotFound, "index.html 未找到")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
