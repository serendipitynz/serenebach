package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/auth"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/web/templates"
)

// ErrAdminAlreadyExists signals that seed wanted to create the
// bootstrap administrator but found one already present. The
// in-process /setup gate uses a mutex to keep concurrent POSTs from
// reaching this point twice, but in CGI mode each request is its
// own process — the mutex doesn't span them — so the admin INSERT
// itself has to be the synchronisation point. SQLite serialises
// writes, and the conditional INSERT below is evaluated under that
// write lock, so exactly one process commits the row and the rest
// see RowsAffected == 0 and surface this error. /setup translates
// it to admin.ErrSetupAlreadyDone (404); the CLI seed swallows it
// because re-runs are expected to be idempotent.
var ErrAdminAlreadyExists = errors.New("seed: admin already exists")

type SeedSpec struct {
	AdminName     string
	AdminPassword string
	AdminEmail    string
	WeblogTitle   string
	WeblogDesc    string
	WeblogBaseURL string
	WeblogLang    string
	TemplateName  string
	// SampleEntries inserts demo content on the first run so a freshly
	// initialised site shows something on the home page. Skipped on reruns.
	SampleEntries bool
}

func DefaultSeed() SeedSpec {
	return SeedSpec{
		AdminName:     "admin",
		AdminPassword: "changeme",
		AdminEmail:    "",
		WeblogTitle:   "Serene Bach",
		WeblogDesc:    "a fresh install",
		WeblogBaseURL: "",
		WeblogLang:    "ja",
		TemplateName:  "default",
		SampleEntries: true,
	}
}

// Seed populates the database with the minimum rows needed for a public page
// to render: one weblog, one admin user, one active template, and (on first
// run) a handful of sample entries. It is safe to run repeatedly — every
// step skips work that has already been done.
func (a *App) Seed(ctx context.Context, spec SeedSpec) error {
	now := time.Now().Unix()
	wid := DefaultWID

	// Admin INSERT runs first as the install-claim sentinel: its
	// WHERE NOT EXISTS guard is the cross-process race winner. Only
	// the winner proceeds to write the weblog / template / samples,
	// so a CGI race loser cannot pollute durable initial settings
	// (e.g. weblog title) with its submission before realising it
	// lost the admin INSERT.
	if err := a.seedAdminUser(ctx, wid, spec, now); err != nil {
		return err
	}
	if err := a.seedWeblog(ctx, wid, spec, now); err != nil {
		return err
	}
	if err := a.seedDefaultTemplate(ctx, wid, spec, now); err != nil {
		return err
	}
	if spec.SampleEntries {
		if err := a.seedSampleContent(ctx, wid, now); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) seedWeblog(ctx context.Context, wid int64, spec SeedSpec, now int64) error {
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO weblogs (id, title, description, base_url, lang, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		wid, spec.WeblogTitle, spec.WeblogDesc, spec.WeblogBaseURL, spec.WeblogLang, now, now)
	if err != nil {
		return fmt.Errorf("seed: weblog: %w", err)
	}
	return nil
}

func (a *App) seedAdminUser(ctx context.Context, wid int64, spec SeedSpec, now int64) error {
	// Same-name re-runs of `task seed` stay fully idempotent: if a
	// user with the requested admin name already exists, return nil
	// without touching the row. Different-name re-runs (or a CGI
	// race loser whose proposed name isn't in the DB yet) fall
	// through to the atomic INSERT below.
	var exists int
	if err := a.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE name = ?`, spec.AdminName,
	).Scan(&exists); err != nil {
		return fmt.Errorf("seed: check admin: %w", err)
	}
	if exists > 0 {
		return nil
	}

	hash, err := auth.HashPassword(spec.AdminPassword)
	if err != nil {
		return fmt.Errorf("seed: hash password: %w", err)
	}
	// role=RoleAdmin. The bootstrap admin must carry the admin role
	// explicitly so CanManageUsers / CanManageDesign pass on every
	// request. description_format defaults to "html" for the profile
	// renderer. The WHERE NOT EXISTS guard makes this insert
	// atomic with respect to "any admin already exists" — see the
	// ErrAdminAlreadyExists doc comment for why this matters under
	// CGI deployments.
	res, err := a.DB.ExecContext(ctx, `
		INSERT INTO users (wid, name, display_name, email, password_hash, role,
		                   list_visible, description_format,
		                   created_at, updated_at)
		SELECT ?, ?, ?, ?, ?, ?, 1, 'html', ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM users WHERE role = ?)`,
		wid, spec.AdminName, spec.AdminName, spec.AdminEmail, hash, domain.RoleAdmin, now, now, domain.RoleAdmin)
	if err != nil {
		return fmt.Errorf("seed: insert admin: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("seed: rows affected: %w", err)
	}
	if n == 0 {
		return ErrAdminAlreadyExists
	}
	return nil
}

// seedDefaultTemplate installs the embedded default template as the active
// template for the weblog, but only if no active template already exists —
// it must never clobber a template the user has customised.
func (a *App) seedDefaultTemplate(ctx context.Context, wid int64, spec SeedSpec, now int64) error {
	var existingID int64
	err := a.DB.QueryRowContext(ctx,
		`SELECT id FROM templates WHERE wid = ? AND is_active = 1 LIMIT 1`, wid,
	).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("seed: check template: %w", err)
	}
	if existingID != 0 {
		return nil
	}

	if _, err := a.DB.ExecContext(ctx, `
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, created_at, updated_at)
		VALUES (?, ?, 1, ?, '', ?, 'bundled default template', ?, ?)`,
		wid, spec.TemplateName, templates.DefaultMain, templates.DefaultCSS, now, now); err != nil {
		return fmt.Errorf("seed: insert template: %w", err)
	}
	return nil
}

// seedSampleContent adds a category and two published entries so the home
// page has something to display. Runs only when both tables are empty, so
// it never interferes with real data.
func (a *App) seedSampleContent(ctx context.Context, wid int64, now int64) error {
	var catCount, entryCount int
	if err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM categories WHERE wid = ?`, wid).Scan(&catCount); err != nil {
		return fmt.Errorf("seed: count categories: %w", err)
	}
	if err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE wid = ?`, wid).Scan(&entryCount); err != nil {
		return fmt.Errorf("seed: count entries: %w", err)
	}
	if catCount > 0 || entryCount > 0 {
		return nil
	}

	res, err := a.DB.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, created_at, updated_at)
		VALUES (?, 0, 'お知らせ', 'news', 0, ?, ?)`, wid, now, now)
	if err != nil {
		return fmt.Errorf("seed: insert category: %w", err)
	}
	catID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("seed: lastid category: %w", err)
	}

	var adminID int64
	if err := a.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE wid = ? ORDER BY id LIMIT 1`, wid).Scan(&adminID); err != nil {
		return fmt.Errorf("seed: locate admin: %w", err)
	}

	samples := []struct {
		title string
		body  string
		more  string
		ago   time.Duration
	}{
		{
			title: "ようこそ Serene Bach へ",
			body:  "<p>このページは seed で投入されたサンプル投稿です。管理画面から自由に書き換えてください。</p>",
			more:  "",
			ago:   0,
		},
		{
			title: "カテゴリとテンプレートについて",
			body:  "<p>SB3 から引き継いだマルチテンプレート・多カテゴリ構造をそのまま維持しています。</p>",
			more:  "<p>詳細は追記に。</p>",
			ago:   24 * time.Hour,
		},
	}
	for _, s := range samples {
		posted := time.Now().Add(-s.ago).Unix()
		if _, err := a.DB.ExecContext(ctx, `
			INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
			wid, adminID, catID, s.title, s.body, s.more, domain.EntryPublished, posted, posted, posted); err != nil {
			return fmt.Errorf("seed: insert entry: %w", err)
		}
	}
	return nil
}
