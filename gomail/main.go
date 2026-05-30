package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi"
)

const (
	defaultAddr        = ":8080"
	defaultDBPath      = "gomail.db"
	sendMailPerm       = "send:mail"
	ipLookupPerm       = "lookup:ip"
	qrcodeGeneratePerm = "generate:qrcode"
	maxJSONBodyBytes   = 1 << 20
	maxRecipients      = 100
	requestTimeout     = 30 * time.Second
	defaultEncryption  = "starttls"
	qrcodeCacheTTL     = 10 * time.Minute
)

type app struct {
	db             *sql.DB
	adminToken     string
	goAdminEnabled bool
	goAdminDBPath  string
	ipLookup       *ipLookupService
	qrCache        *qrCodeCache
	now            func() time.Time
}

type contextKey string

const apiKeyIDContextKey contextKey = "api_key_id"

type smtpConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"-"`
	FromEmail  string `json:"from_email"`
	FromName   string `json:"from_name"`
	Encryption string `json:"encryption"`
}

type smtpConfigRequest struct {
	Host       string  `json:"host"`
	Port       int     `json:"port"`
	Username   string  `json:"username"`
	Password   *string `json:"password,omitempty"`
	FromEmail  string  `json:"from_email"`
	FromName   string  `json:"from_name"`
	Encryption string  `json:"encryption"`
}

type publicSMTPConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordSet bool   `json:"password_set"`
	FromEmail   string `json:"from_email"`
	FromName    string `json:"from_name"`
	Encryption  string `json:"encryption"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type apiKeyRecord struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Prefix      string   `json:"prefix"`
	Permissions []string `json:"permissions"`
	CreatedAt   string   `json:"created_at"`
	ExpiresAt   *string  `json:"expires_at,omitempty"`
	RevokedAt   *string  `json:"revoked_at,omitempty"`
	LastUsedAt  *string  `json:"last_used_at,omitempty"`
}

type createAPIKeyRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	ExpiresAt   *string  `json:"expires_at,omitempty"`
}

type createAPIKeyResponse struct {
	APIKey apiKeyRecord `json:"api_key"`
	Key    string       `json:"key"`
}

type sendMailRequest struct {
	To      []string `json:"to"`
	Cc      []string `json:"cc,omitempty"`
	Bcc     []string `json:"bcc,omitempty"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	HTML    string   `json:"html,omitempty"`
	ReplyTo string   `json:"reply_to,omitempty"`
}

type sendMailResponse struct {
	Status     string `json:"status"`
	Recipients int    `json:"recipients"`
}

func main() {
	adminToken := strings.TrimSpace(os.Getenv("ADMIN_TOKEN"))
	if adminToken == "" {
		log.Fatal("ADMIN_TOKEN is required")
	}

	addr := strings.TrimSpace(os.Getenv("ADDR"))
	if addr == "" {
		addr = defaultAddr
	}

	dbPath := strings.TrimSpace(os.Getenv("DB_PATH"))
	if dbPath == "" {
		dbPath = defaultDBPath
	}

	db, err := openDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := migrate(context.Background(), db); err != nil {
		log.Fatal(err)
	}

	goAdminUser, goAdminPassword := goAdminCredentials()
	if err := migrateGoAdmin(context.Background(), db, goAdminUser, goAdminPassword); err != nil {
		log.Fatal(err)
	}

	ipLookup, err := newIPLookupService()
	if err != nil {
		log.Printf("ip lookup disabled: %v", err)
	} else {
		defer ipLookup.close()
		log.Printf("ip lookup enabled: city=%s asn=%s qqwry=%s", ipLookup.cityPath, ipLookup.asnPath, ipLookup.qqwryPath)
	}

	application := newApp(db, adminToken)
	application.ipLookup = ipLookup
	application.enableGoAdmin(dbPath)

	srv := &http.Server{
		Addr:              addr,
		Handler:           application.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("gomail listening on %s; goadmin: %s", addr, localAdminURL(addr))
	log.Fatal(srv.ListenAndServe())
}

func newApp(db *sql.DB, adminToken string) *app {
	return &app{
		db:         db,
		adminToken: adminToken,
		qrCache:    newQRCodeCache(qrcodeCacheTTL),
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (a *app) enableGoAdmin(dbPath string) {
	a.goAdminEnabled = true
	a.goAdminDBPath = dbPath
}

func localAdminURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr + "/admin"
	}
	return "http://" + addr + "/admin"
}

func (a *app) routes() http.Handler {
	r := chi.NewRouter()

	if a.goAdminEnabled {
		if err := a.mountGoAdmin(r); err != nil {
			panic(err)
		}
	}

	r.Get("/healthz", a.handleHealth)
	r.Get("/api/admin/smtp", a.requireAdmin(a.handleGetSMTP))
	r.Put("/api/admin/smtp", a.requireAdmin(a.handlePutSMTP))
	r.Post("/api/admin/test-email", a.requireAdmin(a.handleTestEmail))
	r.Post("/api/admin/api-keys", a.requireAdmin(a.handleCreateAPIKey))
	r.Get("/api/admin/api-keys", a.requireAdmin(a.handleListAPIKeys))
	r.Delete("/api/admin/api-keys/{id}", a.requireAdmin(a.handleRevokeAPIKey))
	r.Get("/api/admin/ip/lookup", a.requireAdmin(a.handleIPLookup))
	r.Get("/api/admin/qrcode", a.requireAdmin(a.handleQRCode))
	r.Post("/api/admin/qrcode", a.requireAdmin(a.handleQRCode))
	r.Post("/api/v1/mail/send", a.requirePermission(sendMailPerm, a.handleSendMail))
	r.Get("/api/v1/ip/me", a.requirePermission(ipLookupPerm, a.handleIPMe))
	r.Get("/api/v1/ip/{ip}", a.requirePermission(ipLookupPerm, a.handleIPLookup))
	r.Get("/api/v1/ip", a.requirePermission(ipLookupPerm, a.handleIPLookup))
	r.Get("/api/v1/qrcode", a.requirePermission(qrcodeGeneratePerm, a.handleQRCode))
	r.Post("/api/v1/qrcode", a.requirePermission(qrcodeGeneratePerm, a.handleQRCode))

	return withTimeout(withLogging(r))
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS smtp_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			username TEXT NOT NULL DEFAULT '',
			password TEXT NOT NULL DEFAULT '',
			from_email TEXT NOT NULL,
			from_name TEXT NOT NULL DEFAULT '',
			encryption TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			permissions TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at TEXT,
			revoked_at TEXT,
			last_used_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS email_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			api_key_id INTEGER,
			subject TEXT NOT NULL,
			recipient_count INTEGER NOT NULL,
			status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			FOREIGN KEY(api_key_id) REFERENCES api_keys(id)
		);`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func (a *app) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			token = r.Header.Get("X-Admin-Token")
		}
		if !constantTimeEqual(token, a.adminToken) {
			writeError(w, http.StatusUnauthorized, "invalid admin token")
			return
		}
		next(w, r)
	}
}

func (a *app) requirePermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing api key")
			return
		}

		record, err := a.findAPIKey(r.Context(), token)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusUnauthorized, "invalid api key")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to check api key")
			return
		}

		if record.RevokedAt != nil {
			writeError(w, http.StatusUnauthorized, "api key revoked")
			return
		}
		if record.ExpiresAt != nil {
			expiresAt, err := time.Parse(time.RFC3339, *record.ExpiresAt)
			if err != nil || !a.now().Before(expiresAt) {
				writeError(w, http.StatusUnauthorized, "api key expired")
				return
			}
		}
		if !hasPermission(record.Permissions, permission) {
			writeError(w, http.StatusForbidden, "missing permission")
			return
		}

		if err := a.markAPIKeyUsed(r.Context(), record.ID, a.now()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update api key usage")
			return
		}

		ctx := context.WithValue(r.Context(), apiKeyIDContextKey, record.ID)
		next(w, r.WithContext(ctx))
	}
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	ipStatus := "disabled"
	if a.ipLookup.enabled() {
		ipStatus = "ok"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"mail":   "ok",
		"ip":     ipStatus,
		"qrcode": "ok",
	})
}

func (a *app) handleGetSMTP(w http.ResponseWriter, r *http.Request) {
	cfg, updatedAt, ok, err := a.getSMTPConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load smtp config")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "smtp config not set")
		return
	}
	writeJSON(w, http.StatusOK, map[string]publicSMTPConfig{"smtp": publicConfig(cfg, updatedAt)})
}

func (a *app) handlePutSMTP(w http.ResponseWriter, r *http.Request) {
	var req smtpConfigRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	current, _, exists, err := a.getSMTPConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load smtp config")
		return
	}

	password := ""
	if req.Password != nil {
		password = *req.Password
	} else if exists {
		password = current.Password
	}

	cfg := smtpConfig{
		Host:       strings.TrimSpace(req.Host),
		Port:       req.Port,
		Username:   strings.TrimSpace(req.Username),
		Password:   password,
		FromEmail:  strings.TrimSpace(req.FromEmail),
		FromName:   strings.TrimSpace(req.FromName),
		Encryption: strings.TrimSpace(req.Encryption),
	}
	if cfg.Encryption == "" {
		cfg.Encryption = defaultEncryption
	}

	if err := validateSMTPConfig(cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updatedAt := a.now().Format(time.RFC3339)
	if err := a.saveSMTPConfig(r.Context(), cfg, updatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save smtp config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]publicSMTPConfig{"smtp": publicConfig(cfg, updatedAt)})
}

func (a *app) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	var req sendMailRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Subject == "" {
		req.Subject = "Gomail test email"
	}
	if req.Text == "" && req.HTML == "" {
		req.Text = "This is a test email from Gomail."
	}

	response, err := a.sendMail(r.Context(), req, nil)
	if err != nil {
		writeError(w, mailErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(name) > 80 {
		writeError(w, http.StatusBadRequest, "name is too long")
		return
	}

	permissions := req.Permissions
	if len(permissions) == 0 {
		permissions = []string{sendMailPerm}
	}
	if err := validatePermissions(permissions); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var expiresAt *string
	if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
			return
		}
		if !parsed.After(a.now()) {
			writeError(w, http.StatusBadRequest, "expires_at must be in the future")
			return
		}
		normalized := parsed.UTC().Format(time.RFC3339)
		expiresAt = &normalized
	}

	plainKey, hash, prefix, err := generateAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate api key")
		return
	}

	permsJSON, err := json.Marshal(permissions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode permissions")
		return
	}

	createdAt := a.now().Format(time.RFC3339)
	result, err := a.db.ExecContext(
		r.Context(),
		`INSERT INTO api_keys (name, key_hash, key_prefix, permissions, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		name,
		hash,
		prefix,
		string(permsJSON),
		createdAt,
		nullableString(expiresAt),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save api key")
		return
	}

	id, err := result.LastInsertId()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read api key id")
		return
	}

	writeJSON(w, http.StatusCreated, createAPIKeyResponse{
		APIKey: apiKeyRecord{
			ID:          id,
			Name:        name,
			Prefix:      prefix,
			Permissions: permissions,
			CreatedAt:   createdAt,
			ExpiresAt:   expiresAt,
		},
		Key: plainKey,
	})
}

func (a *app) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(
		r.Context(),
		`SELECT id, name, key_prefix, permissions, created_at, expires_at, revoked_at, last_used_at
		 FROM api_keys ORDER BY id DESC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list api keys")
		return
	}
	defer rows.Close()

	keys := make([]apiKeyRecord, 0)
	for rows.Next() {
		record, err := scanAPIKeyRecord(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read api key")
			return
		}
		keys = append(keys, record)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list api keys")
		return
	}

	writeJSON(w, http.StatusOK, map[string][]apiKeyRecord{"api_keys": keys})
}

func (a *app) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	idValue := chi.URLParam(r, "id")
	if idValue == "" {
		idValue = r.PathValue("id")
	}
	id, err := strconv.ParseInt(idValue, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid api key id")
		return
	}

	result, err := a.db.ExecContext(
		r.Context(),
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		a.now().Format(time.RFC3339),
		id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke api key")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read revoke result")
		return
	}
	if affected == 0 {
		writeError(w, http.StatusNotFound, "active api key not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *app) handleSendMail(w http.ResponseWriter, r *http.Request) {
	var req sendMailRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiKeyID, _ := r.Context().Value(apiKeyIDContextKey).(int64)
	response, err := a.sendMail(r.Context(), req, &apiKeyID)
	if err != nil {
		writeError(w, mailErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) handleIPLookup(w http.ResponseWriter, r *http.Request) {
	ip := requestIPValue(r)
	if ip == "" {
		writeError(w, http.StatusBadRequest, "ip is required")
		return
	}

	result, err := a.ipLookup.query(ip)
	if err != nil {
		writeError(w, mailErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *app) handleIPMe(w http.ResponseWriter, r *http.Request) {
	ip := clientIPFromRequest(r)
	if ip == "" {
		writeError(w, http.StatusBadRequest, "client ip not found")
		return
	}

	result, err := a.ipLookup.query(ip)
	if err != nil {
		writeError(w, mailErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *app) handleQRCode(w http.ResponseWriter, r *http.Request) {
	req, err := decodeQRCodeRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	image, err := a.qrCodeImage(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate qrcode")
		return
	}

	w.Header().Set("Content-Type", image.ContentType)
	w.Header().Set("Content-Disposition", `inline; filename="`+image.Filename+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(image.Data)
}

func (a *app) qrCodeImage(req qrCodeRequest) (qrCodeImage, error) {
	key := qrCodeCacheKey(req)
	if data, ok := a.qrCache.get(key); ok {
		return qrCodeImageFromData(req, data), nil
	}

	image, err := renderQRCode(req)
	if err != nil {
		return qrCodeImage{}, err
	}
	a.qrCache.set(key, image.Data)
	return image, nil
}

func qrCodeImageFromData(req qrCodeRequest, data []byte) qrCodeImage {
	if req.Format == "svg" {
		return qrCodeImage{Data: data, ContentType: "image/svg+xml", Filename: "qrcode.svg"}
	}
	return qrCodeImage{Data: data, ContentType: "image/png", Filename: "qrcode.png"}
}

func (a *app) sendMail(ctx context.Context, req sendMailRequest, apiKeyID *int64) (sendMailResponse, error) {
	cfg, _, ok, err := a.getSMTPConfig(ctx)
	if err != nil {
		return sendMailResponse{}, fmt.Errorf("failed to load smtp config")
	}
	if !ok {
		return sendMailResponse{}, statusError{status: http.StatusConflict, message: "smtp config not set"}
	}

	message, recipients, err := buildMailMessage(cfg, req, a.now())
	if err != nil {
		return sendMailResponse{}, statusError{status: http.StatusBadRequest, message: err.Error()}
	}

	status := "sent"
	errorMessage := ""
	if err := deliverSMTP(ctx, cfg, recipients, message); err != nil {
		status = "failed"
		errorMessage = err.Error()
		_ = a.logEmail(ctx, apiKeyID, req.Subject, len(recipients), status, errorMessage)
		return sendMailResponse{}, statusError{status: http.StatusBadGateway, message: "smtp delivery failed: " + err.Error()}
	}

	_ = a.logEmail(ctx, apiKeyID, req.Subject, len(recipients), status, errorMessage)
	return sendMailResponse{Status: status, Recipients: len(recipients)}, nil
}

func (a *app) getSMTPConfig(ctx context.Context) (smtpConfig, string, bool, error) {
	var cfg smtpConfig
	var updatedAt string
	err := a.db.QueryRowContext(
		ctx,
		`SELECT host, port, username, password, from_email, from_name, encryption, updated_at
		 FROM smtp_config WHERE id = 1`,
	).Scan(&cfg.Host, &cfg.Port, &cfg.Username, &cfg.Password, &cfg.FromEmail, &cfg.FromName, &cfg.Encryption, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return smtpConfig{}, "", false, nil
	}
	if err != nil {
		return smtpConfig{}, "", false, err
	}
	return cfg, updatedAt, true, nil
}

func (a *app) saveSMTPConfig(ctx context.Context, cfg smtpConfig, updatedAt string) error {
	_, err := a.db.ExecContext(
		ctx,
		`INSERT INTO smtp_config (id, host, port, username, password, from_email, from_name, encryption, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			host = excluded.host,
			port = excluded.port,
			username = excluded.username,
			password = excluded.password,
			from_email = excluded.from_email,
			from_name = excluded.from_name,
			encryption = excluded.encryption,
			updated_at = excluded.updated_at`,
		cfg.Host,
		cfg.Port,
		cfg.Username,
		cfg.Password,
		cfg.FromEmail,
		cfg.FromName,
		cfg.Encryption,
		updatedAt,
	)
	return err
}

func (a *app) findAPIKey(ctx context.Context, plainKey string) (apiKeyRecord, error) {
	row := a.db.QueryRowContext(
		ctx,
		`SELECT id, name, key_prefix, permissions, created_at, expires_at, revoked_at, last_used_at
		 FROM api_keys WHERE key_hash = ?`,
		hashAPIKey(plainKey),
	)
	return scanAPIKeyRecord(row)
}

func (a *app) markAPIKeyUsed(ctx context.Context, id int64, when time.Time) error {
	_, err := a.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, when.Format(time.RFC3339), id)
	return err
}

func (a *app) logEmail(ctx context.Context, apiKeyID *int64, subject string, recipientCount int, status string, errMessage string) error {
	_, err := a.db.ExecContext(
		ctx,
		`INSERT INTO email_logs (api_key_id, subject, recipient_count, status, error, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		nullableInt64(apiKeyID),
		subject,
		recipientCount,
		status,
		errMessage,
		a.now().Format(time.RFC3339),
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAPIKeyRecord(s scanner) (apiKeyRecord, error) {
	var record apiKeyRecord
	var permissionsJSON string
	var expiresAt, revokedAt, lastUsedAt sql.NullString

	err := s.Scan(
		&record.ID,
		&record.Name,
		&record.Prefix,
		&permissionsJSON,
		&record.CreatedAt,
		&expiresAt,
		&revokedAt,
		&lastUsedAt,
	)
	if err != nil {
		return apiKeyRecord{}, err
	}

	if err := json.Unmarshal([]byte(permissionsJSON), &record.Permissions); err != nil {
		return apiKeyRecord{}, err
	}
	record.ExpiresAt = nullStringPtr(expiresAt)
	record.RevokedAt = nullStringPtr(revokedAt)
	record.LastUsedAt = nullStringPtr(lastUsedAt)
	return record, nil
}

func validateSMTPConfig(cfg smtpConfig) error {
	if cfg.Host == "" {
		return errors.New("host is required")
	}
	if strings.ContainsAny(cfg.Host, "\r\n") {
		return errors.New("host contains invalid characters")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if cfg.FromEmail == "" {
		return errors.New("from_email is required")
	}
	from, err := mail.ParseAddress(cfg.FromEmail)
	if err != nil {
		return errors.New("from_email is invalid")
	}
	if from.Address != cfg.FromEmail {
		return errors.New("from_email must be a mailbox address without display name")
	}
	if hasLineBreak(cfg.Username) || hasLineBreak(cfg.FromName) {
		return errors.New("smtp config contains invalid newline characters")
	}
	if cfg.Username != "" && cfg.Password == "" {
		return errors.New("password is required when username is set")
	}
	switch cfg.Encryption {
	case "none", "starttls", "tls":
		return nil
	default:
		return errors.New("encryption must be one of: none, starttls, tls")
	}
}

func validatePermissions(permissions []string) error {
	seen := map[string]bool{}
	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			return errors.New("permissions cannot contain empty values")
		}
		if !supportedPermission(permission) {
			return fmt.Errorf("unsupported permission: %s", permission)
		}
		if seen[permission] {
			return fmt.Errorf("duplicate permission: %s", permission)
		}
		seen[permission] = true
	}
	return nil
}

func supportedPermission(permission string) bool {
	switch permission {
	case sendMailPerm, ipLookupPerm, qrcodeGeneratePerm:
		return true
	default:
		return false
	}
}

func buildMailMessage(cfg smtpConfig, req sendMailRequest, now time.Time) ([]byte, []string, error) {
	req.Subject = strings.TrimSpace(req.Subject)
	req.ReplyTo = strings.TrimSpace(req.ReplyTo)

	if req.Subject == "" {
		return nil, nil, errors.New("subject is required")
	}
	if hasLineBreak(req.Subject) || hasLineBreak(req.ReplyTo) {
		return nil, nil, errors.New("subject or reply_to contains invalid newline characters")
	}
	if req.Text == "" && req.HTML == "" {
		return nil, nil, errors.New("text or html is required")
	}

	toAddrs, toRecipients, err := parseAddressList(req.To)
	if err != nil {
		return nil, nil, fmt.Errorf("to: %w", err)
	}
	ccAddrs, ccRecipients, err := parseAddressList(req.Cc)
	if err != nil {
		return nil, nil, fmt.Errorf("cc: %w", err)
	}
	_, bccRecipients, err := parseAddressList(req.Bcc)
	if err != nil {
		return nil, nil, fmt.Errorf("bcc: %w", err)
	}

	recipients := append(append(toRecipients, ccRecipients...), bccRecipients...)
	if len(recipients) == 0 {
		return nil, nil, errors.New("at least one recipient is required")
	}
	if len(recipients) > maxRecipients {
		return nil, nil, fmt.Errorf("too many recipients; max is %d", maxRecipients)
	}

	from := mail.Address{Name: cfg.FromName, Address: cfg.FromEmail}

	var msg bytes.Buffer
	writeHeader(&msg, "From", from.String())
	if len(toAddrs) > 0 {
		writeHeader(&msg, "To", joinAddresses(toAddrs))
	}
	if len(ccAddrs) > 0 {
		writeHeader(&msg, "Cc", joinAddresses(ccAddrs))
	}
	if req.ReplyTo != "" {
		replyTo, err := mail.ParseAddress(req.ReplyTo)
		if err != nil {
			return nil, nil, errors.New("reply_to is invalid")
		}
		writeHeader(&msg, "Reply-To", replyTo.String())
	}
	writeHeader(&msg, "Subject", mime.QEncoding.Encode("utf-8", req.Subject))
	writeHeader(&msg, "Date", now.Format(time.RFC1123Z))
	writeHeader(&msg, "Message-ID", messageID(cfg.FromEmail, now))
	writeHeader(&msg, "MIME-Version", "1.0")

	switch {
	case req.Text != "" && req.HTML != "":
		body, boundary, err := multipartAlternativeBody(req.Text, req.HTML)
		if err != nil {
			return nil, nil, err
		}
		writeHeader(&msg, "Content-Type", fmt.Sprintf(`multipart/alternative; boundary="%s"`, boundary))
		msg.WriteString("\r\n")
		msg.Write(body)
	case req.HTML != "":
		writeHeader(&msg, "Content-Type", `text/html; charset="UTF-8"`)
		writeHeader(&msg, "Content-Transfer-Encoding", "quoted-printable")
		msg.WriteString("\r\n")
		if err := writeQuotedPrintable(&msg, req.HTML); err != nil {
			return nil, nil, err
		}
	default:
		writeHeader(&msg, "Content-Type", `text/plain; charset="UTF-8"`)
		writeHeader(&msg, "Content-Transfer-Encoding", "quoted-printable")
		msg.WriteString("\r\n")
		if err := writeQuotedPrintable(&msg, req.Text); err != nil {
			return nil, nil, err
		}
	}

	return msg.Bytes(), recipients, nil
}

func multipartAlternativeBody(textBody, htmlBody string) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	textHeader := textproto.MIMEHeader{}
	textHeader.Set("Content-Type", `text/plain; charset="UTF-8"`)
	textHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	textPart, err := writer.CreatePart(textHeader)
	if err != nil {
		return nil, "", err
	}
	if err := writeQuotedPrintable(textPart, textBody); err != nil {
		return nil, "", err
	}

	htmlHeader := textproto.MIMEHeader{}
	htmlHeader.Set("Content-Type", `text/html; charset="UTF-8"`)
	htmlHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	htmlPart, err := writer.CreatePart(htmlHeader)
	if err != nil {
		return nil, "", err
	}
	if err := writeQuotedPrintable(htmlPart, htmlBody); err != nil {
		return nil, "", err
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.Boundary(), nil
}

func writeQuotedPrintable(w io.Writer, value string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(value)); err != nil {
		_ = qp.Close()
		return err
	}
	return qp.Close()
}

func parseAddressList(values []string) ([]mail.Address, []string, error) {
	addresses := make([]mail.Address, 0, len(values))
	recipients := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if hasLineBreak(value) {
			return nil, nil, errors.New("address contains invalid newline characters")
		}
		parsed, err := mail.ParseAddress(value)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid address %q", value)
		}
		addresses = append(addresses, *parsed)
		recipients = append(recipients, parsed.Address)
	}
	return addresses, recipients, nil
}

func joinAddresses(addresses []mail.Address) string {
	parts := make([]string, 0, len(addresses))
	for _, address := range addresses {
		parts = append(parts, address.String())
	}
	return strings.Join(parts, ", ")
}

func writeHeader(w io.Writer, key, value string) {
	fmt.Fprintf(w, "%s: %s\r\n", key, value)
}

func messageID(fromEmail string, now time.Time) string {
	host := "localhost"
	if at := strings.LastIndex(fromEmail, "@"); at >= 0 && at < len(fromEmail)-1 {
		host = fromEmail[at+1:]
	}
	return fmt.Sprintf("<%d.%s@%s>", now.UnixNano(), randomToken(8), host)
}

func deliverSMTP(ctx context.Context, cfg smtpConfig, recipients []string, message []byte) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	tlsConfig := &tls.Config{
		ServerName: cfg.Host,
		MinVersion: tls.VersionTLS12,
	}

	dialer := &net.Dialer{Timeout: 15 * time.Second}

	var client *smtp.Client
	switch cfg.Encryption {
	case "tls":
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return err
		}
		c, err := smtp.NewClient(tlsConn, cfg.Host)
		if err != nil {
			_ = tlsConn.Close()
			return err
		}
		client = c
	case "starttls", "none":
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			_ = conn.Close()
			return err
		}
		client = c
		if cfg.Encryption == "starttls" {
			if ok, _ := client.Extension("STARTTLS"); !ok {
				_ = client.Close()
				return errors.New("smtp server does not support STARTTLS")
			}
			if err := client.StartTLS(tlsConfig); err != nil {
				_ = client.Close()
				return err
			}
		}
	default:
		return errors.New("unsupported smtp encryption")
	}
	defer client.Close()

	if cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("smtp server does not advertise AUTH")
		}
		auth := smtp.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host))
		if cfg.Encryption == "tls" {
			auth = implicitTLSPlainAuth{username: cfg.Username, password: cfg.Password}
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	if err := client.Mail(cfg.FromEmail); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

type statusError struct {
	status  int
	message string
}

func (e statusError) Error() string {
	return e.message
}

func mailErrorStatus(err error) int {
	var se statusError
	if errors.As(err, &se) {
		return se.status
	}
	return http.StatusInternalServerError
}

type implicitTLSPlainAuth struct {
	username string
	password string
}

func (a implicitTLSPlainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

func (a implicitTLSPlainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected smtp auth challenge")
	}
	return nil, nil
}

func publicConfig(cfg smtpConfig, updatedAt string) publicSMTPConfig {
	return publicSMTPConfig{
		Host:        cfg.Host,
		Port:        cfg.Port,
		Username:    cfg.Username,
		PasswordSet: cfg.Password != "",
		FromEmail:   cfg.FromEmail,
		FromName:    cfg.FromName,
		Encryption:  cfg.Encryption,
		UpdatedAt:   updatedAt,
	}
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func constantTimeEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func generateAPIKey() (plain string, hash string, prefix string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", err
	}
	plain = "gm_" + base64.RawURLEncoding.EncodeToString(raw)
	hash = hashAPIKey(plain)
	prefix = plain
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return plain, hash, prefix, nil
}

func hashAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func randomToken(bytesLen int) string {
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func hasPermission(permissions []string, permission string) bool {
	for _, p := range permissions {
		if p == permission {
			return true
		}
	}
	return false
}

func hasLineBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()

	limited := io.LimitReader(r.Body, maxJSONBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(data) > maxJSONBodyBytes {
		return errors.New("json body is too large")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("json body must contain one object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
