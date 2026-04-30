package importer

// SB2 importer — reads a Serene Bach 2 flat-file data directory and
// writes the same destination schema the SB3 importer uses. SB2 stored
// every record in a TAB-separated text file (one row per record, with
// `\t` / `\n` / `\\` backslash escapes). The driver in
// _sandbox/sb2/lib/sb/Driver/Text.pm implements both an "index" file
// per record class (data/entry.cgi, data/message.cgi, …) carrying a
// subset of fields, and per-id "detail" files under
// data/{class}/{id}.cgi carrying every field. The detail files are the
// authoritative source for record bodies; this importer reads detail
// files directly and ignores the index files.
//
// Field orderings come straight from _sandbox/sb2/lib/sb/Data/*.pm
// (the elements() method on each Data class). Encoding is auto-detected
// (typically EUC-JP) per record file via internal/jacharset.

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/jacharset"
)

// importSB2 walks dataDir, parses every flat-file table the destination
// schema cares about, and writes the result inside a single
// destination transaction. Trackbacks, users, and links are not
// imported (matching SB3 importer policy); user-authored content is
// attributed to opts.AuthorID like SB3.
func importSB2(ctx context.Context, dest *sql.DB, dataDir string, opts Options) (*Report, error) {
	absDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("importer: abs path: %w", err)
	}
	if st, err := os.Stat(absDir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("importer: SB2 data directory not found: %s", absDir)
	}

	// Read every source-side table up front. Each helper is read-only
	// and returns an error only on truly unexpected failure (missing
	// file is OK for tables that may legitimately be absent — e.g. a
	// blog with no comments has no message/ directory).
	weblog, err := readSB2Weblog(absDir)
	if err != nil {
		return nil, err
	}
	cats, err := readSB2Categories(absDir)
	if err != nil {
		return nil, err
	}
	entries, err := readSB2Entries(absDir, opts.OnlyPublished)
	if err != nil {
		return nil, err
	}
	messages, err := readSB2Messages(absDir)
	if err != nil {
		return nil, err
	}
	templates, err := readSB2Templates(absDir)
	if err != nil {
		return nil, err
	}

	tx, err := dest.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("importer: begin tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	report := &Report{}

	var authorName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, opts.AuthorID).Scan(&authorName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("importer: AuthorID %d does not exist in destination — seed an admin user first", opts.AuthorID)
		}
		return nil, fmt.Errorf("importer: check author: %w", err)
	}

	// configure.cgi → legacy_*. Reuse the SB3 path; SB2 uses the same
	// InitParser format with the same key names. The sb_config table
	// arg is unused (no SQLite source) so pass nil and a 0 wid.
	cfg, err := loadLegacyConfig(ctx, nil, 0, absDir)
	if err != nil {
		return nil, err
	}

	if weblog != nil {
		now := time.Now().Unix()
		if _, err := tx.ExecContext(ctx, `
			UPDATE weblogs SET title = ?, description = ?, updated_at = ? WHERE id = ?`,
			weblog.Title, weblog.Description, now, opts.TargetWID); err != nil {
			return nil, fmt.Errorf("importer: update weblog: %w", err)
		}
		report.WeblogUpdated = true
	} else {
		report.Warnings = append(report.Warnings, "SB2 weblog.cgi missing or empty; leaving destination weblog untouched")
	}
	if err := applyLegacyConfig(ctx, tx, opts.TargetWID, cfg); err != nil {
		return nil, err
	}

	if err := importSB2Templates(ctx, tx, templates, opts, report); err != nil {
		return nil, err
	}
	catMap, err := importSB2Categories(ctx, tx, cats, opts, report)
	if err != nil {
		return nil, err
	}
	entryMap, err := importSB2Entries(ctx, tx, entries, catMap, opts, report)
	if err != nil {
		return nil, err
	}
	if err := importSB2Messages(ctx, tx, messages, entryMap, opts, report); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("importer: commit: %w", err)
	}
	tx = nil
	return report, nil
}

// =============================================================================
// SB2 record types & flat-file parsing
// =============================================================================

// sb2Entry mirrors _sandbox/sb2/lib/sb/Data/Entry.pm elements() in
// declared order. Only fields the destination schema cares about are
// retained as their own struct members; the rest decode into Extra so
// future use cases can pull from them without re-parsing.
type sb2Entry struct {
	ID, WID, Cat, Date, Auth, Stat int64
	Subj, File, TZ, Form           string
	Body, More, Sum, Key           string
}

type sb2Category struct {
	ID, WID, Main, Order int64
	Name, Text, Dir      string
}

type sb2Message struct {
	ID, WID, EID, Stat, Date               int64
	Auth, Host, TZ, Mail, URL, Agent, Body string
}

type sb2Template struct {
	ID, WID, Use, Gen, Mod          int64
	Name, Info, Main, CSS, EntryTpl string
}

type sb2Weblog struct {
	ID                 int64
	Title, Description string
}

// readSB2Weblog reads data/weblog.cgi. SB2 stores at most a few rows
// (multi-blog support); we keep the first row, mirroring the SB3
// importer's "import the first weblog" policy.
func readSB2Weblog(dir string) (*sb2Weblog, error) {
	rows, err := readSB2Records(filepath.Join(dir, "weblog.cgi"))
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	r := rows[0]
	w := &sb2Weblog{
		ID:          atoi64(at(r, 0)),
		Title:       at(r, 1),
		Description: at(r, 2),
	}
	return w, nil
}

// readSB2Categories reads data/category.cgi. Categories are list-only
// in SB2 (no per-id detail file), so the .cgi row already carries
// every field we need.
func readSB2Categories(dir string) ([]sb2Category, error) {
	rows, err := readSB2Records(filepath.Join(dir, "category.cgi"))
	if err != nil {
		return nil, err
	}
	out := make([]sb2Category, 0, len(rows))
	for _, r := range rows {
		// elements: id, wid, name, text, url, main, order, temp, dir, disp, sub, num, idx
		out = append(out, sb2Category{
			ID:    atoi64(at(r, 0)),
			WID:   atoi64(at(r, 1)),
			Name:  at(r, 2),
			Text:  at(r, 3),
			Main:  atoi64(at(r, 5)),
			Order: atoi64(at(r, 6)),
			Dir:   at(r, 8),
		})
	}
	return out, nil
}

// readSB2Entries walks data/entry/*.cgi and decodes every detail file.
// The index file (data/entry.cgi) is intentionally skipped: it carries
// only a subset of fields, and walking the directory is simpler.
//
// onlyPublished, when true, drops rows whose stat is not in {1,2} —
// SB2's published-list condition (lib/sb/Build.pm uses cond stat=>[1,2]).
func readSB2Entries(dir string, onlyPublished bool) ([]sb2Entry, error) {
	files, err := os.ReadDir(filepath.Join(dir, "entry"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("importer: read SB2 entry dir: %w", err)
	}
	out := make([]sb2Entry, 0, len(files))
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".cgi") {
			continue
		}
		rows, err := readSB2Records(filepath.Join(dir, "entry", f.Name()))
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		r := rows[0]
		// elements: id, wid, subj, cat, date, auth, stat, com, tb, file,
		// tz, add, edit, acm, atb, form, ping, body, more, sum, key,
		// ext, tmp
		stat := atoi64(at(r, 6))
		if onlyPublished && stat != 1 && stat != 2 {
			continue
		}
		out = append(out, sb2Entry{
			ID:   atoi64(at(r, 0)),
			WID:  atoi64(at(r, 1)),
			Subj: at(r, 2),
			Cat:  atoi64(at(r, 3)),
			Date: atoi64(at(r, 4)),
			Auth: atoi64(at(r, 5)),
			Stat: stat,
			File: at(r, 9),
			TZ:   at(r, 10),
			Form: at(r, 15),
			Body: at(r, 17),
			More: at(r, 18),
			Sum:  at(r, 19),
			Key:  at(r, 20),
		})
	}
	// Sort by ID so import order is deterministic. SB2's directory
	// scan returns entries in lexical filename order which is *not*
	// numeric ("100.cgi" < "11.cgi"); without sorting the report's
	// per-row diagnostics would be confusing.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// readSB2Messages walks data/message/*.cgi for full comment records.
func readSB2Messages(dir string) ([]sb2Message, error) {
	files, err := os.ReadDir(filepath.Join(dir, "message"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("importer: read SB2 message dir: %w", err)
	}
	out := make([]sb2Message, 0, len(files))
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".cgi") {
			continue
		}
		rows, err := readSB2Records(filepath.Join(dir, "message", f.Name()))
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		r := rows[0]
		// elements: id, wid, eid, stat, date, auth, host, tz, mail,
		// url, agnt, body, icon, ext, admn, out
		out = append(out, sb2Message{
			ID:    atoi64(at(r, 0)),
			WID:   atoi64(at(r, 1)),
			EID:   atoi64(at(r, 2)),
			Stat:  atoi64(at(r, 3)),
			Date:  atoi64(at(r, 4)),
			Auth:  at(r, 5),
			Host:  at(r, 6),
			TZ:    at(r, 7),
			Mail:  at(r, 8),
			URL:   at(r, 9),
			Agent: at(r, 10),
			Body:  at(r, 11),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// readSB2Templates walks data/template/*.cgi.
func readSB2Templates(dir string) ([]sb2Template, error) {
	files, err := os.ReadDir(filepath.Join(dir, "template"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("importer: read SB2 template dir: %w", err)
	}
	out := make([]sb2Template, 0, len(files))
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".cgi") {
			continue
		}
		rows, err := readSB2Records(filepath.Join(dir, "template", f.Name()))
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		r := rows[0]
		// elements: id, wid, use, name, gen, mod, info, main, css,
		// entry, files
		out = append(out, sb2Template{
			ID:       atoi64(at(r, 0)),
			WID:      atoi64(at(r, 1)),
			Use:      atoi64(at(r, 2)),
			Name:     at(r, 3),
			Gen:      atoi64(at(r, 4)),
			Mod:      atoi64(at(r, 5)),
			Info:     at(r, 6),
			Main:     at(r, 7),
			CSS:      at(r, 8),
			EntryTpl: at(r, 9),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// readSB2Records reads path as a TAB-separated SB2 record file. Each
// line yields one []string row, with the per-field `\t` / `\n` / `\\`
// backslash escapes decoded. The file is run through jacharset to
// auto-promote non-UTF-8 (typically EUC-JP) input.
//
// A missing file is not an error — it returns a nil slice. SB2 blogs
// without any comments, for instance, won't have data/message.cgi.
func readSB2Records(path string) ([][]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("importer: read %s: %w", path, err)
	}
	text, _ := jacharset.DecodeToUTF8(raw, "", jacharset.KindPlain)
	var out [][]string
	sc := bufio.NewScanner(strings.NewReader(text))
	// Bodies can be long after `\n` decoding; size the buffer up
	// front so a 5MB entry doesn't blow scanner's default 64KB ceiling.
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		// SB2 _encode appends a trailing \t before the newline so
		// every encoded row ends with an empty trailing field. Split
		// keeps it; downstream consumers ignore extras via at().
		fields := strings.Split(line, "\t")
		for i, f := range fields {
			fields[i] = decodeSB2Escapes(f)
		}
		out = append(out, fields)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("importer: scan %s: %w", path, err)
	}
	return out, nil
}

// decodeSB2Escapes reverses Driver::Text._encode: \t → tab, \n →
// newline, \\ → backslash, \X → X. Same behaviour as initparser.decode
// but kept local to avoid exporting that helper.
func decodeSB2Escapes(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		next := s[i+1]
		switch next {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		default:
			b.WriteByte(next)
		}
		i++
	}
	return b.String()
}

// at returns the i'th element of row, or "" when the row is shorter.
// SB2 sometimes pads short rows with fewer fields (notably when later
// fields were added in a later schema version), so robust indexing
// matters more than panic-on-bounds.
func at(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// =============================================================================
// SB2 → destination writers
// =============================================================================

// importSB2Templates inserts every SB2 template as inactive — same
// policy as the SB3 importer. Lint warnings surface in the report so
// the operator can review unsupported tags before activating.
func importSB2Templates(ctx context.Context, tx *sql.Tx, templates []sb2Template, opts Options, report *Report) error {
	now := time.Now().Unix()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, created_at, updated_at)
		VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("importer: prepare templates: %w", err)
	}
	defer stmt.Close()
	for _, t := range templates {
		if _, err := stmt.ExecContext(ctx, opts.TargetWID, t.Name, t.Main, t.EntryTpl, t.CSS, t.Info, now, now); err != nil {
			return fmt.Errorf("importer: insert template %d: %w", t.ID, err)
		}
		report.Templates++
		// Surface lint warnings on the main body — SB2 templates often
		// reference trackback / amazon / mobile blocks that the Go
		// port does not support. Reuses the SB3 importer's helper so
		// both versions emit the same warning shape.
		lintTemplateBody(t.Name, t.Main, report)
	}
	return nil
}

// importSB2Categories inserts categories preserving parent/child links
// and returns a SB2-id → dest-id map for entry remapping. The two-pass
// shape is the same as the SB3 importer.
func importSB2Categories(ctx context.Context, tx *sql.Tx, cats []sb2Category, opts Options, report *Report) (map[int64]int64, error) {
	idMap := make(map[int64]int64, len(cats))
	now := time.Now().Unix()
	insertStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, legacy_id, legacy_dir, created_at, updated_at)
		VALUES (?, ?, ?, '', ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("importer: prepare categories: %w", err)
	}
	defer insertStmt.Close()
	updateParentStmt, err := tx.PrepareContext(ctx, `UPDATE categories SET parent_id = ? WHERE id = ?`)
	if err != nil {
		return nil, fmt.Errorf("importer: prepare category parent fixup: %w", err)
	}
	defer updateParentStmt.Close()

	// First pass: insert with parent_id = 0; track legacy parent ids.
	type fixup struct{ destID, legacyParent int64 }
	var fixups []fixup
	for _, c := range cats {
		res, err := insertStmt.ExecContext(ctx, opts.TargetWID, 0, c.Name, c.Order, c.ID, c.Dir, now, now)
		if err != nil {
			return nil, fmt.Errorf("importer: insert category %d: %w", c.ID, err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		idMap[c.ID] = newID
		if c.Main != 0 {
			fixups = append(fixups, fixup{destID: newID, legacyParent: c.Main})
		}
		report.Categories++
	}
	for _, f := range fixups {
		newParent, ok := idMap[f.legacyParent]
		if !ok {
			report.Warnings = append(report.Warnings, fmt.Sprintf("category dest=%d: parent legacy id %d not found", f.destID, f.legacyParent))
			continue
		}
		if _, err := updateParentStmt.ExecContext(ctx, newParent, f.destID); err != nil {
			return nil, fmt.Errorf("importer: fixup category parent %d: %w", f.destID, err)
		}
	}
	return idMap, nil
}

// importSB2Entries inserts every SB2 entry, attributes authorship to
// opts.AuthorID, and returns a SB2-id → dest-id map so subsequent
// passes (notably comments) can remap their entry references.
func importSB2Entries(ctx context.Context, tx *sql.Tx, entries []sb2Entry, catMap map[int64]int64, opts Options, report *Report) (map[int64]int64, error) {
	idMap := make(map[int64]int64, len(entries))
	now := time.Now().Unix()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, legacy_id, legacy_file, keywords, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("importer: prepare entries: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		// SB2 stat 1 / 2 → Published; 0 → Draft; anything else → Closed.
		var status int64
		switch e.Stat {
		case 0:
			status = 0 // EntryDraft
		case 1, 2:
			status = 1 // EntryPublished
		default:
			status = -1 // EntryClosed
		}
		// Caller already filtered with OnlyPublished; the switch above
		// is defensive only.

		var catID sql.NullInt64
		if mapped, ok := catMap[e.Cat]; ok {
			catID = sql.NullInt64{Int64: mapped, Valid: true}
		}

		// SB3 importer normalises format to "" / "html" / "md" / "sbtext".
		// SB2 only ever stored an empty string (legacy HTML) or the
		// plugin name; keep the value verbatim and let the renderer
		// fall through to its default if unknown.
		format := strings.TrimSpace(e.Form)

		res, err := stmt.ExecContext(ctx,
			opts.TargetWID,
			opts.AuthorID,
			catID,
			e.Subj,
			e.Body,
			e.More,
			format,
			status,
			e.Date,
			e.ID,
			e.File,
			e.Key,
			now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("importer: insert entry %d: %w", e.ID, err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		idMap[e.ID] = newID
		report.Entries++
	}
	return idMap, nil
}

// importSB2Messages inserts comments. SB2 message stat aligns with the
// destination messages.status: 0=waiting, 1=approved, -1=closed; pass
// it through directly. Comments whose entry was filtered out by
// OnlyPublished are skipped silently — there's no entry to attach to.
func importSB2Messages(ctx context.Context, tx *sql.Tx, msgs []sb2Message, entryMap map[int64]int64, opts Options, report *Report) error {
	if len(msgs) == 0 {
		return nil
	}
	now := time.Now().Unix()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO messages (wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("importer: prepare messages: %w", err)
	}
	defer stmt.Close()
	for _, m := range msgs {
		entryID, ok := entryMap[m.EID]
		if !ok {
			// Orphaned by OnlyPublished filtering or by a deleted
			// source entry. Drop silently — counting these is more
			// noise than signal in the typical migration.
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			opts.TargetWID,
			entryID,
			m.Stat,
			m.Date,
			m.Auth,
			m.Mail,
			m.URL,
			m.Body,
			m.Host,
			m.Agent,
			now, now,
		); err != nil {
			return fmt.Errorf("importer: insert message %d: %w", m.ID, err)
		}
	}
	return nil
}
