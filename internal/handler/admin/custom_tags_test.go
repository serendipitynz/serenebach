package admin

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage"
	"github.com/serendipitynz/serenebach/internal/storage/repo"

	_ "modernc.org/sqlite"
)

func newAdminTestHandler(t *testing.T) (*Handler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := repo.New(db)
	return &Handler{Store: store, WID: 1}, db
}

func TestValidateCustomTagRejectsInvalidName(t *testing.T) {
	// Boundary cases: exactly 49, 50, and 51 characters after "custom_"
	name49 := "custom_" + strings.Repeat("a", 49)
	name50 := "custom_" + strings.Repeat("a", 50)
	name51 := "custom_" + strings.Repeat("a", 51)

	cases := []struct {
		name string
		want bool
	}{
		{"custom_hello", true},
		{"custom_hello_world", true},
		{"custom_a1", true},
		{name49, true},  // 49 chars after custom_ → valid
		{name50, true},  // 50 chars after custom_ → valid (boundary)
		{name51, false}, // 51 chars after custom_ → invalid
		{"custom_", false},
		{"custom_1start", false},
		{"custom-hello", false},
		{"custom_hello_world_very_long_name_exceeds_fifty_chars_limit", false},
		{"hello", false},
		{"", false},
	}

	for _, c := range cases {
		valid, _ := validateCustomTag(c.name, "v")
		if valid != c.want {
			t.Errorf("validateCustomTag(name=%q) = %v, want %v", c.name, valid, c.want)
		}
	}
}

func TestValidateCustomTagRejectsLongValue(t *testing.T) {
	short := strings.Repeat("a", maxCustomTagValueBytes)
	long := strings.Repeat("a", maxCustomTagValueBytes+1)

	valid, _ := validateCustomTag("custom_ok", short)
	if !valid {
		t.Error("expected valid for max-length value")
	}

	valid, _ = validateCustomTag("custom_ok", long)
	if valid {
		t.Error("expected invalid for over-long value")
	}
}

func TestCustomTagCreateRejectsDuplicate(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	ctx := context.Background()

	// Seed one tag.
	if _, err := h.Store.CreateCustomTag(ctx, domain.CustomTag{WID: 1, Name: "custom_dup", Value: "a"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Attempt to create the same name.
	form := url.Values{}
	form.Set("name", "custom_dup")
	form.Set("value", "b")
	req := httptest.NewRequest("POST", "/admin/templates/custom-tags", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.customTagCreate(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Errorf("expected error redirect for duplicate, got %q", loc)
	}
}

func TestCustomTagCreateRejectsAtLimit(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	ctx := context.Background()

	// Seed up to the limit.
	for i := 0; i < maxCustomTags; i++ {
		name := "custom_tag_" + strconv.Itoa(i)
		if _, err := h.Store.CreateCustomTag(ctx, domain.CustomTag{WID: 1, Name: name, Value: "v"}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	form := url.Values{}
	form.Set("name", "custom_one_too_many")
	form.Set("value", "v")
	req := httptest.NewRequest("POST", "/admin/templates/custom-tags", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.customTagCreate(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Errorf("expected error redirect at limit, got %q", loc)
	}
}
