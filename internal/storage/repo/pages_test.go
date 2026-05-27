package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestPageCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id := pageCRUDCreate(t, ctx, s)
	p := pageCRUDReadByID(t, ctx, s, id)
	pageCRUDReadBySlug(t, ctx, s, id)
	pageCRUDListAdminIncludesDrafts(t, ctx, s)
	pageCRUDListPublishedFiltersDrafts(t, ctx, s)
	pageCRUDUpdate(t, ctx, s, id, p)
	pageCRUDDelete(t, ctx, s, id)
}

func pageCRUDCreate(t *testing.T, ctx context.Context, s *Store) int64 {
	t.Helper()
	id, err := s.CreatePage(ctx, domain.Page{
		WID:           1,
		AuthorID:      1,
		Title:         "About",
		Body:          "<p>hello</p>",
		Format:        "html",
		Slug:          "/about",
		TemplateID:    0,
		SortOrder:     1,
		Status:        domain.PagePublished,
		OGBGImagePath: "",
		Summary:       "about page summary",
		CanonicalURL:  "https://example.com/about",
		NoIndex:       false,
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
	return id
}

func pageCRUDReadByID(t *testing.T, ctx context.Context, s *Store, id int64) *domain.Page {
	t.Helper()
	p, err := s.PageByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("PageByID: %v", err)
	}
	if p.Title != "About" {
		t.Errorf("title = %q, want About", p.Title)
	}
	if p.Slug != "/about" {
		t.Errorf("slug = %q, want /about", p.Slug)
	}
	if p.Summary != "about page summary" {
		t.Errorf("summary = %q", p.Summary)
	}
	if p.CanonicalURL != "https://example.com/about" {
		t.Errorf("canonical_url = %q", p.CanonicalURL)
	}
	if p.NoIndex {
		t.Error("noindex = true, want false")
	}
	return p
}

func pageCRUDReadBySlug(t *testing.T, ctx context.Context, s *Store, wantID int64) {
	t.Helper()
	p2, err := s.PageBySlug(ctx, 1, "/about")
	if err != nil {
		t.Fatalf("PageBySlug: %v", err)
	}
	if p2.ID != wantID {
		t.Errorf("PageBySlug id = %d, want %d", p2.ID, wantID)
	}
}

func pageCRUDListAdminIncludesDrafts(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	if _, err := s.CreatePage(ctx, domain.Page{
		WID: 1, AuthorID: 1, Title: "Draft", Body: "d",
		Format: "html", Slug: "/draft", Status: domain.PageDraft,
	}); err != nil {
		t.Fatalf("CreatePage draft: %v", err)
	}
	all, err := s.ListPagesForAdmin(ctx, 1, ListPagesQuery{})
	if err != nil {
		t.Fatalf("ListPagesForAdmin: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("admin list len = %d, want 2", len(all))
	}
}

func pageCRUDListPublishedFiltersDrafts(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	pub, err := s.PublishedPages(ctx, 1)
	if err != nil {
		t.Fatalf("PublishedPages: %v", err)
	}
	if len(pub) != 1 {
		t.Errorf("published len = %d, want 1", len(pub))
	}
	if pub[0].Title != "About" {
		t.Errorf("published title = %q, want About", pub[0].Title)
	}
}

func pageCRUDUpdate(t *testing.T, ctx context.Context, s *Store, id int64, p *domain.Page) {
	t.Helper()
	p.Title = "About Us"
	p.Body = "<p>updated</p>"
	p.Slug = "/about-us"
	p.TemplateID = 2
	p.Status = domain.PageDraft
	p.OGBGImagePath = "bg.png"
	p.Summary = "updated summary"
	p.CanonicalURL = ""
	p.NoIndex = true
	if err := s.UpdatePage(ctx, *p); err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}
	updated, err := s.PageByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("PageByID after update: %v", err)
	}
	assertPageUpdated(t, updated)
}

func pageCRUDDelete(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	if err := s.DeletePage(ctx, 1, id); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	if _, err := s.PageByID(ctx, 1, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

// assertPageUpdated bundles the post-update field checks so the parent
// helper stays under the gocyclo threshold.
func assertPageUpdated(t *testing.T, updated *domain.Page) {
	t.Helper()
	if updated.Title != "About Us" {
		t.Errorf("title after update = %q, want About Us", updated.Title)
	}
	if updated.Body != "<p>updated</p>" {
		t.Errorf("body after update = %q", updated.Body)
	}
	if updated.Slug != "/about-us" {
		t.Errorf("slug after update = %q, want /about-us", updated.Slug)
	}
	if updated.TemplateID != 2 {
		t.Errorf("template_id after update = %d, want 2", updated.TemplateID)
	}
	if updated.Status != domain.PageDraft {
		t.Errorf("status after update = %d, want draft", updated.Status)
	}
	if updated.OGBGImagePath != "bg.png" {
		t.Errorf("og_bg after update = %q, want bg.png", updated.OGBGImagePath)
	}
	if updated.Summary != "updated summary" {
		t.Errorf("summary after update = %q", updated.Summary)
	}
	if updated.CanonicalURL != "" {
		t.Errorf("canonical_url after update = %q, want empty", updated.CanonicalURL)
	}
	if !updated.NoIndex {
		t.Error("noindex after update = false, want true")
	}
}

func TestPageSlugUnique(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.CreatePage(ctx, domain.Page{
		WID: 1, AuthorID: 1, Title: "A", Body: "b",
		Format: "html", Slug: "/about", Status: domain.PagePublished,
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = s.CreatePage(ctx, domain.Page{
		WID: 1, AuthorID: 1, Title: "B", Body: "b",
		Format: "html", Slug: "/about", Status: domain.PagePublished,
	})
	if !errors.Is(err, ErrSlugInUse) {
		t.Errorf("expected ErrSlugInUse, got %v", err)
	}
}

func TestPageUpdateNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.UpdatePage(ctx, domain.Page{
		ID: 9999, WID: 1, Title: "X", Body: "b",
		Format: "html", Slug: "/x", Status: domain.PagePublished,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPageDeleteNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.DeletePage(ctx, 1, 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
