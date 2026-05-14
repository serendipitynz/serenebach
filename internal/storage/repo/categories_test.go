package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestCategoryBySlug(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, err := s.CreateCategory(ctx, domain.Category{
		WID: 1, Name: "Travel", Slug: "travel",
	}, 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	got, err := s.CategoryBySlug(ctx, 1, "travel")
	if err != nil {
		t.Fatalf("CategoryBySlug: %v", err)
	}
	if got.ID != id {
		t.Errorf("id = %d, want %d", got.ID, id)
	}
	if got.Slug != "travel" {
		t.Errorf("slug = %q, want travel", got.Slug)
	}

	// Empty input must be rejected so the slug-less default ('') does
	// not match every uncustomised category.
	if _, err := s.CategoryBySlug(ctx, 1, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty slug: got %v, want ErrNotFound", err)
	}

	if _, err := s.CategoryBySlug(ctx, 1, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing slug: got %v, want ErrNotFound", err)
	}
}

// TestCategorySlugUniqueIndex confirms the partial unique index on
// categories(wid, slug) prevents a second non-empty slug duplicate
// from being inserted. The admin form rejects duplicates earlier, but
// the DB-level guard backstops anything that bypasses the handler
// (importer, manual SQL, future write paths).
func TestCategorySlugUniqueIndex(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.CreateCategory(ctx, domain.Category{
		WID: 1, Name: "First", Slug: "shared",
	}, 0); err != nil {
		t.Fatalf("CreateCategory first: %v", err)
	}
	if _, err := s.CreateCategory(ctx, domain.Category{
		WID: 1, Name: "Second", Slug: "shared",
	}, 1); err == nil {
		t.Fatal("expected duplicate slug insert to fail at the unique index")
	}

	// Empty slugs may coexist freely (the partial index excludes the
	// blank value so the slug-less default does not block anybody).
	if _, err := s.CreateCategory(ctx, domain.Category{
		WID: 1, Name: "Plain A", Slug: "",
	}, 2); err != nil {
		t.Fatalf("empty slug A: %v", err)
	}
	if _, err := s.CreateCategory(ctx, domain.Category{
		WID: 1, Name: "Plain B", Slug: "",
	}, 3); err != nil {
		t.Fatalf("empty slug B: %v", err)
	}
}

func TestCategorySlugInUse(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, err := s.CreateCategory(ctx, domain.Category{
		WID: 1, Name: "Travel", Slug: "travel",
	}, 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	cases := []struct {
		name     string
		slug     string
		except   int64
		expected bool
	}{
		{"hit, no exception", "travel", 0, true},
		{"hit, but excluded", "travel", id, false},
		{"empty slug never collides", "", 0, false},
		{"unused slug", "foo", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.CategorySlugInUse(ctx, 1, tc.slug, tc.except)
			if err != nil {
				t.Fatalf("CategorySlugInUse: %v", err)
			}
			if got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestCategoryByLegacyDir(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO categories (wid, name, slug, sort_order, legacy_dir, created_at, updated_at)
		VALUES (1, 'Travel', 'travel', 0, 'travel/', strftime('%s','now'), strftime('%s','now'))`); err != nil {
		t.Fatalf("seed category: %v", err)
	}

	ref, err := s.CategoryByLegacyDir(ctx, 1, "travel/")
	if err != nil {
		t.Fatalf("CategoryByLegacyDir: %v", err)
	}
	if ref.Slug != "travel" {
		t.Errorf("slug = %q, want travel", ref.Slug)
	}
	if ref.ID == 0 {
		t.Error("expected non-zero id")
	}

	if _, err := s.CategoryByLegacyDir(ctx, 1, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty dir: got %v, want ErrNotFound", err)
	}
}
