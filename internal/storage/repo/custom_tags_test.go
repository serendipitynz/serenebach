package repo

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db)
}

func TestCustomTagCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id := customTagCRUDCreate(t, ctx, s)
	customTagCRUDList(t, ctx, s)
	customTagCRUDReadByID(t, ctx, s, id)
	customTagCRUDUpdate(t, ctx, s, id)
	customTagCRUDCount(t, ctx, s)
	customTagCRUDDelete(t, ctx, s, id)
}

func customTagCRUDCreate(t *testing.T, ctx context.Context, s *Store) int64 {
	t.Helper()
	id, err := s.CreateCustomTag(ctx, domain.CustomTag{
		WID:   1,
		Name:  "custom_ga",
		Value: "<script>ga</script>",
	})
	if err != nil {
		t.Fatalf("CreateCustomTag: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
	return id
}

func customTagCRUDList(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	tags, err := s.ListCustomTags(ctx, 1)
	if err != nil {
		t.Fatalf("ListCustomTags: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].Name != "custom_ga" || tags[0].Value != "<script>ga</script>" {
		t.Errorf("unexpected tag: %+v", tags[0])
	}
}

func customTagCRUDReadByID(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	got, err := s.CustomTagByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("CustomTagByID: %v", err)
	}
	if got.Name != "custom_ga" {
		t.Errorf("ByID name = %q, want custom_ga", got.Name)
	}
}

func customTagCRUDUpdate(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	if err := s.UpdateCustomTag(ctx, domain.CustomTag{
		ID:    id,
		WID:   1,
		Name:  "custom_ga4",
		Value: "<script>ga4</script>",
	}); err != nil {
		t.Fatalf("UpdateCustomTag: %v", err)
	}
	got, _ := s.CustomTagByID(ctx, 1, id)
	if got.Name != "custom_ga4" || got.Value != "<script>ga4</script>" {
		t.Errorf("after update: %+v", got)
	}
}

func customTagCRUDCount(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	c, err := s.CountCustomTags(ctx, 1)
	if err != nil {
		t.Fatalf("CountCustomTags: %v", err)
	}
	if c != 1 {
		t.Errorf("count = %d, want 1", c)
	}
}

func customTagCRUDDelete(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	if err := s.DeleteCustomTag(ctx, 1, id); err != nil {
		t.Fatalf("DeleteCustomTag: %v", err)
	}
	if _, err := s.CustomTagByID(ctx, 1, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestCustomTagDuplicateName(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.CreateCustomTag(ctx, domain.CustomTag{WID: 1, Name: "custom_x", Value: "a"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err = s.CreateCustomTag(ctx, domain.CustomTag{WID: 1, Name: "custom_x", Value: "b"})
	if !errors.Is(err, ErrSlugInUse) {
		t.Errorf("expected ErrSlugInUse for duplicate, got %v", err)
	}
}

func TestCustomTagDBDefaultTimestamp(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Insert directly using DB defaults (no explicit timestamps).
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO site_custom_tags (wid, name, value) VALUES (?, ?, ?)`,
		1, "custom_default", "v")
	if err != nil {
		t.Fatalf("insert with defaults: %v", err)
	}
	id, _ := res.LastInsertId()

	// Repo methods must scan the DB-default timestamp without error.
	tags, err := s.ListCustomTags(ctx, 1)
	if err != nil {
		t.Fatalf("ListCustomTags with default timestamp: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt from DB default")
	}

	got, err := s.CustomTagByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("CustomTagByID with default timestamp: %v", err)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("expected non-zero timestamps from DB default")
	}
}
