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
		{101, 1, "SMTP Config", "fa-cog", "/info/smtp_config"},
		{102, 2, "API Keys", "fa-key", "/info/api_keys"},
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
	cfg := goadminconfig.Config{
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
		IndexUrl:                      "/admin",
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

	admin := adminplugin.NewAdmin(a.goadminGenerators())
	if err := eng.AddConfig(&cfg).
		AddPlugins(admin).
		AddDisplayFilterXssJsFilter().
		Use(router); err != nil {
		return err
	}

	eng.HTML("GET", "/admin", a.goadminDashboard)
	eng.HTML("GET", "/admin/tools/ip", a.goadminIPTool)
	eng.HTML("GET", "/admin/tools/qrcode", a.goadminQRCodeTool)
	eng.Data("GET", "/admin/tools/ip/lookup", a.goadminIPLookupData)
	eng.Data("GET", "/admin/tools/qrcode/render", a.goadminQRCodeData)
	eng.Data("POST", "/admin/tools/qrcode/render", a.goadminQRCodeData)
	return nil
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
      <a href="/admin/info/smtp_config" class="small-box-footer">Open <i class="fa fa-arrow-circle-right"></i></a>
    </div>
  </div>
  <div class="col-lg-2 col-sm-4 col-xs-6">
    <div class="small-box bg-green">
      <div class="inner"><h3>%d</h3><p>API Keys</p></div>
      <div class="icon"><i class="fa fa-key"></i></div>
      <a href="/admin/info/api_keys" class="small-box-footer">Open <i class="fa fa-arrow-circle-right"></i></a>
    </div>
  </div>
  <div class="col-lg-2 col-sm-4 col-xs-6">
    <div class="small-box bg-yellow">
      <div class="inner"><h3>%d</h3><p>Active Keys</p></div>
      <div class="icon"><i class="fa fa-shield"></i></div>
      <a href="/admin/info/api_keys" class="small-box-footer">Open <i class="fa fa-arrow-circle-right"></i></a>
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
        <a class="btn btn-app" href="/admin/info/smtp_config"><i class="fa fa-cog"></i> SMTP</a>
        <a class="btn btn-app" href="/admin/info/api_keys"><i class="fa fa-key"></i> Keys</a>
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
