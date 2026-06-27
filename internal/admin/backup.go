package admin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// --- 数据库备份（v0.8）---
//
// 备份服务由 main.go 装配后经 SetBackup 注入；未注入时端点返回 503。
// 备份机制见 internal/backup：VACUUM INTO 事务一致快照 + retention 轮转。

func (h *Handler) listBackups(c *gin.Context) {
	if h.bk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "备份服务未启用"})
		return
	}
	list, err := h.bk.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// createBackup 立即执行一次备份，返回新备份文件名。
func (h *Handler) createBackup(c *gin.Context) {
	if h.bk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "备份服务未启用"})
		return
	}
	name, err := h.bk.BackupOnce(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "name": name})
}

// downloadBackup 以附件形式下载备份文件（application/octet-stream）。
// :file 经 Path 白名单校验，防路径穿越。
func (h *Handler) downloadBackup(c *gin.Context) {
	if h.bk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "备份服务未启用"})
		return
	}
	name := c.Param("file")
	p, err := h.bk.Path(name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Content-Disposition 附件下载；文件名用 URL 安全的原名。
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Content-Type", "application/octet-stream")
	c.File(p)
}

// deleteBackup 删除指定备份文件。
func (h *Handler) deleteBackup(c *gin.Context) {
	if h.bk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "备份服务未启用"})
		return
	}
	name := c.Param("file")
	if err := h.bk.Delete(name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// 防止 strconv 未使用告警（保留给未来分页 limit 解析）
var _ = strconv.Atoi
