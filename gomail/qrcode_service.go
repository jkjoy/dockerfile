package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	goqrcode "github.com/skip2/go-qrcode"
)

const (
	defaultQRCodeSize       = 256
	minQRCodeSize           = 64
	maxQRCodeSize           = 2048
	maxQRCodeContentLength  = 4096
	defaultQRCodeFormat     = "png"
	defaultQRCodeForeground = "#000000"
	defaultQRCodeBackground = "#FFFFFF"
)

type qrCodeRequest struct {
	Content string `json:"content" form:"content"`
	Size    int    `json:"size" form:"size"`
	Format  string `json:"format" form:"format"`
	FgColor string `json:"fg_color" form:"fg_color"`
	BgColor string `json:"bg_color" form:"bg_color"`
}

type qrCodeImage struct {
	Data        []byte
	ContentType string
	Filename    string
}

type qrCodeCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]qrCodeCacheItem
}

type qrCodeCacheItem struct {
	data       []byte
	expiration time.Time
}

func newQRCodeCache(ttl time.Duration) *qrCodeCache {
	return &qrCodeCache{
		ttl:   ttl,
		items: make(map[string]qrCodeCacheItem),
	}
}

func (c *qrCodeCache) get(key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	item, found := c.items[key]
	if !found {
		return nil, false
	}
	if time.Now().After(item.expiration) {
		delete(c.items, key)
		return nil, false
	}
	return item.data, true
}

func (c *qrCodeCache) set(key string, data []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = qrCodeCacheItem{
		data:       data,
		expiration: time.Now().Add(c.ttl),
	}
}

func decodeQRCodeRequest(r *http.Request) (qrCodeRequest, error) {
	if r.Method == http.MethodGet {
		return normalizeQRCodeRequest(qrCodeRequestFromValues(r.URL.Query()))
	}

	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		var req qrCodeRequest
		if err := decodeJSON(r, &req); err != nil {
			return qrCodeRequest{}, err
		}
		return normalizeQRCodeRequest(req)
	}

	if err := r.ParseForm(); err != nil {
		return qrCodeRequest{}, err
	}
	return normalizeQRCodeRequest(qrCodeRequestFromValues(r.Form))
}

func qrCodeRequestFromValues(values url.Values) qrCodeRequest {
	size, _ := strconv.Atoi(strings.TrimSpace(values.Get("size")))
	return qrCodeRequest{
		Content: values.Get("content"),
		Size:    size,
		Format:  values.Get("format"),
		FgColor: values.Get("fg_color"),
		BgColor: values.Get("bg_color"),
	}
}

func normalizeQRCodeRequest(req qrCodeRequest) (qrCodeRequest, error) {
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		return qrCodeRequest{}, errors.New("content is required")
	}
	if len(req.Content) > maxQRCodeContentLength {
		return qrCodeRequest{}, fmt.Errorf("content is too long; max is %d bytes", maxQRCodeContentLength)
	}

	if req.Size == 0 {
		req.Size = defaultQRCodeSize
	}
	if req.Size < minQRCodeSize || req.Size > maxQRCodeSize {
		return qrCodeRequest{}, fmt.Errorf("size must be between %d and %d", minQRCodeSize, maxQRCodeSize)
	}

	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if req.Format == "" {
		req.Format = defaultQRCodeFormat
	}
	if req.Format != "png" && req.Format != "svg" {
		return qrCodeRequest{}, errors.New("format must be one of: png, svg")
	}

	var err error
	req.FgColor, _, err = normalizeHexColor(req.FgColor, defaultQRCodeForeground)
	if err != nil {
		return qrCodeRequest{}, fmt.Errorf("fg_color: %w", err)
	}
	req.BgColor, _, err = normalizeHexColor(req.BgColor, defaultQRCodeBackground)
	if err != nil {
		return qrCodeRequest{}, fmt.Errorf("bg_color: %w", err)
	}

	return req, nil
}

func renderQRCode(req qrCodeRequest) (qrCodeImage, error) {
	fgHex, fgColor, err := normalizeHexColor(req.FgColor, defaultQRCodeForeground)
	if err != nil {
		return qrCodeImage{}, err
	}
	bgHex, bgColor, err := normalizeHexColor(req.BgColor, defaultQRCodeBackground)
	if err != nil {
		return qrCodeImage{}, err
	}

	qr, err := goqrcode.New(req.Content, goqrcode.Medium)
	if err != nil {
		return qrCodeImage{}, err
	}
	qr.DisableBorder = true
	qr.ForegroundColor = fgColor
	qr.BackgroundColor = bgColor

	if req.Format == "svg" {
		return qrCodeImage{
			Data:        []byte(generateQRCodeSVG(qr, req.Size, fgHex, bgHex)),
			ContentType: "image/svg+xml",
			Filename:    "qrcode.svg",
		}, nil
	}

	data, err := qr.PNG(req.Size)
	if err != nil {
		return qrCodeImage{}, err
	}
	return qrCodeImage{
		Data:        data,
		ContentType: "image/png",
		Filename:    "qrcode.png",
	}, nil
}

func normalizeHexColor(value string, fallback string) (string, color.RGBA, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return "", color.RGBA{}, errors.New("must be a 6-digit hex color")
	}

	r, err := strconv.ParseUint(value[0:2], 16, 8)
	if err != nil {
		return "", color.RGBA{}, errors.New("must be a valid hex color")
	}
	g, err := strconv.ParseUint(value[2:4], 16, 8)
	if err != nil {
		return "", color.RGBA{}, errors.New("must be a valid hex color")
	}
	b, err := strconv.ParseUint(value[4:6], 16, 8)
	if err != nil {
		return "", color.RGBA{}, errors.New("must be a valid hex color")
	}

	normalized := "#" + strings.ToUpper(value)
	return normalized, color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, nil
}

func qrCodeCacheKey(req qrCodeRequest) string {
	return fmt.Sprintf("%s|%d|%s|%s|%s", req.Content, req.Size, req.Format, req.FgColor, req.BgColor)
}

func generateQRCodeSVG(qr *goqrcode.QRCode, size int, fgColor, bgColor string) string {
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
