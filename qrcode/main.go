package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/skip2/go-qrcode"
	"golang.org/x/exp/slog"
)

// 缓存项结构
type cacheItem struct {
	data       []byte
	expiration time.Time
}

// 全局缓存
var (
	cache      = make(map[string]cacheItem)
	cacheMutex sync.RWMutex
	cacheTTL   = 10 * time.Minute // 缓存有效期
)

// 请求结构
type qrRequest struct {
	Content string `json:"content" form:"content" binding:"required"`
	Size    int    `json:"size" form:"size"`
	Format  string `json:"format" form:"format"`
}

func main() {
	// 初始化日志
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// 启动缓存清理协程
	go cleanupCache()

	r := gin.Default()

	// 添加日志中间件
	r.Use(loggingMiddleware())

	// 定义API路由
	r.GET("/qrcode", generateQRCode)
	r.POST("/qrcode", generateQRCode)

	// 启动服务器
	slog.Info("QR Code API is running on :8080")
	if err := r.Run(":8080"); err != nil {
		slog.Error("Failed to start server", "error", err)
		log.Fatal(err)
	}
}

// 日志中间件
func loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// 处理请求
		c.Next()

		// 记录日志
		end := time.Now()
		latency := end.Sub(start)
		if raw != "" {
			path = path + "?" + raw
		}

		slog.Info("request",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"ip", c.ClientIP(),
			"latency", latency,
		)
	}
}

// 清理过期缓存
func cleanupCache() {
	for {
		time.Sleep(time.Hour)
		now := time.Now()
		cacheMutex.Lock()
		for key, item := range cache {
			if now.After(item.expiration) {
				delete(cache, key)
			}
		}
		cacheMutex.Unlock()
	}
}

// 从缓存获取
func getFromCache(key string) ([]byte, bool) {
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()
	item, found := cache[key]
	if !found || time.Now().After(item.expiration) {
		return nil, false
	}
	return item.data, true
}

// 存入缓存
func setToCache(key string, data []byte) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	cache[key] = cacheItem{
		data:       data,
		expiration: time.Now().Add(cacheTTL),
	}
}

// 生成缓存键
func generateCacheKey(content string, size int, format string) string {
	return fmt.Sprintf("%s|%d|%s", content, size, format)
}

// 生成二维码
func generateQRCode(c *gin.Context) {
	var req qrRequest
	//	var err error // 这里声明了err但后面没有使用

	// 根据请求方法解析参数
	if c.Request.Method == http.MethodGet {
		req.Content = c.Query("content")
		if req.Content == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "content parameter is required"})
			return
		}
		req.Size, _ = strconv.Atoi(c.DefaultQuery("size", "256"))
		req.Format = c.DefaultQuery("format", "png")
	} else {
		// 这里使用 := 重新声明err，不需要之前的声明
		if err := c.ShouldBind(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
			return
		}
	}

	// 参数验证
	if req.Size <= 0 {
		req.Size = 256
	}
	if req.Format != "png" && req.Format != "svg" {
		req.Format = "png"
	}

	// 检查缓存
	cacheKey := generateCacheKey(req.Content, req.Size, req.Format)
	if cachedData, found := getFromCache(cacheKey); found {
		slog.Debug("serving from cache", "key", cacheKey)
		sendResponse(c, req.Format, cachedData)
		return
	}

	// 生成二维码
	var output []byte
	if req.Format == "svg" {
		// SVG格式 - 使用简单的SVG生成
		qr, err := qrcode.New(req.Content, qrcode.Medium)
		if err != nil {
			slog.Error("failed to generate QR code", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate QR code"})
			return
		}
		output = []byte(generateSimpleSVG(qr, req.Size))
	} else {
		// PNG格式
		qr, err := qrcode.New(req.Content, qrcode.Medium)
		if err != nil {
			slog.Error("failed to generate QR code", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate QR code"})
			return
		}
		qr.DisableBorder = true
		output, err = qr.PNG(req.Size)
		if err != nil {
			slog.Error("failed to generate PNG QR code", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate PNG QR code"})
			return
		}
	}

	// 存入缓存
	setToCache(cacheKey, output)

	// 发送响应
	sendResponse(c, req.Format, output)
}

// 生成简单的SVG
func generateSimpleSVG(qr *qrcode.QRCode, size int) string {
	var buf bytes.Buffer
	buf.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 29 29" width="`)
	buf.WriteString(strconv.Itoa(size))
	buf.WriteString(`" height="`)
	buf.WriteString(strconv.Itoa(size))
	buf.WriteString(`">`)
	buf.WriteString(`<rect width="29" height="29" fill="#FFFFFF"/>`)

	for y := 0; y < len(qr.Bitmap()); y++ {
		for x := 0; x < len(qr.Bitmap()[y]); x++ {
			if qr.Bitmap()[y][x] {
				buf.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="1" height="1" fill="#000000"/>`, x, y))
			}
		}
	}

	buf.WriteString(`</svg>`)
	return buf.String()
}

// 发送响应
func sendResponse(c *gin.Context, format string, data []byte) {
	if format == "svg" {
		c.Data(http.StatusOK, "image/svg+xml", data)
	} else {
		c.JSON(http.StatusOK, gin.H{
			"qrcode": base64.StdEncoding.EncodeToString(data),
			"format": format,
		})
	}
}
