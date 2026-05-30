package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goadminconfig "github.com/GoAdminGroup/go-admin/modules/config"
)

func TestBuildMailMessageDoesNotExposeBcc(t *testing.T) {
	cfg := smtpConfig{
		FromEmail:  "sender@example.com",
		FromName:   "Gomail",
		Encryption: "starttls",
	}
	req := sendMailRequest{
		To:      []string{"to@example.com"},
		Cc:      []string{"cc@example.com"},
		Bcc:     []string{"hidden@example.com"},
		Subject: "Hello",
		Text:    "plain body",
		HTML:    "<p>html body</p>",
	}

	message, recipients, err := buildMailMessage(cfg, req, time.Date(2026, 5, 29, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatalf("buildMailMessage() error = %v", err)
	}

	if len(recipients) != 3 {
		t.Fatalf("recipients len = %d, want 3", len(recipients))
	}

	raw := string(message)
	for _, want := range []string{
		`From: "Gomail" <sender@example.com>`,
		"To: <to@example.com>",
		"Cc: <cc@example.com>",
		"Content-Type: multipart/alternative;",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("message missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "Bcc:") || strings.Contains(raw, "hidden@example.com") {
		t.Fatalf("message exposes bcc recipient:\n%s", raw)
	}
}

func TestPutSMTPPreservesPasswordWhenOmitted(t *testing.T) {
	a := newTestApp(t)

	putJSON(t, a, "/api/admin/smtp", `{
		"host":"smtp.example.com",
		"port":587,
		"username":"sender@example.com",
		"password":"first-secret",
		"from_email":"sender@example.com",
		"from_name":"Gomail",
		"encryption":"starttls"
	}`, http.StatusOK)

	putJSON(t, a, "/api/admin/smtp", `{
		"host":"smtp.example.com",
		"port":587,
		"username":"new-user@example.com",
		"from_email":"sender@example.com",
		"from_name":"Gomail",
		"encryption":"starttls"
	}`, http.StatusOK)

	cfg, _, ok, err := a.getSMTPConfig(context.Background())
	if err != nil {
		t.Fatalf("getSMTPConfig() error = %v", err)
	}
	if !ok {
		t.Fatal("smtp config not found")
	}
	if cfg.Password != "first-secret" {
		t.Fatalf("password = %q, want preserved secret", cfg.Password)
	}
	if cfg.Username != "new-user@example.com" {
		t.Fatalf("username = %q, want updated username", cfg.Username)
	}
}

func TestCreateAPIKeyCanAuthorizeSendPermission(t *testing.T) {
	a := newTestApp(t)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", strings.NewReader(`{"name":"test-client"}`))
	req.Header.Set("Authorization", "Bearer test-admin")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	a.routes().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}

	var payload createAPIKeyResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Key == "" {
		t.Fatal("created api key is empty")
	}

	protected := a.requirePermission(sendMailPerm, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	sendReq := httptest.NewRequest(http.MethodPost, "/protected", nil)
	sendReq.Header.Set("Authorization", "Bearer "+payload.Key)
	sendRes := httptest.NewRecorder()

	protected(sendRes, sendReq)
	if sendRes.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", sendRes.Code, sendRes.Body.String())
	}
}

func TestAdminQRCodeGeneratesPNG(t *testing.T) {
	a := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/qrcode?content=hello&size=128&format=png", nil)
	req.Header.Set("Authorization", "Bearer test-admin")
	res := httptest.NewRecorder()

	a.routes().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q, want image/png", got)
	}
	if !bytes.HasPrefix(res.Body.Bytes(), []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("response does not look like a PNG")
	}
}

func TestHomePageDocumentsAPIs(t *testing.T) {
	a := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "docs.example.test"
	res := httptest.NewRecorder()

	a.routes().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("content-type = %q, want text/html", got)
	}
	body := res.Body.String()
	for _, want := range []string{
		"Gomail Suite API 调用文档",
		"http://docs.example.test",
		"POST /api/v1/mail/send",
		"GET /api/v1/ip/{ip}",
		"GET /api/v1/qrcode",
		"Authorization: Bearer",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("home page missing %q", want)
		}
	}
}

func TestIPLookupUnavailableWhenNotConfigured(t *testing.T) {
	a := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ip/lookup?ip=8.8.8.8", nil)
	req.Header.Set("Authorization", "Bearer test-admin")
	res := httptest.NewRecorder()

	a.routes().ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d, body = %s", res.Code, http.StatusServiceUnavailable, res.Body.String())
	}
}

func TestUtilityPermissionsAreSupported(t *testing.T) {
	if err := validatePermissions([]string{sendMailPerm, ipLookupPerm, qrcodeGeneratePerm}); err != nil {
		t.Fatalf("validatePermissions() error = %v", err)
	}
	if err := validatePermissions([]string{"unknown"}); err == nil {
		t.Fatal("validatePermissions() expected unsupported permission error")
	}
}

func TestMigrateGoAdminSeedsAdminAndMenus(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "goadmin.db"))
	if err != nil {
		t.Fatalf("openDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrateGoAdmin(context.Background(), db, "admin", "secret"); err != nil {
		t.Fatalf("migrateGoAdmin() error = %v", err)
	}

	var userCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goadmin_users WHERE username = 'admin'`).Scan(&userCount); err != nil {
		t.Fatalf("query user count: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("user count = %d, want 1", userCount)
	}

	var menuCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goadmin_menu WHERE title = 'Gomail'`).Scan(&menuCount); err != nil {
		t.Fatalf("query menu count: %v", err)
	}
	if menuCount != 1 {
		t.Fatalf("menu count = %d, want 1", menuCount)
	}

	var toolMenuCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goadmin_menu WHERE title IN ('IP Lookup', 'QR Code')`).Scan(&toolMenuCount); err != nil {
		t.Fatalf("query tool menu count: %v", err)
	}
	if toolMenuCount != 2 {
		t.Fatalf("tool menu count = %d, want 2", toolMenuCount)
	}

	var smtpURI string
	if err := db.QueryRow(`SELECT uri FROM goadmin_menu WHERE id = 101`).Scan(&smtpURI); err != nil {
		t.Fatalf("query smtp menu uri: %v", err)
	}
	if smtpURI != "/tools/smtp" {
		t.Fatalf("smtp menu uri = %q, want /tools/smtp", smtpURI)
	}

	var apiKeysURI string
	if err := db.QueryRow(`SELECT uri FROM goadmin_menu WHERE id = 102`).Scan(&apiKeysURI); err != nil {
		t.Fatalf("query api keys menu uri: %v", err)
	}
	if apiKeysURI != "/tools/api-keys" {
		t.Fatalf("api keys menu uri = %q, want /tools/api-keys", apiKeysURI)
	}

	if _, err := db.Exec(`INSERT INTO goadmin_site ("key", value, state) VALUES ('index_url', '/admin', 1)`); err != nil {
		t.Fatalf("insert stale index_url: %v", err)
	}
	if err := migrateGoAdmin(context.Background(), db, "admin", "secret"); err != nil {
		t.Fatalf("second migrateGoAdmin() error = %v", err)
	}
	var indexURL string
	if err := db.QueryRow(`SELECT value FROM goadmin_site WHERE "key" = 'index_url'`).Scan(&indexURL); err != nil {
		t.Fatalf("query index_url: %v", err)
	}
	if indexURL != "/" {
		t.Fatalf("persisted index_url = %q, want /", indexURL)
	}

	var permCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goadmin_user_permissions WHERE user_id = 1 AND permission_id = 1`).Scan(&permCount); err != nil {
		t.Fatalf("query permission count: %v", err)
	}
	if permCount != 1 {
		t.Fatalf("permission count = %d, want 1", permCount)
	}
}

func TestGoAdminIndexURLDoesNotDoublePrefix(t *testing.T) {
	a := newTestApp(t)
	a.goAdminDBPath = filepath.Join(t.TempDir(), "goadmin.db")

	cfg := a.goadminConfig()
	if cfg.IndexUrl != "/" {
		t.Fatalf("IndexUrl = %q, want /", cfg.IndexUrl)
	}

	goadminconfig.Initialize(&cfg)
	if got := goadminconfig.Get().GetIndexURL(); got != "/admin" {
		t.Fatalf("GetIndexURL() = %q, want /admin", got)
	}
}

func newTestApp(t *testing.T) *app {
	t.Helper()

	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate() error = %v", err)
	}

	a := newApp(db, "test-admin")
	a.now = func() time.Time { return time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC) }
	return a
}

func putJSON(t *testing.T, a *app, path string, body string, wantStatus int) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	a.routes().ServeHTTP(res, req)
	if res.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body = %s", res.Code, wantStatus, res.Body.String())
	}
}
