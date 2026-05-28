package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// WeblogByID returns the weblog with the given id; ErrNotFound if missing.
func (s *Store) WeblogByID(ctx context.Context, id int64) (*domain.Weblog, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, description, base_url, lang, comment_mode, spam_words, ip_blacklist, llms_enabled,
		       auto_rebuild_on_publish,
		       og_bg_image_path, og_text_color,
		       archive_template_id, profile_template_id,
		       date_format_entry, time_format_entry, date_format_comment,
		       date_format_list, date_format_archive,
		       entries_per_page, entry_sort_order, comment_sort_order,
		       sitemap_enabled, robots_enabled,
		       static_search_form_enabled
		FROM weblogs WHERE id = ?`, id)
	w := &domain.Weblog{}
	var mode string
	var llmsEnabled, autoRebuild, sitemapEnabled, robotsEnabled, staticSearchForm int
	if err := row.Scan(&w.ID, &w.Title, &w.Description, &w.BaseURL, &w.Lang, &mode, &w.SpamWords, &w.IPBlacklist, &llmsEnabled,
		&autoRebuild,
		&w.OGBGImagePath, &w.OGTextColor,
		&w.ArchiveTemplateID, &w.ProfileTemplateID,
		&w.DateFormatEntry, &w.TimeFormatEntry, &w.DateFormatComment,
		&w.DateFormatList, &w.DateFormatArchive,
		&w.EntriesPerPage, &w.EntrySortOrder, &w.CommentSortOrder,
		&sitemapEnabled, &robotsEnabled,
		&staticSearchForm); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: WeblogByID: %w", err)
	}
	w.CommentMode = domain.CommentMode(mode)
	if !w.CommentMode.Valid() {
		w.CommentMode = domain.CommentModerated
	}
	w.LLMSEnabled = llmsEnabled != 0
	w.AutoRebuildOnPublish = autoRebuild != 0
	w.SitemapEnabled = sitemapEnabled != 0
	w.RobotsEnabled = robotsEnabled != 0
	w.StaticSearchFormEnabled = staticSearchForm != 0
	return w, nil
}

// UpdateWeblogDesign sets just the design-settings columns so the
// settings form can stay narrow and the archive/profile tabs don't need
// to re-submit every weblog field. Leaves the rest untouched.
func (s *Store) UpdateWeblogDesign(ctx context.Context, wid, archiveID, profileID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE weblogs SET archive_template_id = ?, profile_template_id = ?
		WHERE id = ?`, archiveID, profileID, wid)
	if err != nil {
		return fmt.Errorf("repo: UpdateWeblogDesign: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateWeblogDisplaySettings persists the count + sort knobs edited on
// the design-settings 表示件数 / 並び順 section. Callers normalise the
// sort-order strings before calling so "desc" / "asc" are the only
// values that ever land here.
func (s *Store) UpdateWeblogDisplaySettings(ctx context.Context, wid int64, entriesPerPage int, entrySort, commentSort string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE weblogs SET
			entries_per_page   = ?,
			entry_sort_order   = ?,
			comment_sort_order = ?
		WHERE id = ?`, entriesPerPage, entrySort, commentSort, wid)
	if err != nil {
		return fmt.Errorf("repo: UpdateWeblogDisplaySettings: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateWeblogDateFormats persists the five date-format pattern strings
// edited on the design-settings 時刻表記 tab. Empty strings are stored
// as-is so the reader path can resolve "unset" to the package defaults
// without a second flag column.
func (s *Store) UpdateWeblogDateFormats(ctx context.Context, wid int64, entry, entryTime, comment, list, archive string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE weblogs SET
			date_format_entry   = ?,
			time_format_entry   = ?,
			date_format_comment = ?,
			date_format_list    = ?,
			date_format_archive = ?
		WHERE id = ?`, entry, entryTime, comment, list, archive, wid)
	if err != nil {
		return fmt.Errorf("repo: UpdateWeblogDateFormats: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateWeblog rewrites every user-editable field on the weblog row. The
// caller hands over a full Weblog struct because the admin settings form
// always submits every field together — no partial updates to worry about.
func (s *Store) UpdateWeblog(ctx context.Context, w domain.Weblog) error {
	llms := 0
	if w.LLMSEnabled {
		llms = 1
	}
	autoRebuild := 0
	if w.AutoRebuildOnPublish {
		autoRebuild = 1
	}
	sitemap := 0
	if w.SitemapEnabled {
		sitemap = 1
	}
	robots := 0
	if w.RobotsEnabled {
		robots = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE weblogs SET
			title = ?, description = ?, base_url = ?, lang = ?,
			comment_mode = ?, spam_words = ?, ip_blacklist = ?, llms_enabled = ?,
			auto_rebuild_on_publish = ?,
			og_bg_image_path = ?, og_text_color = ?,
			sitemap_enabled = ?, robots_enabled = ?
		WHERE id = ?`,
		w.Title, w.Description, w.BaseURL, w.Lang,
		string(w.CommentMode), w.SpamWords, w.IPBlacklist, llms,
		autoRebuild,
		w.OGBGImagePath, w.OGTextColor,
		sitemap, robots,
		w.ID)
	if err != nil {
		return fmt.Errorf("repo: UpdateWeblog: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
