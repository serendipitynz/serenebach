package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// ErrUserNameInUse is returned when a create / rename call would duplicate
// an existing user's login name. Callers (the /admin/users form) catch
// this and re-render with a validation message.
var ErrUserNameInUse = errors.New("repo: username already in use")

// userNamePattern matches the SB3 login-name grammar: ASCII
// alphanumerics plus underscore, hyphen, and period. Display names
// are unrestricted so 日本語 etc. survives untouched.
var userNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// IsValidUserName reports whether s is an acceptable login name. Empty
// is invalid — callers treat blank as "missing" in their own layer.
func IsValidUserName(s string) bool {
	if len(s) == 0 || len(s) > 50 {
		return false
	}
	return userNamePattern.MatchString(s)
}

// ListUsers returns every user row for the weblog, ordered by
// sort_order then id — same pattern categories use so the admin
// drag-reorder contract stays uniform.
func (s *Store) ListUsers(ctx context.Context, wid int64) ([]domain.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, name, display_name, email, role, description, description_format, list_visible, sort_order
		FROM users WHERE wid = ?
		ORDER BY sort_order, id`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: ListUsers: %w", err)
	}
	defer rows.Close()
	out := []domain.User{}
	for rows.Next() {
		var u domain.User
		var listVis int
		if err := rows.Scan(&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role, &u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder); err != nil {
			return nil, fmt.Errorf("repo: scan user: %w", err)
		}
		u.ListVisible = listVis != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// HasAdminUser reports whether at least one admin (role=RoleAdmin) row
// exists in any weblog. Used by the first-run setup gate to decide
// whether the install still needs an initial administrator. Wid is
// intentionally not part of the lookup — multi-blog is not implemented
// yet, but even when it is the question "has *anyone* set this install
// up" stays singular.
func (s *Store) HasAdminUser(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = ?`, domain.RoleAdmin,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("repo: HasAdminUser: %w", err)
	}
	return n > 0, nil
}

// CreateUser inserts a new user row with a pre-hashed password. The
// caller hashes with auth.HashPassword so the repo never sees the
// plaintext. Returns ErrUserNameInUse on a name collision.
func (s *Store) CreateUser(ctx context.Context, u domain.User, passwordHash string) (int64, error) {
	now := time.Now().Unix()
	listVis := 0
	if u.ListVisible {
		listVis = 1
	}
	descFmt := u.DescriptionFormat
	if descFmt == "" {
		descFmt = "html"
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (wid, name, display_name, email, password_hash, role,
		                   description, description_format, list_visible, sort_order,
		                   created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.WID, u.Name, u.DisplayName, u.Email, passwordHash, u.Role,
		u.Description, descFmt, listVis, u.SortOrder,
		now, now)
	if err != nil {
		if isUniqueUserNameViolation(err) {
			return 0, ErrUserNameInUse
		}
		return 0, fmt.Errorf("repo: CreateUser: %w", err)
	}
	return res.LastInsertId()
}

// UpdateUser rewrites the profile fields (everything except password,
// which UpdateUserPassword handles so password changes never mix with
// a profile save that left the field blank).
func (s *Store) UpdateUser(ctx context.Context, u domain.User) error {
	listVis := 0
	if u.ListVisible {
		listVis = 1
	}
	descFmt := u.DescriptionFormat
	if descFmt == "" {
		descFmt = "html"
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET
			name = ?, display_name = ?, email = ?, role = ?,
			description = ?, description_format = ?, list_visible = ?, sort_order = ?,
			updated_at = ?
		WHERE id = ?`,
		u.Name, u.DisplayName, u.Email, u.Role,
		u.Description, descFmt, listVis, u.SortOrder,
		time.Now().Unix(), u.ID)
	if err != nil {
		if isUniqueUserNameViolation(err) {
			return ErrUserNameInUse
		}
		return fmt.Errorf("repo: UpdateUser: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserAIConfig writes the per-user AI provider fields.
// Kept separate from UpdateUser so the profile save path doesn't
// have to drag the ciphertext column through every render — the
// admin form POSTs provider config to its own action. Passing an
// empty AIKind clears the config back to "AI disabled".
func (s *Store) UpdateUserAIConfig(ctx context.Context, userID int64, u domain.User) error {
	autoAlt := 0
	if u.AIAutoAlt {
		autoAlt = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET
			ai_kind = ?, ai_base_url = ?, ai_model = ?, ai_api_key_enc = ?, ai_auto_alt = ?, ai_timeout_seconds = ?,
			updated_at = ?
		WHERE id = ?`,
		u.AIKind, u.AIBaseURL, u.AIModel, u.AIAPIKeyEnc, autoAlt, u.AITimeoutSeconds,
		time.Now().Unix(), userID)
	if err != nil {
		return fmt.Errorf("repo: UpdateUserAIConfig: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserPassword writes a new bcrypt hash for one user. Keeping
// this separate from UpdateUser matches the admin form's "leave
// blank to keep" UX — the form handler only calls this when a new
// password was actually entered.
func (s *Store) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, time.Now().Unix(), userID)
	if err != nil {
		return fmt.Errorf("repo: UpdateUserPassword: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes a user. Associated session rows are swept in the
// same transaction so the deleted user's browsers get forced-logged-out
// on their next request. Entry rows are left untouched — they still
// carry author_id, which UsersByIDs silently maps to a missing author
// (rendered as blank) rather than breaking the page.
func (s *Store) DeleteUser(ctx context.Context, wid, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: DeleteUser begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		return fmt.Errorf("repo: DeleteUser sessions: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND wid = ?`, id, wid)
	if err != nil {
		return fmt.Errorf("repo: DeleteUser row: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: DeleteUser commit: %w", err)
	}
	tx = nil
	return nil
}

// ReorderUsers applies a new sort_order based on the position of each
// id in the input slice. Mirrors ReorderCategories / ReorderTemplates;
// ids not present are left alone so a partial admin payload can't
// blank out the list.
func (s *Store) ReorderUsers(ctx context.Context, wid int64, orderedIDs []int64) error {
	if len(orderedIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: ReorderUsers begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.PrepareContext(ctx,
		`UPDATE users SET sort_order = ?, updated_at = ? WHERE wid = ? AND id = ?`)
	if err != nil {
		return fmt.Errorf("repo: ReorderUsers prepare: %w", err)
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for i, id := range orderedIDs {
		if _, err := stmt.ExecContext(ctx, i, now, wid, id); err != nil {
			return fmt.Errorf("repo: ReorderUsers exec: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: ReorderUsers commit: %w", err)
	}
	tx = nil
	return nil
}

// VisibleProfileUsers returns the users the public {profile_area}
// block should iterate over — list_visible=1 only, ordered by
// sort_order then id so the admin reorder UI drives the public
// display order.
func (s *Store) VisibleProfileUsers(ctx context.Context, wid int64) ([]domain.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, name, display_name, email, role, description, description_format, list_visible, sort_order
		FROM users WHERE wid = ? AND list_visible = 1
		ORDER BY sort_order, id`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: VisibleProfileUsers: %w", err)
	}
	defer rows.Close()
	out := []domain.User{}
	for rows.Next() {
		var u domain.User
		var listVis int
		if err := rows.Scan(&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role, &u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder); err != nil {
			return nil, fmt.Errorf("repo: scan user: %w", err)
		}
		u.ListVisible = listVis != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountAdmins returns how many RoleAdmin users exist on the weblog.
// The delete + update paths consult this to block demoting / removing
// the last admin, which would lock the site out of its own user UI.
func (s *Store) CountAdmins(ctx context.Context, wid int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE wid = ? AND role = ?`, wid, domain.RoleAdmin).Scan(&n)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("repo: CountAdmins: %w", err)
	}
	return n, nil
}

// UserByName looks up one user by login name. Used on login.
func (s *Store) UserByName(ctx context.Context, name string) (*domain.User, string, error) {
	var u domain.User
	var hash string
	var listVis int
	var autoAlt int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, wid, name, display_name, email, role, description, description_format, list_visible, sort_order,
		       ai_kind, ai_base_url, ai_model, ai_api_key_enc, ai_auto_alt, ai_timeout_seconds,
		       password_hash
		FROM users WHERE name = ?`, name).Scan(
		&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role, &u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder,
		&u.AIKind, &u.AIBaseURL, &u.AIModel, &u.AIAPIKeyEnc, &autoAlt, &u.AITimeoutSeconds,
		&hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("repo: UserByName: %w", err)
	}
	u.ListVisible = listVis != 0
	u.AIAutoAlt = autoAlt != 0
	return &u, hash, nil
}

// UserByID looks up one user by primary key. Used by session middleware.
func (s *Store) UserByID(ctx context.Context, id int64) (*domain.User, error) {
	var u domain.User
	var listVis, autoAlt int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, wid, name, display_name, email, role, description, description_format, list_visible, sort_order,
		       ai_kind, ai_base_url, ai_model, ai_api_key_enc, ai_auto_alt, ai_timeout_seconds
		FROM users WHERE id = ?`, id).Scan(
		&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role, &u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder,
		&u.AIKind, &u.AIBaseURL, &u.AIModel, &u.AIAPIKeyEnc, &autoAlt, &u.AITimeoutSeconds)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: UserByID: %w", err)
	}
	u.ListVisible = listVis != 0
	u.AIAutoAlt = autoAlt != 0
	return &u, nil
}

// UsersByIDs returns the users matching the given ids as a map keyed by id.
func (s *Store) UsersByIDs(ctx context.Context, ids []int64) (map[int64]domain.User, error) {
	if len(ids) == 0 {
		return map[int64]domain.User{}, nil
	}
	args := make([]any, 0, len(ids))
	placeholders := make([]byte, 0, 2*len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	q := "SELECT id, wid, name, display_name, email, role, description, description_format, list_visible, sort_order FROM users WHERE id IN (" + string(placeholders) + ")"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("repo: UsersByIDs: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]domain.User, len(ids))
	for rows.Next() {
		var u domain.User
		var listVis int
		if err := rows.Scan(&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role, &u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder); err != nil {
			return nil, fmt.Errorf("repo: scan user: %w", err)
		}
		u.ListVisible = listVis != 0
		out[u.ID] = u
	}
	return out, rows.Err()
}

// isUniqueUserNameViolation narrows isUniqueViolation to collisions on
// the users.name column. The users table doesn't carry a formal unique
// index (yet) — name uniqueness is enforced in the admin handler via
// a lookup + attempt, so this helper currently just piggybacks on the
// shared sniffer.
func isUniqueUserNameViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Match the same substrings isUniqueViolation() looks for, narrowed
	// to the users.name column name so unrelated unique indexes (should
	// any appear) don't get misattributed to the name field.
	if !strings.Contains(msg, "users.name") {
		return false
	}
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed (unique)")
}
