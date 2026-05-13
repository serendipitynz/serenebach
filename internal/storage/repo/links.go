package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// linkColumns is the canonical column list for the links table. Order
// must match the inline Scan call sites in AllLinks / VisibleLinks /
// LinkByID.
const linkColumns = `id, wid, name, url, description, target, kind, parent_id, sort_order, disp, created_at, updated_at`

// AllLinks returns every link row for a weblog, ordered by sort_order.
// Admin list + public sidebar both iterate in this order; group vs link
// disambiguation is done by the caller via Link.IsGroup.
func (s *Store) AllLinks(ctx context.Context, wid int64) ([]domain.Link, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+linkColumns+`
		FROM links WHERE wid = ?
		ORDER BY sort_order ASC, id ASC`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: AllLinks: %w", err)
	}
	defer rows.Close()
	var out []domain.Link
	for rows.Next() {
		var l domain.Link
		if err := rows.Scan(&l.ID, &l.WID, &l.Name, &l.URL, &l.Description, &l.Target, &l.Kind, &l.ParentID, &l.SortOrder, &l.Disp, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, fmt.Errorf("repo: AllLinks scan: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// VisibleLinks returns only the rows the public sidebar should render —
// disp == 0. Groups are included whether or not they currently have
// visible children; the renderer trims empty groups itself.
func (s *Store) VisibleLinks(ctx context.Context, wid int64) ([]domain.Link, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+linkColumns+`
		FROM links WHERE wid = ? AND disp = 0
		ORDER BY sort_order ASC, id ASC`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: VisibleLinks: %w", err)
	}
	defer rows.Close()
	var out []domain.Link
	for rows.Next() {
		var l domain.Link
		if err := rows.Scan(&l.ID, &l.WID, &l.Name, &l.URL, &l.Description, &l.Target, &l.Kind, &l.ParentID, &l.SortOrder, &l.Disp, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, fmt.Errorf("repo: VisibleLinks scan: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LinkByID fetches one link row. Returns ErrNotFound when the row
// doesn't exist or belongs to a different weblog.
func (s *Store) LinkByID(ctx context.Context, wid, id int64) (*domain.Link, error) {
	var l domain.Link
	err := s.db.QueryRowContext(ctx, `
		SELECT `+linkColumns+`
		FROM links WHERE wid = ? AND id = ?`, wid, id).
		Scan(&l.ID, &l.WID, &l.Name, &l.URL, &l.Description, &l.Target, &l.Kind, &l.ParentID, &l.SortOrder, &l.Disp, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repo: LinkByID: %w", err)
	}
	return &l, nil
}

// CreateLink inserts a new link row. The caller is responsible for
// setting Kind / ParentID / URL correctly — this function doesn't
// enforce invariants so tests can exercise bad-path behaviour (the admin
// handler does the validation). sort_order defaults to max+1 so new
// items land at the end of the list.
func (s *Store) CreateLink(ctx context.Context, l domain.Link) (int64, error) {
	now := time.Now().Unix()
	if l.CreatedAt == 0 {
		l.CreatedAt = now
	}
	if l.UpdatedAt == 0 {
		l.UpdatedAt = now
	}
	if l.SortOrder == 0 {
		var maxOrder sql.NullInt64
		if err := s.db.QueryRowContext(ctx,
			`SELECT MAX(sort_order) FROM links WHERE wid = ?`, l.WID).Scan(&maxOrder); err != nil {
			return 0, fmt.Errorf("repo: CreateLink max sort: %w", err)
		}
		l.SortOrder = int(maxOrder.Int64) + 1
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO links (wid, name, url, description, target, kind, parent_id, sort_order, disp, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.WID, l.Name, l.URL, l.Description, l.Target, l.Kind, l.ParentID, l.SortOrder, l.Disp, l.CreatedAt, l.UpdatedAt)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateLink: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateLink lastid: %w", err)
	}
	return id, nil
}

// UpdateLink overwrites the mutable fields. Kind is not updatable by
// design — the admin form hides the selector on edit so a group never
// becomes a link (its children would orphan) and a link never becomes a
// group (it might already be referenced by parent_id). created_at stays
// put; updated_at advances.
func (s *Store) UpdateLink(ctx context.Context, l domain.Link) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE links
		SET name = ?, url = ?, description = ?, target = ?,
		    parent_id = ?, sort_order = ?, disp = ?, updated_at = ?
		WHERE wid = ? AND id = ?`,
		l.Name, l.URL, l.Description, l.Target,
		l.ParentID, l.SortOrder, l.Disp, time.Now().Unix(), l.WID, l.ID)
	if err != nil {
		return fmt.Errorf("repo: UpdateLink: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLink removes a link. When the target is a group its member
// links are detached (parent_id → 0) in the same transaction so they
// survive as ungrouped root-level rows; the admin can then re-home or
// delete them individually.
func (s *Store) DeleteLink(ctx context.Context, wid, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: DeleteLink begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx,
		`UPDATE links SET parent_id = 0, updated_at = ?
		 WHERE wid = ? AND parent_id = ?`,
		time.Now().Unix(), wid, id); err != nil {
		return fmt.Errorf("repo: DeleteLink detach members: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`DELETE FROM links WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteLink: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: DeleteLink commit: %w", err)
	}
	tx = nil
	return nil
}

// ReorderLinks rewrites sort_order for the given ids so the list order
// matches the slice. Mirrors ReorderCategories — one transaction, ids
// not present in the slice are left alone.
func (s *Store) ReorderLinks(ctx context.Context, wid int64, orderedIDs []int64) error {
	if len(orderedIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: ReorderLinks begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.PrepareContext(ctx,
		`UPDATE links SET sort_order = ?, updated_at = ? WHERE wid = ? AND id = ?`)
	if err != nil {
		return fmt.Errorf("repo: ReorderLinks prepare: %w", err)
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for i, id := range orderedIDs {
		if _, err := stmt.ExecContext(ctx, i, now, wid, id); err != nil {
			return fmt.Errorf("repo: ReorderLinks update id=%d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: ReorderLinks commit: %w", err)
	}
	tx = nil
	return nil
}

// ReorderLinksInGroup rewrites sort_order for the given ids scoped to a
// specific group. Only rows matching wid + parent_id are updated; ids
// that don't belong to the group are silently skipped.
func (s *Store) ReorderLinksInGroup(ctx context.Context, wid, groupID int64, orderedIDs []int64) error {
	if len(orderedIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: ReorderLinksInGroup begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.PrepareContext(ctx,
		`UPDATE links SET sort_order = ?, updated_at = ? WHERE wid = ? AND parent_id = ? AND id = ?`)
	if err != nil {
		return fmt.Errorf("repo: ReorderLinksInGroup prepare: %w", err)
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for i, id := range orderedIDs {
		if _, err := stmt.ExecContext(ctx, i, now, wid, groupID, id); err != nil {
			return fmt.Errorf("repo: ReorderLinksInGroup update id=%d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: ReorderLinksInGroup commit: %w", err)
	}
	tx = nil
	return nil
}

// CountLinksInGroup returns how many link rows currently belong to the
// given group. The admin list page uses this to print "リンク数: N" next
// to group rows.
func (s *Store) CountLinksInGroup(ctx context.Context, wid, groupID int64) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM links WHERE wid = ? AND parent_id = ?`,
		wid, groupID).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountLinksInGroup: %w", err)
	}
	return n, nil
}
