package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	htmltmpl "html/template"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/GoAdminGroup/go-admin/adapter/chi"
	goadmincontext "github.com/GoAdminGroup/go-admin/context"
	"github.com/GoAdminGroup/go-admin/engine"
	goadminauth "github.com/GoAdminGroup/go-admin/modules/auth"
	goadminconfig "github.com/GoAdminGroup/go-admin/modules/config"
	goadmindb "github.com/GoAdminGroup/go-admin/modules/db"
	"github.com/GoAdminGroup/go-admin/modules/language"
	adminplugin "github.com/GoAdminGroup/go-admin/plugins/admin"
	adminformvalues "github.com/GoAdminGroup/go-admin/plugins/admin/modules/form"
	admintable "github.com/GoAdminGroup/go-admin/plugins/admin/modules/table"
	admintypes "github.com/GoAdminGroup/go-admin/template/types"
	admintypeform "github.com/GoAdminGroup/go-admin/template/types/form"
	"github.com/GoAdminGroup/themes/adminlte"
	"github.com/go-chi/chi"
)

const (
	goAdminPrefix          = "admin"
	goAdminDashboardTitle  = "Gomail Admin"
	goAdminDefaultUser     = "admin"
	goAdminDefaultPassword = "admin"
)

type dashboardStats struct {
	SMTPConfigured bool
	IPConfigured   bool
	QRCodeReady    bool
	APIKeys        int
	ActiveKeys     int
	Sent24h        int
	Failed24h      int
}

func goAdminCredentials() (string, string) {
	user := strings.TrimSpace(os.Getenv("GOADMIN_USER"))
	if user == "" {
		user = goAdminDefaultUser
	}
	password := os.Getenv("GOADMIN_PASSWORD")
	if password == "" {
		password = goAdminDefaultPassword
	}
	return user, password
}

func migrateGoAdmin(ctx context.Context, db *sql.DB, username, password string) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS goadmin_menu (
			id INTEGER PRIMARY KEY,
			parent_id INTEGER NOT NULL DEFAULT 0,
			type INTEGER NOT NULL DEFAULT 0,
			"order" INTEGER NOT NULL DEFAULT 0,
			title TEXT NOT NULL,
			icon TEXT NOT NULL,
			uri TEXT NOT NULL DEFAULT '',
			header TEXT,
			plugin_name TEXT NOT NULL DEFAULT '',
			uuid TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_operation_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			path TEXT NOT NULL,
			method TEXT NOT NULL,
			ip TEXT NOT NULL,
			input TEXT NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_site (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			"key" TEXT,
			value TEXT,
			description TEXT,
			state INTEGER NOT NULL DEFAULT 0,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_permissions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			slug TEXT NOT NULL UNIQUE,
			http_method TEXT,
			http_path TEXT NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_role_menu (
			role_id INTEGER NOT NULL,
			menu_id INTEGER NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(role_id, menu_id)
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_role_permissions (
			role_id INTEGER NOT NULL,
			permission_id INTEGER NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(role_id, permission_id)
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_role_users (
			role_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(role_id, user_id)
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_roles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			slug TEXT NOT NULL UNIQUE,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_session (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sid TEXT NOT NULL DEFAULT '',
			"values" TEXT NOT NULL DEFAULT '',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_user_permissions (
			user_id INTEGER NOT NULL,
			permission_id INTEGER NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, permission_id)
		);`,
		`CREATE TABLE IF NOT EXISTS goadmin_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			avatar TEXT,
			remember_token TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	if err := execStatements(ctx, db, stmts); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx,
		`UPDATE goadmin_site
		 SET value = '/', updated_at = ?
		 WHERE "key" = 'index_url' AND value IN ('/admin', 'admin')`,
		now,
	); err != nil {
		return err
	}

	adminHash := goadminauth.EncodePassword([]byte(password))
	if adminHash == "" {
		return errors.New("failed to hash goadmin password")
	}

	seedPermissionStatements := []string{
		`INSERT OR IGNORE INTO goadmin_permissions (id, name, slug, http_method, http_path, created_at, updated_at)
		 VALUES (1, 'All permission', '*', '', '*', '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_permissions (id, name, slug, http_method, http_path, created_at, updated_at)
		 VALUES (2, 'Dashboard', 'dashboard', 'GET,PUT,POST,DELETE', '/', '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_roles (id, name, slug, created_at, updated_at)
		 VALUES (1, 'Administrator', 'administrator', '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_roles (id, name, slug, created_at, updated_at)
		 VALUES (2, 'Operator', 'operator', '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_role_permissions (role_id, permission_id, created_at, updated_at)
		 VALUES (1, 1, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_role_permissions (role_id, permission_id, created_at, updated_at)
		 VALUES (1, 2, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_role_permissions (role_id, permission_id, created_at, updated_at)
		 VALUES (2, 2, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
		 VALUES (1, 0, 1, 2, 'Admin', 'fa-tasks', '', '', NULL, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
		 VALUES (2, 1, 1, 2, 'Users', 'fa-users', '/info/manager', '', NULL, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
		 VALUES (3, 1, 1, 3, 'Roles', 'fa-user', '/info/roles', '', NULL, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
		 VALUES (4, 1, 1, 4, 'Permission', 'fa-ban', '/info/permission', '', NULL, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
		 VALUES (5, 1, 1, 5, 'Menu', 'fa-bars', '/menu', '', NULL, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
		 VALUES (6, 1, 1, 6, 'Operation log', 'fa-history', '/info/op', '', NULL, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
		 VALUES (7, 0, 1, 1, 'Dashboard', 'fa-bar-chart', '/', '', NULL, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_role_users (role_id, user_id, created_at, updated_at)
		 VALUES (1, 1, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_role_users (role_id, user_id, created_at, updated_at)
		 VALUES (2, 2, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_user_permissions (user_id, permission_id, created_at, updated_at)
		 VALUES (1, 1, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_user_permissions (user_id, permission_id, created_at, updated_at)
		 VALUES (1, 2, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
		`INSERT OR IGNORE INTO goadmin_user_permissions (user_id, permission_id, created_at, updated_at)
		 VALUES (2, 2, '2019-09-10 00:00:00', '2019-09-10 00:00:00');`,
	}

	if err := execStatements(ctx, db, seedPermissionStatements); err != nil {
		return err
	}

	_, err := db.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO goadmin_users (id, username, password, name, avatar, remember_token, created_at, updated_at)
		 VALUES (1, ?, ?, ?, '', 'tlNcBVK9AvfYH7WEnwB1RKvocJu8FfRy4um3DJtwdHuJy0dwFsLOgAc0xUfh', ?, ?)`,
		username,
		adminHash,
		username,
		now,
		now,
	)
	if err != nil {
		return err
	}

	operatorHash := goadminauth.EncodePassword([]byte("operator"))
	if operatorHash == "" {
		return errors.New("failed to hash goadmin operator password")
	}
	_, err = db.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO goadmin_users (id, username, password, name, avatar, remember_token, created_at, updated_at)
		 VALUES (2, 'operator', ?, 'Operator', '', NULL, '2019-09-10 00:00:00', '2019-09-10 00:00:00')`,
		operatorHash,
	)
	if err != nil {
		return err
	}

	var adminUserID int
	err = db.QueryRowContext(ctx, `SELECT id FROM goadmin_users WHERE username = ?`, username).Scan(&adminUserID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err := db.ExecContext(
			ctx,
			`INSERT INTO goadmin_users (username, password, name, avatar, remember_token, created_at, updated_at)
			 VALUES (?, ?, ?, '', NULL, ?, ?)`,
			username,
			adminHash,
			username,
			now,
			now,
		)
		if err != nil {
			return err
		}
		if err := db.QueryRowContext(ctx, `SELECT id FROM goadmin_users WHERE username = ?`, username).Scan(&adminUserID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`INSERT OR IGNORE INTO goadmin_role_users (role_id, user_id, created_at, updated_at)
		 VALUES (1, ?, ?, ?)`,
		adminUserID, now, now,
	)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`INSERT OR IGNORE INTO goadmin_user_permissions (user_id, permission_id, created_at, updated_at)
		 VALUES (?, 1, ?, ?)`,
		adminUserID, now, now,
	)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
		VALUES (100, 0, 1, 20, 'Gomail', 'fa-envelope', '', '', NULL, ?, ?)`, now, now)
	if err != nil {
		return err
	}
	menuRows := []struct {
		id    int
		order int
		title string
		icon  string
		uri   string
	}{
		{101, 1, "SMTP Config", "fa-cog", "/tools/smtp"},
		{102, 2, "API Keys", "fa-key", "/tools/api-keys"},
		{103, 3, "Email Logs", "fa-list", "/info/email_logs"},
	}
	for _, row := range menuRows {
		_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
			VALUES (?, 100, 1, ?, ?, ?, ?, '', NULL, ?, ?)`, row.id, row.order, row.title, row.icon, row.uri, now, now)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO goadmin_role_menu (role_id, menu_id, created_at, updated_at)
			VALUES (1, ?, ?, ?)`, row.id, now, now)
		if err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO goadmin_role_menu (role_id, menu_id, created_at, updated_at)
		VALUES (1, 100, ?, ?)`, now, now)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE goadmin_menu SET uri = '/tools/smtp', updated_at = ? WHERE id = 101`, now)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE goadmin_menu SET uri = '/tools/api-keys', updated_at = ? WHERE id = 102`, now)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
		VALUES (110, 0, 1, 21, 'Utilities', 'fa-wrench', '', '', NULL, ?, ?)`, now, now)
	if err != nil {
		return err
	}
	toolRows := []struct {
		id    int
		order int
		title string
		icon  string
		uri   string
	}{
		{111, 1, "IP Lookup", "fa-map-marker", "/tools/ip"},
		{112, 2, "QR Code", "fa-qrcode", "/tools/qrcode"},
	}
	for _, row := range toolRows {
		_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO goadmin_menu (id, parent_id, type, "order", title, icon, uri, plugin_name, header, created_at, updated_at)
			VALUES (?, 110, 1, ?, ?, ?, ?, '', NULL, ?, ?)`, row.id, row.order, row.title, row.icon, row.uri, now, now)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO goadmin_role_menu (role_id, menu_id, created_at, updated_at)
			VALUES (1, ?, ?, ?)`, row.id, now, now)
		if err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO goadmin_role_menu (role_id, menu_id, created_at, updated_at)
		VALUES (1, 110, ?, ?)`, now, now)
	return err
}

func (a *app) mountGoAdmin(router chi.Router) error {
	if err := os.MkdirAll("uploads", 0o755); err != nil {
		return err
	}

	eng := engine.Default()
	cfg := a.goadminConfig()

	admin := adminplugin.NewAdmin(a.goadminGenerators())
	if err := eng.AddConfig(&cfg).
		AddPlugins(admin).
		AddDisplayFilterXssJsFilter().
		Use(router); err != nil {
		return err
	}

	eng.HTML("GET", "/admin", a.goadminDashboard)
	eng.HTML("GET", "/admin/tools/smtp", a.goadminSMTPTool)
	eng.Data("GET", "/admin/tools/smtp/config", a.goadminSMTPConfigData)
	eng.Data("POST", "/admin/tools/smtp/config", a.goadminSMTPSaveData)
	eng.Data("POST", "/admin/tools/smtp/test", a.goadminSMTPTestData)
	eng.HTML("GET", "/admin/tools/api-keys", a.goadminAPIKeysTool)
	eng.Data("GET", "/admin/tools/api-keys/list", a.goadminAPIKeysListData)
	eng.Data("POST", "/admin/tools/api-keys/create", a.goadminAPIKeysCreateData)
	eng.Data("POST", "/admin/tools/api-keys/revoke", a.goadminAPIKeysRevokeData)
	eng.HTML("GET", "/admin/tools/ip", a.goadminIPTool)
	eng.HTML("GET", "/admin/tools/qrcode", a.goadminQRCodeTool)
	eng.Data("GET", "/admin/tools/ip/lookup", a.goadminIPLookupData)
	eng.Data("GET", "/admin/tools/qrcode/render", a.goadminQRCodeData)
	eng.Data("POST", "/admin/tools/qrcode/render", a.goadminQRCodeData)
	return nil
}

func (a *app) goadminConfig() goadminconfig.Config {
	return goadminconfig.Config{
		Databases: goadminconfig.DatabaseList{
			"default": {
				File:         a.goAdminDBPath,
				Driver:       goadminconfig.DriverSqlite,
				MaxIdleConns: 1,
				MaxOpenConns: 1,
			},
		},
		UrlPrefix:                     goAdminPrefix,
		Store:                         goadminconfig.Store{Path: "./uploads", Prefix: "uploads"},
		Language:                      language.CN,
		IndexUrl:                      "/",
		LoginTitle:                    goAdminDashboardTitle,
		Title:                         goAdminDashboardTitle,
		Logo:                          htmltmpl.HTML("Gomail"),
		MiniLogo:                      htmltmpl.HTML("GM"),
		Debug:                         false,
		Env:                           goadminconfig.EnvLocal,
		ColorScheme:                   adminlte.ColorschemeSkinBlack,
		ProhibitConfigModification:    true,
		HideConfigCenterEntrance:      true,
		HideAppInfoEntrance:           true,
		HideToolEntrance:              true,
		HidePluginEntrance:            true,
		HideVisitorUserCenterEntrance: true,
	}
}

func (a *app) goadminGenerators() admintable.GeneratorList {
	return admintable.GeneratorList{
		"smtp_config": a.smtpConfigTable,
		"api_keys":    a.apiKeysTable,
		"email_logs":  a.emailLogsTable,
	}
}

func (a *app) goadminDashboard(ctx *goadmincontext.Context) (admintypes.Panel, error) {
	stats, err := a.dashboardStats(ctx.Request.Context())
	if err != nil {
		return admintypes.Panel{}, err
	}

	smtpState := "Not configured"
	if stats.SMTPConfigured {
		smtpState = "Configured"
	}
	ipState := "Disabled"
	if stats.IPConfigured {
		ipState = "Ready"
	}

	content := fmt.Sprintf(`
<div class="row">
  <div class="col-lg-2 col-sm-4 col-xs-6">
    <div class="small-box bg-aqua">
      <div class="inner"><h3>%s</h3><p>SMTP</p></div>
      <div class="icon"><i class="fa fa-envelope"></i></div>
      <a href="/admin/tools/smtp" class="small-box-footer">Open <i class="fa fa-arrow-circle-right"></i></a>
    </div>
  </div>
  <div class="col-lg-2 col-sm-4 col-xs-6">
    <div class="small-box bg-green">
      <div class="inner"><h3>%d</h3><p>API Keys</p></div>
      <div class="icon"><i class="fa fa-key"></i></div>
      <a href="/admin/tools/api-keys" class="small-box-footer">Open <i class="fa fa-arrow-circle-right"></i></a>
    </div>
  </div>
  <div class="col-lg-2 col-sm-4 col-xs-6">
    <div class="small-box bg-yellow">
      <div class="inner"><h3>%d</h3><p>Active Keys</p></div>
      <div class="icon"><i class="fa fa-shield"></i></div>
      <a href="/admin/tools/api-keys" class="small-box-footer">Open <i class="fa fa-arrow-circle-right"></i></a>
    </div>
  </div>
  <div class="col-lg-2 col-sm-4 col-xs-6">
    <div class="small-box bg-purple">
      <div class="inner"><h3>%d</h3><p>Sent 24h</p></div>
      <div class="icon"><i class="fa fa-paper-plane"></i></div>
      <a href="/admin/info/email_logs" class="small-box-footer">Open <i class="fa fa-arrow-circle-right"></i></a>
    </div>
  </div>
  <div class="col-lg-2 col-sm-4 col-xs-6">
    <div class="small-box bg-red">
      <div class="inner"><h3>%d</h3><p>Failed 24h</p></div>
      <div class="icon"><i class="fa fa-exclamation-triangle"></i></div>
      <a href="/admin/info/email_logs" class="small-box-footer">Open <i class="fa fa-arrow-circle-right"></i></a>
    </div>
  </div>
  <div class="col-lg-2 col-sm-4 col-xs-6">
    <div class="small-box bg-teal">
      <div class="inner"><h3>%s</h3><p>IP DB</p></div>
      <div class="icon"><i class="fa fa-map-marker"></i></div>
      <a href="/admin/tools/ip" class="small-box-footer">Open <i class="fa fa-arrow-circle-right"></i></a>
    </div>
  </div>
</div>
<div class="row">
  <div class="col-md-6">
    <div class="box box-solid">
      <div class="box-header with-border"><h3 class="box-title"><i class="fa fa-random"></i> Routes</h3></div>
      <div class="box-body no-padding">
        <table class="table table-striped">
          <tbody>
            <tr><td><code>POST /api/v1/mail/send</code></td><td>send:mail</td></tr>
            <tr><td><code>GET /api/v1/ip/{ip}</code></td><td>lookup:ip</td></tr>
            <tr><td><code>GET|POST /api/v1/qrcode</code></td><td>generate:qrcode</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
  <div class="col-md-6">
    <div class="box box-solid">
      <div class="box-header with-border"><h3 class="box-title"><i class="fa fa-cubes"></i> Tools</h3></div>
      <div class="box-body">
        <a class="btn btn-app" href="/admin/tools/ip"><i class="fa fa-map-marker"></i> IP Lookup</a>
        <a class="btn btn-app" href="/admin/tools/qrcode"><i class="fa fa-qrcode"></i> QR Code</a>
        <a class="btn btn-app" href="/admin/tools/smtp"><i class="fa fa-cog"></i> SMTP</a>
        <a class="btn btn-app" href="/admin/tools/api-keys"><i class="fa fa-key"></i> Keys</a>
      </div>
    </div>
  </div>
</div>`,
		htmltmpl.HTMLEscapeString(smtpState),
		stats.APIKeys,
		stats.ActiveKeys,
		stats.Sent24h,
		stats.Failed24h,
		htmltmpl.HTMLEscapeString(ipState),
	)

	return admintypes.Panel{
		Title:       htmltmpl.HTML("Gomail Suite"),
		Description: htmltmpl.HTML("Mail delivery, IP lookup, and QR code generation"),
		Content:     htmltmpl.HTML(content),
	}, nil
}

func (a *app) dashboardStats(ctx context.Context) (dashboardStats, error) {
	var stats dashboardStats
	stats.IPConfigured = a.ipLookup.enabled()
	stats.QRCodeReady = true

	var statsVal int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smtp_config WHERE id = 1 AND host <> ''`).Scan(&statsVal); err != nil {
		return stats, err
	}
	stats.SMTPConfigured = statsVal > 0

	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys`).Scan(&stats.APIKeys); err != nil {
		return stats, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := a.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM api_keys
		WHERE revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at = '' OR expires_at > ?)`, now).Scan(&stats.ActiveKeys); err != nil {
		return stats, err
	}

	since := a.now().Add(-24 * time.Hour).Format(time.RFC3339)
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_logs WHERE created_at >= ? AND status = 'failed'`, since).Scan(&stats.Failed24h); err != nil {
		return stats, err
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_logs WHERE created_at >= ? AND status = 'sent'`, since).Scan(&stats.Sent24h); err != nil {
		return stats, err
	}
	return stats, nil
}

func (a *app) goadminSMTPTool(ctx *goadmincontext.Context) (admintypes.Panel, error) {
	content := `
<style>
.gm-smtp-grid{display:grid;grid-template-columns:minmax(0,1fr) 360px;gap:18px;align-items:start}
.gm-smtp-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}
.gm-smtp-fields .full{grid-column:1 / -1}
.gm-smtp-state{min-height:22px;margin-top:12px;color:#666;white-space:pre-line}
.gm-smtp-state.text-danger{color:#dd4b39}
.gm-smtp-state.text-success{color:#00a65a}
.gm-smtp-actions{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:4px}
.gm-smtp-badge{font-size:12px;color:#666}
.gm-smtp-help{font-size:12px;color:#777;margin:6px 0 0;line-height:1.45}
@media (max-width:991px){.gm-smtp-grid{grid-template-columns:1fr}}
@media (max-width:640px){.gm-smtp-fields{grid-template-columns:1fr}.gm-smtp-actions .btn{width:100%}}
</style>
<div id="gm-smtp-tool">
  <div class="gm-smtp-grid">
    <div class="box box-solid">
      <div class="box-header with-border"><h3 class="box-title"><i class="fa fa-cog"></i> SMTP Config</h3></div>
      <div class="box-body">
        <div class="gm-smtp-fields">
          <div class="form-group">
            <label for="gm-smtp-host">Host</label>
            <input id="gm-smtp-host" class="form-control" type="text" autocomplete="off">
          </div>
          <div class="form-group">
            <label for="gm-smtp-port">Port</label>
            <input id="gm-smtp-port" class="form-control" type="number" min="1" max="65535" value="587">
          </div>
          <div class="form-group">
            <label for="gm-smtp-username">Username</label>
            <input id="gm-smtp-username" class="form-control" type="text" autocomplete="off">
          </div>
          <div class="form-group">
            <label for="gm-smtp-password">Password</label>
            <input id="gm-smtp-password" class="form-control" type="password" autocomplete="new-password">
            <div id="gm-smtp-password-state" class="gm-smtp-badge"></div>
          </div>
          <div class="form-group">
            <label for="gm-smtp-from-email">From Email</label>
            <input id="gm-smtp-from-email" class="form-control" type="email" autocomplete="off">
          </div>
          <div class="form-group">
            <label for="gm-smtp-from-name">From Name</label>
            <input id="gm-smtp-from-name" class="form-control" type="text" autocomplete="off">
          </div>
          <div class="form-group full">
            <label for="gm-smtp-encryption">Encryption</label>
            <select id="gm-smtp-encryption" class="form-control">
              <option value="starttls">STARTTLS (587)</option>
              <option value="tls">TLS / SSL (465)</option>
              <option value="none">None (25)</option>
            </select>
            <p class="gm-smtp-help">SSL maps to TLS / SSL here. Use STARTTLS for port 587, TLS / SSL for port 465, and None only for trusted internal SMTP.</p>
          </div>
        </div>
        <div class="gm-smtp-actions">
          <button id="gm-smtp-save" type="button" class="btn btn-primary"><i class="fa fa-save"></i> Save</button>
          <button id="gm-smtp-reload" type="button" class="btn btn-default"><i class="fa fa-refresh"></i> Reload</button>
        </div>
        <div id="gm-smtp-state" class="gm-smtp-state"></div>
      </div>
    </div>
    <div class="box box-solid">
      <div class="box-header with-border"><h3 class="box-title"><i class="fa fa-paper-plane"></i> Test Email</h3></div>
      <div class="box-body">
        <div class="form-group">
          <label for="gm-smtp-test-to">To</label>
          <input id="gm-smtp-test-to" class="form-control" type="email" autocomplete="off">
        </div>
        <div class="form-group">
          <label for="gm-smtp-test-subject">Subject</label>
          <input id="gm-smtp-test-subject" class="form-control" type="text" value="Gomail SMTP test">
        </div>
        <div class="form-group">
          <label for="gm-smtp-test-text">Text</label>
          <textarea id="gm-smtp-test-text" class="form-control" rows="6">This is a test email from Gomail.</textarea>
        </div>
        <button id="gm-smtp-test-send" type="button" class="btn btn-success"><i class="fa fa-send"></i> Send Test</button>
        <p class="gm-smtp-help">The test uses the current form values. If the password box is empty, the saved password is kept.</p>
        <div id="gm-smtp-test-state" class="gm-smtp-state"></div>
      </div>
    </div>
  </div>
</div>
<script>
(function(){
  var root = document.getElementById('gm-smtp-tool');
  if (!root || root.dataset.ready) return;
  root.dataset.ready = '1';
  var host = root.querySelector('#gm-smtp-host');
  var port = root.querySelector('#gm-smtp-port');
  var username = root.querySelector('#gm-smtp-username');
  var password = root.querySelector('#gm-smtp-password');
  var passwordState = root.querySelector('#gm-smtp-password-state');
  var fromEmail = root.querySelector('#gm-smtp-from-email');
  var fromName = root.querySelector('#gm-smtp-from-name');
  var encryption = root.querySelector('#gm-smtp-encryption');
  var saveBtn = root.querySelector('#gm-smtp-save');
  var reloadBtn = root.querySelector('#gm-smtp-reload');
  var state = root.querySelector('#gm-smtp-state');
  var testTo = root.querySelector('#gm-smtp-test-to');
  var testSubject = root.querySelector('#gm-smtp-test-subject');
  var testText = root.querySelector('#gm-smtp-test-text');
  var testBtn = root.querySelector('#gm-smtp-test-send');
  var testState = root.querySelector('#gm-smtp-test-state');

  function setState(el, type, msg) {
    el.className = 'gm-smtp-state' + (type ? ' text-' + type : '');
    el.textContent = msg || '';
  }
  function responseExcerpt(text) {
    return (text || '').replace(/\s+/g, ' ').trim().slice(0, 180);
  }
  function nonJSONMessage(res, text) {
    if (res.status === 401 || res.status === 403 || /<html|<!doctype|login/i.test(text || '')) {
      return 'Admin session may have expired. Refresh this page and log in again.';
    }
    var msg = 'Expected JSON but received HTTP ' + res.status;
    var excerpt = responseExcerpt(text);
    if (excerpt) msg += ': ' + excerpt;
    return msg;
  }
  function requestJSON(url, options) {
    options = options || {};
    options.headers = options.headers || {};
    options.headers.Accept = options.headers.Accept || 'application/json';
    return fetch(url, options).then(function(res) {
      return res.text().then(function(text) {
        var data = {};
        if (text) {
          try {
            data = JSON.parse(text);
          } catch (parseErr) {
            var jsonErr = new Error(nonJSONMessage(res, text));
            jsonErr.status = res.status;
            throw jsonErr;
          }
        }
        if (!res.ok) {
          var err = new Error(data.error || ('HTTP ' + res.status + ' request failed'));
          err.status = data.status || res.status;
          err.kind = data.kind || '';
          err.hint = data.hint || '';
          throw err;
        }
        return data;
      });
    }).catch(function(err) {
      if (err && err.name === 'TypeError' && !err.status) {
        err.message = 'Network request failed. Check that Gomail is reachable, then try again.';
      }
      throw err;
    });
  }
  function formatError(err, context) {
    var lines = [err && err.message ? err.message : 'Request failed'];
    if (err && err.hint) lines.push('Hint: ' + err.hint);
    if (err && (err.status || err.kind)) lines.push('Status: ' + (err.status || '-') + (err.kind ? ' / ' + err.kind : ''));
    if (context) lines.push(context);
    return lines.join('\n');
  }
  function applyConfig(smtp) {
    host.value = smtp.host || '';
    port.value = smtp.port || 587;
    username.value = smtp.username || '';
    fromEmail.value = smtp.from_email || '';
    fromName.value = smtp.from_name || '';
    encryption.value = smtp.encryption || 'starttls';
    password.value = '';
    passwordState.textContent = smtp.password_set ? 'Password saved' : '';
    if (!testTo.value && smtp.from_email) testTo.value = smtp.from_email;
  }
  function loadConfig() {
    setState(state, '', 'Loading...');
    requestJSON('/admin/tools/smtp/config')
      .then(function(data) {
        if (data.configured && data.smtp) {
          applyConfig(data.smtp);
          setState(state, 'success', 'Ready');
        } else {
          setState(state, '', '');
        }
      })
      .catch(function(err) { setState(state, 'danger', formatError(err)); });
  }
  function configPayload() {
    var payload = {
      host: host.value.trim(),
      port: Number(port.value),
      username: username.value.trim(),
      from_email: fromEmail.value.trim(),
      from_name: fromName.value.trim(),
      encryption: encryption.value
    };
    if (password.value) payload.password = password.value;
    return payload;
  }
  function saveConfig() {
    saveBtn.disabled = true;
    setState(state, '', 'Saving...');
    requestJSON('/admin/tools/smtp/config', {
      method: 'POST',
      headers: {'Content-Type':'application/json', Accept:'application/json'},
      body: JSON.stringify(configPayload())
    })
      .then(function(data) {
        if (data.smtp) applyConfig(data.smtp);
        setState(state, 'success', 'Saved');
      })
      .catch(function(err) { setState(state, 'danger', formatError(err)); })
      .finally(function() { saveBtn.disabled = false; });
  }
  function testConfigPayload() {
    var payload = configPayload();
    if (!password.value) delete payload.password;
    return payload;
  }
  function currentConfigSummary() {
    var selected = encryption.options[encryption.selectedIndex];
    var label = selected ? selected.text : encryption.value;
    return 'Current config: ' + (host.value.trim() || '-') + ':' + (port.value || '-') + ' / ' + label;
  }
  function sendTest() {
    var to = testTo.value.trim();
    if (!to) {
      setState(testState, 'danger', 'To is required');
      return;
    }
    testBtn.disabled = true;
    setState(testState, '', 'Sending...');
    var payload = testConfigPayload();
    payload.to = [to];
    payload.subject = testSubject.value.trim();
    payload.text = testText.value;
    requestJSON('/admin/tools/smtp/test', {
      method: 'POST',
      headers: {'Content-Type':'application/json', Accept:'application/json'},
      body: JSON.stringify(payload)
    })
      .then(function(data) {
        setState(testState, 'success', 'Sent to ' + (data.recipients || 0) + ' recipient(s)');
      })
      .catch(function(err) { setState(testState, 'danger', formatError(err, currentConfigSummary())); })
      .finally(function() { testBtn.disabled = false; });
  }
  saveBtn.addEventListener('click', saveConfig);
  reloadBtn.addEventListener('click', loadConfig);
  testBtn.addEventListener('click', sendTest);
  loadConfig();
})();
</script>`

	return admintypes.Panel{
		Title:       htmltmpl.HTML("SMTP Config"),
		Description: htmltmpl.HTML("SMTP delivery settings and test email"),
		Content:     htmltmpl.HTML(content),
	}, nil
}

func (a *app) goadminSMTPConfigData(ctx *goadmincontext.Context) {
	cfg, updatedAt, ok, err := a.getSMTPConfig(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, map[string]interface{}{"error": "failed to load smtp config"})
		return
	}
	if !ok {
		ctx.JSON(http.StatusOK, map[string]interface{}{"configured": false})
		return
	}
	ctx.JSON(http.StatusOK, map[string]interface{}{
		"configured": true,
		"smtp":       publicConfig(cfg, updatedAt),
	})
}

func (a *app) goadminSMTPSaveData(ctx *goadmincontext.Context) {
	var req smtpConfigRequest
	if err := decodeJSON(ctx.Request, &req); err != nil {
		ctx.JSON(http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}

	smtp, err := a.upsertSMTPConfig(ctx.Request.Context(), req)
	if err != nil {
		ctx.JSON(mailErrorStatus(err), smtpErrorPayload(err))
		return
	}
	ctx.JSON(http.StatusOK, map[string]interface{}{"smtp": smtp})
}

func (a *app) goadminSMTPTestData(ctx *goadmincontext.Context) {
	var req smtpTestRequest
	if err := decodeJSON(ctx.Request, &req); err != nil {
		ctx.JSON(http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	if req.Subject == "" {
		req.Subject = "Gomail test email"
	}
	if req.Text == "" && req.HTML == "" {
		req.Text = "This is a test email from Gomail."
	}

	var response sendMailResponse
	var err error
	if emptySMTPConfigRequest(req.smtpConfigRequest) {
		response, err = a.sendMail(ctx.Request.Context(), req.sendMailRequest, nil)
	} else {
		var cfg smtpConfig
		cfg, err = a.buildSMTPConfig(ctx.Request.Context(), req.smtpConfigRequest)
		if err == nil {
			response, err = a.sendMailWithConfig(ctx.Request.Context(), cfg, req.sendMailRequest, nil)
		}
	}
	if err != nil {
		ctx.JSON(mailErrorStatus(err), smtpErrorPayload(err))
		return
	}
	ctx.JSON(http.StatusOK, map[string]interface{}{
		"status":     response.Status,
		"recipients": response.Recipients,
	})
}

func (a *app) goadminAPIKeysTool(ctx *goadmincontext.Context) (admintypes.Panel, error) {
	content := `
<style>
.gm-key-grid{display:grid;grid-template-columns:360px minmax(0,1fr);gap:18px;align-items:start}
.gm-key-perms{display:grid;gap:8px;margin-top:6px}
.gm-key-state{min-height:22px;margin-top:12px;color:#666}
.gm-key-state.text-danger{color:#dd4b39}
.gm-key-result{display:none;margin-top:14px}
.gm-key-result textarea{font-family:Menlo,Consolas,monospace;resize:vertical}
.gm-key-table td,.gm-key-table th{vertical-align:middle!important}
.gm-key-table code{font-size:12px}
.gm-key-actions{white-space:nowrap}
@media (max-width:991px){.gm-key-grid{grid-template-columns:1fr}}
</style>
<div id="gm-api-keys-tool">
  <div class="gm-key-grid">
    <div class="box box-solid">
      <div class="box-header with-border"><h3 class="box-title"><i class="fa fa-key"></i> New API Key</h3></div>
      <div class="box-body">
        <div class="form-group">
          <label for="gm-key-name">Name</label>
          <input id="gm-key-name" class="form-control" type="text" maxlength="80" autocomplete="off">
        </div>
        <div class="form-group">
          <label>Permissions</label>
          <div class="gm-key-perms">
            <label><input type="checkbox" value="send:mail" checked> send:mail</label>
            <label><input type="checkbox" value="lookup:ip"> lookup:ip</label>
            <label><input type="checkbox" value="generate:qrcode"> generate:qrcode</label>
          </div>
        </div>
        <div class="form-group">
          <label for="gm-key-expires">Expires At</label>
          <input id="gm-key-expires" class="form-control" type="datetime-local">
        </div>
        <button id="gm-key-create" type="button" class="btn btn-primary"><i class="fa fa-plus"></i> Create</button>
        <div id="gm-key-state" class="gm-key-state"></div>
        <div id="gm-key-result" class="gm-key-result">
          <label for="gm-key-plain">Plain Key</label>
          <textarea id="gm-key-plain" class="form-control" rows="3" readonly></textarea>
          <button id="gm-key-copy" type="button" class="btn btn-default btn-sm" style="margin-top:8px"><i class="fa fa-copy"></i> Copy</button>
        </div>
      </div>
    </div>
    <div class="box box-solid">
      <div class="box-header with-border">
        <h3 class="box-title"><i class="fa fa-list"></i> API Keys</h3>
        <div class="box-tools"><button id="gm-key-refresh" class="btn btn-box-tool" type="button"><i class="fa fa-refresh"></i></button></div>
      </div>
      <div class="box-body table-responsive no-padding">
        <table class="table table-striped gm-key-table">
          <thead><tr><th>ID</th><th>Name</th><th>Prefix</th><th>Permissions</th><th>Created</th><th>Status</th><th></th></tr></thead>
          <tbody id="gm-key-list"><tr><td colspan="7">Loading...</td></tr></tbody>
        </table>
      </div>
    </div>
  </div>
</div>
<script>
(function(){
  var root = document.getElementById('gm-api-keys-tool');
  if (!root || root.dataset.ready) return;
  root.dataset.ready = '1';
  var nameInput = root.querySelector('#gm-key-name');
  var expiresInput = root.querySelector('#gm-key-expires');
  var createBtn = root.querySelector('#gm-key-create');
  var refreshBtn = root.querySelector('#gm-key-refresh');
  var state = root.querySelector('#gm-key-state');
  var result = root.querySelector('#gm-key-result');
  var plain = root.querySelector('#gm-key-plain');
  var copyBtn = root.querySelector('#gm-key-copy');
  var list = root.querySelector('#gm-key-list');

  function text(value) {
    if (value === undefined || value === null) return '';
    return String(value);
  }
  function esc(value) {
    return text(value).replace(/[&<>"']/g, function(ch) {
      return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch];
    });
  }
  function selectedPermissions() {
    return Array.prototype.slice.call(root.querySelectorAll('.gm-key-perms input:checked')).map(function(input) {
      return input.value;
    });
  }
  function expiresValue() {
    if (!expiresInput.value) return null;
    var date = new Date(expiresInput.value);
    if (isNaN(date.getTime())) return 'invalid';
    return date.toISOString();
  }
  function keyStatus(key) {
    if (key.revoked_at) return '<span class="label label-default">Revoked</span>';
    if (key.expires_at && new Date(key.expires_at).getTime() <= Date.now()) return '<span class="label label-warning">Expired</span>';
    return '<span class="label label-success">Active</span>';
  }
  function renderKeys(keys) {
    if (!keys || !keys.length) {
      list.innerHTML = '<tr><td colspan="7">No API keys</td></tr>';
      return;
    }
    list.innerHTML = keys.map(function(key) {
      var disabled = key.revoked_at ? ' disabled' : '';
      return '<tr>' +
        '<td>' + esc(key.id) + '</td>' +
        '<td>' + esc(key.name) + '</td>' +
        '<td><code>' + esc(key.prefix) + '</code></td>' +
        '<td>' + esc((key.permissions || []).join(', ')) + '</td>' +
        '<td>' + esc(key.created_at) + '</td>' +
        '<td>' + keyStatus(key) + '</td>' +
        '<td class="gm-key-actions"><button class="btn btn-xs btn-danger gm-key-revoke" data-id="' + esc(key.id) + '"' + disabled + '><i class="fa fa-ban"></i> Revoke</button></td>' +
      '</tr>';
    }).join('');
  }
  function loadKeys() {
    list.innerHTML = '<tr><td colspan="7">Loading...</td></tr>';
    fetch('/admin/tools/api-keys/list', {headers:{Accept:'application/json'}})
      .then(function(res) {
        return res.json().then(function(data) {
          if (!res.ok) throw new Error(data.error || 'Request failed');
          return data;
        });
      })
      .then(function(data) { renderKeys(data.api_keys || []); })
      .catch(function(err) {
        list.innerHTML = '<tr><td colspan="7">' + esc(err.message) + '</td></tr>';
      });
  }
  function createKey() {
    var name = nameInput.value.trim();
    var expires = expiresValue();
    var permissions = selectedPermissions();
    if (!name) {
      state.className = 'gm-key-state text-danger';
      state.textContent = 'Name is required';
      return;
    }
    if (!permissions.length) {
      state.className = 'gm-key-state text-danger';
      state.textContent = 'Select at least one permission';
      return;
    }
    if (expires === 'invalid') {
      state.className = 'gm-key-state text-danger';
      state.textContent = 'Expires At is invalid';
      return;
    }
    createBtn.disabled = true;
    state.className = 'gm-key-state';
    state.textContent = 'Creating...';
    result.style.display = 'none';
    fetch('/admin/tools/api-keys/create', {
      method: 'POST',
      headers: {'Content-Type':'application/json', Accept:'application/json'},
      body: JSON.stringify({name:name, permissions:permissions, expires_at:expires})
    })
      .then(function(res) {
        return res.json().then(function(data) {
          if (!res.ok) throw new Error(data.error || 'Request failed');
          return data;
        });
      })
      .then(function(data) {
        plain.value = data.key || '';
        result.style.display = 'block';
        state.textContent = 'Created';
        nameInput.value = '';
        loadKeys();
      })
      .catch(function(err) {
        state.className = 'gm-key-state text-danger';
        state.textContent = err.message;
      })
      .finally(function() { createBtn.disabled = false; });
  }
  function revokeKey(id) {
    fetch('/admin/tools/api-keys/revoke', {
      method: 'POST',
      headers: {'Content-Type':'application/json', Accept:'application/json'},
      body: JSON.stringify({id:Number(id)})
    })
      .then(function(res) {
        if (res.status === 204) return;
        return res.json().then(function(data) {
          if (!res.ok) throw new Error(data.error || 'Request failed');
        });
      })
      .then(loadKeys)
      .catch(function(err) {
        state.className = 'gm-key-state text-danger';
        state.textContent = err.message;
      });
  }
  createBtn.addEventListener('click', createKey);
  refreshBtn.addEventListener('click', loadKeys);
  copyBtn.addEventListener('click', function() {
    plain.select();
    navigator.clipboard && navigator.clipboard.writeText(plain.value);
  });
  list.addEventListener('click', function(event) {
    var btn = event.target.closest('.gm-key-revoke');
    if (btn && btn.dataset.id) revokeKey(btn.dataset.id);
  });
  nameInput.addEventListener('keydown', function(event) {
    if (event.key === 'Enter') createKey();
  });
  loadKeys();
})();
</script>`

	return admintypes.Panel{
		Title:       htmltmpl.HTML("API Keys"),
		Description: htmltmpl.HTML("Create, view, and revoke API keys"),
		Content:     htmltmpl.HTML(content),
	}, nil
}

func (a *app) goadminAPIKeysListData(ctx *goadmincontext.Context) {
	keys, err := a.listAPIKeys(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, map[string]interface{}{"error": "failed to list api keys"})
		return
	}
	ctx.JSON(http.StatusOK, map[string]interface{}{"api_keys": keys})
}

func (a *app) goadminAPIKeysCreateData(ctx *goadmincontext.Context) {
	var req createAPIKeyRequest
	if err := decodeJSON(ctx.Request, &req); err != nil {
		ctx.JSON(http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}

	response, err := a.createAPIKey(ctx.Request.Context(), req)
	if err != nil {
		ctx.JSON(mailErrorStatus(err), map[string]interface{}{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusCreated, map[string]interface{}{
		"api_key": response.APIKey,
		"key":     response.Key,
	})
}

func (a *app) goadminAPIKeysRevokeData(ctx *goadmincontext.Context) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := decodeJSON(ctx.Request, &req); err != nil {
		ctx.JSON(http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	if req.ID <= 0 {
		ctx.JSON(http.StatusBadRequest, map[string]interface{}{"error": "invalid api key id"})
		return
	}
	if err := a.revokeAPIKey(ctx.Request.Context(), req.ID); err != nil {
		ctx.JSON(mailErrorStatus(err), map[string]interface{}{"error": err.Error()})
		return
	}
	ctx.Data(http.StatusNoContent, "application/json", nil)
}

func (a *app) goadminIPTool(ctx *goadmincontext.Context) (admintypes.Panel, error) {
	content := `
<style>
.gm-tool-actions{display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap}
.gm-tool-actions .form-group{flex:1 1 280px;margin-bottom:0}
.gm-tool-state{min-height:22px;margin-top:14px;color:#666}
.gm-tool-state.text-danger{color:#dd4b39}
.gm-result-table{margin-top:16px;table-layout:fixed}
.gm-result-table th{width:160px;color:#555}
.gm-result-table td{word-break:break-word}
@media (max-width:767px){.gm-tool-actions{display:block}.gm-tool-actions .btn{margin-top:10px;width:100%}.gm-result-table th{width:120px}}
</style>
<div id="gm-ip-tool">
  <div class="box box-solid">
    <div class="box-header with-border"><h3 class="box-title"><i class="fa fa-map-marker"></i> IP Lookup</h3></div>
    <div class="box-body">
      <div class="gm-tool-actions">
        <div class="form-group">
          <label for="gm-ip-input">IP</label>
          <input id="gm-ip-input" class="form-control" type="text" value="8.8.8.8" autocomplete="off">
        </div>
        <button id="gm-ip-submit" type="button" class="btn btn-primary"><i class="fa fa-search"></i> Query</button>
      </div>
      <div id="gm-ip-state" class="gm-tool-state"></div>
      <table id="gm-ip-result" class="table table-striped gm-result-table" style="display:none">
        <tbody></tbody>
      </table>
    </div>
  </div>
</div>
<script>
(function(){
  var root = document.getElementById('gm-ip-tool');
  if (!root || root.dataset.ready) return;
  root.dataset.ready = '1';
  var input = root.querySelector('#gm-ip-input');
  var button = root.querySelector('#gm-ip-submit');
  var state = root.querySelector('#gm-ip-state');
  var table = root.querySelector('#gm-ip-result');
  var tbody = table.querySelector('tbody');

  function text(value) {
    if (value === undefined || value === null) return '';
    return String(value);
  }
  function esc(value) {
    return text(value).replace(/[&<>"']/g, function(ch) {
      return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch];
    });
  }
  function row(label, value) {
    return '<tr><th>' + esc(label) + '</th><td>' + esc(value) + '</td></tr>';
  }
  function render(data) {
    tbody.innerHTML =
      row('IP', data.ip) +
      row('Country', data.country) +
      row('Country Code', data.country_code) +
      row('Province', data.province) +
      row('Region', data.region) +
      row('City', data.city) +
      row('ISP', data.isp) +
      row('Latitude', data.latitude) +
      row('Longitude', data.longitude) +
      row('Source', data.source);
    table.style.display = '';
  }
  function query() {
    var value = input.value.trim();
    if (!value) {
      state.className = 'gm-tool-state text-danger';
      state.textContent = 'IP is required';
      return;
    }
    button.disabled = true;
    state.className = 'gm-tool-state';
    state.textContent = 'Querying...';
    fetch('/admin/tools/ip/lookup?ip=' + encodeURIComponent(value), {headers:{Accept:'application/json'}})
      .then(function(res) {
        return res.json().then(function(data) {
          if (!res.ok) throw new Error(data.error || 'Request failed');
          return data;
        });
      })
      .then(function(payload) {
        render(payload.data || payload);
        state.textContent = 'Ready';
      })
      .catch(function(err) {
        table.style.display = 'none';
        state.className = 'gm-tool-state text-danger';
        state.textContent = err.message;
      })
      .finally(function() { button.disabled = false; });
  }
  button.addEventListener('click', query);
  input.addEventListener('keydown', function(event) {
    if (event.key === 'Enter') query();
  });
})();
</script>`

	return admintypes.Panel{
		Title:       htmltmpl.HTML("IP Lookup"),
		Description: htmltmpl.HTML("MaxMind and QQWry lookup"),
		Content:     htmltmpl.HTML(content),
	}, nil
}

func (a *app) goadminQRCodeTool(ctx *goadmincontext.Context) (admintypes.Panel, error) {
	content := `
<style>
.gm-qr-grid{display:grid;grid-template-columns:minmax(0,1fr) 320px;gap:18px;align-items:start}
.gm-qr-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}
.gm-qr-fields .full{grid-column:1 / -1}
.gm-qr-preview{min-height:320px;border:1px solid #e5e5e5;background:#fafafa;display:flex;align-items:center;justify-content:center;padding:18px}
.gm-qr-preview img{max-width:100%;height:auto;display:none}
.gm-qr-state{min-height:22px;margin-top:12px;color:#666}
.gm-qr-state.text-danger{color:#dd4b39}
.gm-qr-actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:14px}
@media (max-width:991px){.gm-qr-grid{grid-template-columns:1fr}.gm-qr-preview{min-height:240px}}
@media (max-width:640px){.gm-qr-fields{grid-template-columns:1fr}.gm-qr-actions .btn{width:100%}}
</style>
<div id="gm-qrcode-tool">
  <div class="box box-solid">
    <div class="box-header with-border"><h3 class="box-title"><i class="fa fa-qrcode"></i> QR Code</h3></div>
    <div class="box-body">
      <div class="gm-qr-grid">
        <div>
          <div class="gm-qr-fields">
            <div class="form-group full">
              <label for="gm-qr-content">Content</label>
              <textarea id="gm-qr-content" class="form-control" rows="5">https://example.com</textarea>
            </div>
            <div class="form-group">
              <label for="gm-qr-size">Size</label>
              <input id="gm-qr-size" class="form-control" type="number" min="64" max="2048" step="64" value="512">
            </div>
            <div class="form-group">
              <label for="gm-qr-format">Format</label>
              <select id="gm-qr-format" class="form-control">
                <option value="png">PNG</option>
                <option value="svg">SVG</option>
              </select>
            </div>
            <div class="form-group">
              <label for="gm-qr-fg">Foreground</label>
              <input id="gm-qr-fg" class="form-control" type="color" value="#000000">
            </div>
            <div class="form-group">
              <label for="gm-qr-bg">Background</label>
              <input id="gm-qr-bg" class="form-control" type="color" value="#ffffff">
            </div>
          </div>
          <div class="gm-qr-actions">
            <button id="gm-qr-submit" type="button" class="btn btn-primary"><i class="fa fa-refresh"></i> Generate</button>
            <a id="gm-qr-download" class="btn btn-default disabled" href="#"><i class="fa fa-download"></i> Download</a>
          </div>
          <div id="gm-qr-state" class="gm-qr-state"></div>
        </div>
        <div class="gm-qr-preview">
          <img id="gm-qr-img" alt="QR Code">
        </div>
      </div>
    </div>
  </div>
</div>
<script>
(function(){
  var root = document.getElementById('gm-qrcode-tool');
  if (!root || root.dataset.ready) return;
  root.dataset.ready = '1';
  var content = root.querySelector('#gm-qr-content');
  var size = root.querySelector('#gm-qr-size');
  var format = root.querySelector('#gm-qr-format');
  var fg = root.querySelector('#gm-qr-fg');
  var bg = root.querySelector('#gm-qr-bg');
  var button = root.querySelector('#gm-qr-submit');
  var download = root.querySelector('#gm-qr-download');
  var state = root.querySelector('#gm-qr-state');
  var img = root.querySelector('#gm-qr-img');

  function buildURL() {
    var params = new URLSearchParams();
    params.set('content', content.value.trim());
    params.set('size', size.value || '512');
    params.set('format', format.value || 'png');
    params.set('fg_color', fg.value || '#000000');
    params.set('bg_color', bg.value || '#ffffff');
    return '/admin/tools/qrcode/render?' + params.toString();
  }
  function render() {
    if (!content.value.trim()) {
      state.className = 'gm-qr-state text-danger';
      state.textContent = 'Content is required';
      return;
    }
    var url = buildURL();
    button.disabled = true;
    state.className = 'gm-qr-state';
    state.textContent = 'Generating...';
    img.onload = function() {
      img.style.display = 'block';
      download.href = url;
      download.download = 'qrcode.' + (format.value || 'png');
      download.classList.remove('disabled');
      state.textContent = 'Ready';
      button.disabled = false;
    };
    img.onerror = function() {
      img.style.display = 'none';
      download.classList.add('disabled');
      state.className = 'gm-qr-state text-danger';
      state.textContent = 'Request failed';
      button.disabled = false;
    };
    img.src = url;
  }
  button.addEventListener('click', render);
  content.addEventListener('keydown', function(event) {
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) render();
  });
  render();
})();
</script>`

	return admintypes.Panel{
		Title:       htmltmpl.HTML("QR Code"),
		Description: htmltmpl.HTML("PNG and SVG generator"),
		Content:     htmltmpl.HTML(content),
	}, nil
}

func (a *app) goadminIPLookupData(ctx *goadmincontext.Context) {
	ip := strings.TrimSpace(ctx.Request.URL.Query().Get("ip"))
	if ip == "" {
		ctx.JSON(http.StatusBadRequest, map[string]interface{}{"error": "ip is required"})
		return
	}

	result, err := a.ipLookup.query(ip)
	if err != nil {
		ctx.JSON(mailErrorStatus(err), map[string]interface{}{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, map[string]interface{}{"data": result})
}

func (a *app) goadminQRCodeData(ctx *goadmincontext.Context) {
	req, err := decodeQRCodeRequest(ctx.Request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}

	image, err := a.qrCodeImage(req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, map[string]interface{}{"error": "failed to generate qrcode"})
		return
	}

	ctx.DataWithHeaders(http.StatusOK, map[string]string{
		"Content-Type":           image.ContentType,
		"Content-Disposition":    `inline; filename="` + image.Filename + `"`,
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, max-age=300",
	}, image.Data)
}

func (a *app) smtpConfigTable(ctx *goadmincontext.Context) admintable.Table {
	tbl := admintable.NewDefaultTable(ctx, admintable.DefaultConfigWithDriver(goadmindb.DriverSqlite).
		SetCanAdd(true).
		SetEditable(true).
		SetDeletable(false).
		SetExportable(false))

	info := tbl.GetInfo().HideFilterArea()
	info.AddField("ID", "id", goadmindb.Int).FieldSortable()
	info.AddField("Host", "host", goadmindb.Varchar)
	info.AddField("Port", "port", goadmindb.Int)
	info.AddField("Username", "username", goadmindb.Varchar)
	info.AddField("Password", "password", goadmindb.Varchar).FieldDisplay(func(value admintypes.FieldModel) interface{} {
		if strings.TrimSpace(value.Value) == "" {
			return ""
		}
		return "******"
	})
	info.AddField("From Email", "from_email", goadmindb.Varchar)
	info.AddField("From Name", "from_name", goadmindb.Varchar)
	info.AddField("Encryption", "encryption", goadmindb.Varchar)
	info.AddField("Updated At", "updated_at", goadmindb.Timestamp)
	info.SetTable("smtp_config").SetTitle("SMTP Config").SetDescription("SMTP delivery settings")

	formList := tbl.GetForm()
	formList.AddField("ID", "id", goadmindb.Int, admintypeform.Default).
		FieldDisplayButCanNotEditWhenUpdate().
		FieldDisableWhenCreate()
	formList.AddField("Host", "host", goadmindb.Varchar, admintypeform.Text).FieldMust()
	formList.AddField("Port", "port", goadmindb.Int, admintypeform.Number).FieldMust()
	formList.AddField("Username", "username", goadmindb.Varchar, admintypeform.Text)
	formList.AddField("Password", "password", goadmindb.Varchar, admintypeform.Password).
		FieldDisplay(func(value admintypes.FieldModel) interface{} { return "" }).
		FieldHelpMsg(htmltmpl.HTML("Leave empty when editing to keep the current password.")).
		FieldPostFilterFn(func(value admintypes.PostFieldModel) interface{} {
			password := value.Value.Value()
			if value.IsUpdate() && strings.TrimSpace(password) == "" {
				current, _, ok, err := a.getSMTPConfig(context.Background())
				if err == nil && ok {
					return current.Password
				}
			}
			return password
		})
	formList.AddField("From Email", "from_email", goadmindb.Varchar, admintypeform.Email).FieldMust()
	formList.AddField("From Name", "from_name", goadmindb.Varchar, admintypeform.Text)
	formList.AddField("Encryption", "encryption", goadmindb.Varchar, admintypeform.SelectSingle).
		FieldOptions(admintypes.FieldOptions{
			{Text: "STARTTLS", Value: "starttls"},
			{Text: "TLS", Value: "tls"},
			{Text: "None", Value: "none"},
		}).
		FieldDefault("starttls")
	formList.SetTable("smtp_config").SetTitle("SMTP Config").SetDescription("SMTP delivery settings").
		SetPreProcessFn(func(values adminformvalues.Values) adminformvalues.Values {
			if values.Get("id") == "" {
				values.Add("id", "1")
			}
			values.Add("updated_at", a.now().Format(time.RFC3339))
			return values
		}).
		SetPostValidator(func(values adminformvalues.Values) error {
			return a.validateSMTPAdminValues(values)
		}).
		SetPostHook(func(values adminformvalues.Values) error { return nil }).
		EnableAjax("Saved", "Save failed")

	return tbl
}

func (a *app) apiKeysTable(ctx *goadmincontext.Context) admintable.Table {
	tbl := admintable.NewDefaultTable(ctx, admintable.DefaultConfigWithDriver(goadmindb.DriverSqlite).
		SetCanAdd(false).
		SetEditable(false).
		SetDeletable(false).
		SetExportable(false))

	info := tbl.GetInfo().HideFilterArea()
	info.AddField("ID", "id", goadmindb.Int).FieldSortable()
	info.AddField("Name", "name", goadmindb.Varchar)
	info.AddField("Prefix", "key_prefix", goadmindb.Varchar)
	info.AddField("Permissions", "permissions", goadmindb.Varchar)
	info.AddField("Created At", "created_at", goadmindb.Timestamp)
	info.AddField("Expires At", "expires_at", goadmindb.Timestamp)
	info.AddField("Revoked At", "revoked_at", goadmindb.Timestamp)
	info.AddField("Last Used At", "last_used_at", goadmindb.Timestamp)
	info.SetTable("api_keys").SetTitle("API Keys").SetDescription("Read-only API key inventory")

	formList := tbl.GetForm()
	formList.AddField("ID", "id", goadmindb.Int, admintypeform.Default).FieldDisplayButCanNotEditWhenUpdate()
	formList.AddField("Name", "name", goadmindb.Varchar, admintypeform.Text)
	formList.AddField("Prefix", "key_prefix", goadmindb.Varchar, admintypeform.Text)
	formList.AddField("Permissions", "permissions", goadmindb.Text, admintypeform.Text)
	formList.AddField("Created At", "created_at", goadmindb.Timestamp, admintypeform.Datetime)
	formList.AddField("Expires At", "expires_at", goadmindb.Timestamp, admintypeform.Datetime)
	formList.AddField("Revoked At", "revoked_at", goadmindb.Timestamp, admintypeform.Datetime)
	formList.AddField("Last Used At", "last_used_at", goadmindb.Timestamp, admintypeform.Datetime)
	formList.SetTable("api_keys").SetTitle("API Keys").SetDescription("Read-only API key inventory")

	return tbl
}

func (a *app) emailLogsTable(ctx *goadmincontext.Context) admintable.Table {
	tbl := admintable.NewDefaultTable(ctx, admintable.DefaultConfigWithDriver(goadmindb.DriverSqlite).
		SetCanAdd(false).
		SetEditable(false).
		SetDeletable(false).
		SetExportable(false))

	info := tbl.GetInfo().HideFilterArea()
	info.AddField("ID", "id", goadmindb.Int).FieldSortable()
	info.AddField("API Key ID", "api_key_id", goadmindb.Int)
	info.AddField("Subject", "subject", goadmindb.Varchar)
	info.AddField("Recipient Count", "recipient_count", goadmindb.Int)
	info.AddField("Status", "status", goadmindb.Varchar)
	info.AddField("Error", "error", goadmindb.Text)
	info.AddField("Created At", "created_at", goadmindb.Timestamp)
	info.SetTable("email_logs").SetTitle("Email Logs").SetDescription("SMTP delivery logs")

	formList := tbl.GetForm()
	formList.AddField("ID", "id", goadmindb.Int, admintypeform.Default)
	formList.AddField("API Key ID", "api_key_id", goadmindb.Int, admintypeform.Number)
	formList.AddField("Subject", "subject", goadmindb.Varchar, admintypeform.Text)
	formList.AddField("Recipient Count", "recipient_count", goadmindb.Int, admintypeform.Number)
	formList.AddField("Status", "status", goadmindb.Varchar, admintypeform.Text)
	formList.AddField("Error", "error", goadmindb.Text, admintypeform.Text)
	formList.AddField("Created At", "created_at", goadmindb.Timestamp, admintypeform.Datetime)
	formList.SetTable("email_logs").SetTitle("Email Logs").SetDescription("SMTP delivery logs")

	return tbl
}

func (a *app) validateSMTPAdminValues(values adminformvalues.Values) error {
	host := strings.TrimSpace(values.Get("host"))
	username := strings.TrimSpace(values.Get("username"))
	password := values.Get("password")
	port, err := strconv.Atoi(strings.TrimSpace(values.Get("port")))
	if err != nil {
		return errors.New("port must be a number")
	}

	current, _, exists, err := a.getSMTPConfig(context.Background())
	if err != nil {
		return err
	}
	if !exists {
		current = smtpConfig{}
	}

	if values.IsUpdatePost() && strings.TrimSpace(password) == "" {
		password = current.Password
	}

	cfg := smtpConfig{
		Host:       host,
		Port:       port,
		Username:   username,
		Password:   password,
		FromEmail:  strings.TrimSpace(values.Get("from_email")),
		FromName:   strings.TrimSpace(values.Get("from_name")),
		Encryption: strings.TrimSpace(values.Get("encryption")),
	}
	if cfg.Encryption == "" {
		cfg.Encryption = "starttls"
	}

	return validateSMTPConfig(cfg)
}

func execStatements(ctx context.Context, db *sql.DB, stmts []string) error {
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
