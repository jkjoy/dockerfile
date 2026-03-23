package main

import (
	"bytes"
	"fmt"
	"image/color"
	"log"
	"net/http"
	"strings"
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
	cacheTTL   = 10 * time.Minute
)

// 请求结构
type qrRequest struct {
	Content string `json:"content" form:"content"`
	Size    int    `json:"size" form:"size"`
	Format  string `json:"format" form:"format"`
	FgColor string `json:"fg_color" form:"fg_color"` // 前景色，如 #000000
	BgColor string `json:"bg_color" form:"bg_color"` // 背景色，如 #FFFFFF
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	go cleanupCache()

	r := gin.Default()
	r.Use(loggingMiddleware())

	r.StaticFile("/", "./index.html")
	r.GET("/qrcode", generateQRCode)
	r.POST("/qrcode", generateQRCode)

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

		c.Next()

		latency := time.Since(start)
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

func getFromCache(key string) ([]byte, bool) {
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()
	item, found := cache[key]
	if !found || time.Now().After(item.expiration) {
		return nil, false
	}
	return item.data, true
}

func setToCache(key string, data []byte) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	cache[key] = cacheItem{
		data:       data,
		expiration: time.Now().Add(cacheTTL),
	}
}

func generateCacheKey(content string, size int, format, fgColor, bgColor string) string {
	return fmt.Sprintf("%s|%d|%s|%s|%s", content, size, format, fgColor, bgColor)
}

// 解析十六进制颜色，如 #FF0000 或 FF0000
func parseHexColor(hex string) (color.RGBA, error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return color.RGBA{}, fmt.Errorf("invalid color: %s", hex)
	}
	r, err := strconv.ParseUint(hex[0:2], 16, 8)
	if err != nil {
		return color.RGBA{}, err
	}
	g, err := strconv.ParseUint(hex[2:4], 16, 8)
	if err != nil {
		return color.RGBA{}, err
	}
	b, err := strconv.ParseUint(hex[4:6], 16, 8)
	if err != nil {
		return color.RGBA{}, err
	}
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, nil
}

// 生成二维码
func generateQRCode(c *gin.Context) {
	var req qrRequest

	if c.Request.Method == http.MethodGet {
		req.Content = c.Query("content")
		if req.Content == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "content parameter is required"})
			return
		}
		req.Size, _ = strconv.Atoi(c.DefaultQuery("size", "256"))
		req.Format = c.DefaultQuery("format", "png")
		req.FgColor = c.DefaultQuery("fg_color", "#000000")
		req.BgColor = c.DefaultQuery("bg_color", "#FFFFFF")
	} else {
		if err := c.ShouldBind(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
			return
		}
		if req.Content == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "content parameter is required"})
			return
		}
	}

	if req.Size <= 0 {
		req.Size = 256
	}
	if req.Format != "png" && req.Format != "svg" {
		req.Format = "png"
	}
	if req.FgColor == "" {
		req.FgColor = "#000000"
	}
	if req.BgColor == "" {
		req.BgColor = "#FFFFFF"
	}

	// 解析颜色
	fgColor, err := parseHexColor(req.FgColor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid fg_color: " + err.Error()})
		return
	}
	bgColor, err := parseHexColor(req.BgColor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid bg_color: " + err.Error()})
		return
	}

	// 检查缓存
	cacheKey := generateCacheKey(req.Content, req.Size, req.Format, req.FgColor, req.BgColor)
	if cachedData, found := getFromCache(cacheKey); found {
		slog.Debug("serving from cache", "key", cacheKey)
		sendResponse(c, req.Format, cachedData)
		return
	}

	// 生成二维码
	qr, err := qrcode.New(req.Content, qrcode.Medium)
	if err != nil {
		slog.Error("failed to generate QR code", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate QR code"})
		return
	}
	qr.DisableBorder = true
	qr.ForegroundColor = fgColor
	qr.BackgroundColor = bgColor

	var output []byte
	if req.Format == "svg" {
		output = []byte(generateSimpleSVG(qr, req.Size, req.FgColor, req.BgColor))
	} else {
		output, err = qr.PNG(req.Size)
		if err != nil {
			slog.Error("failed to generate PNG", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate PNG"})
			return
		}
	}

	setToCache(cacheKey, output)
	sendResponse(c, req.Format, output)
}

// 生成SVG
func generateSimpleSVG(qr *qrcode.QRCode, size int, fgColor, bgColor string) string {
	bitmap := qr.Bitmap()
	modules := len(bitmap)

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`, modules, modules, size, size))
	buf.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="%s"/>`, modules, modules, bgColor))

	for y := 0; y < modules; y++ {
		for x := 0; x < modules; x++ {
			if bitmap[y][x] {
				buf.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="1" height="1" fill="%s"/>`, x, y, fgColor))
			}
		}
	}

	buf.WriteString(`</svg>`)
	return buf.String()
}

// 发送响应 - 直接返回图片文件
func sendResponse(c *gin.Context, format string, data []byte) {
	if format == "svg" {
		c.Header("Content-Disposition", "inline; filename=qrcode.svg")
		c.Data(http.StatusOK, "image/svg+xml", data)
	} else {
		c.Header("Content-Disposition", "inline; filename=qrcode.png")
		c.Data(http.StatusOK, "image/png", data)
	}
}
