package repo

import (
	"context"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestPageCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create
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
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	// PageByID
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

	// PageBySlug
	p2, err := s.PageBySlug(ctx, 1, "/about")
	if err != nil {
		t.Fatalf("PageBySlug: %v", err)
	}
	if p2.ID != id {
		t.Errorf("PageBySlug id = %d, want %d", p2.ID, id)
	}

	// ListPagesForAdmin returns both statuses
	_, err = s.CreatePage(ctx, domain.Page{
		WID: 1, AuthorID: 1, Title: "Draft", Body: "d",
		Format: "html", Slug: "/draft", Status: domain.PageDraft,
	})
	if err != nil {
		t.Fatalf("CreatePage draft: %v", err)
	}
	all, err := s.ListPagesForAdmin(ctx, 1)
	if err != nil {
		t.Fatalf("ListPagesForAdmin: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("admin list len = %d, want 2", len(all))
	}

	// PublishedPages returns published only
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

	// UpdatePage
	p.Title = "About Us"
	p.Body = "<p>updated</p>"
	p.Slug = "/about-us"
	p.TemplateID = 2
	p.Status = domain.PageDraft
	p.OGBGImagePath = "bg.png"
	if err := s.UpdatePage(ctx, *p); err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}
	updated, err := s.PageByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("PageByID after update: %v", err)
	}
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

	// DeletePage
	if err := s.DeletePage(ctx, 1, id); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	_, err = s.PageByID(ctx, 1, id)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
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
	if err != ErrSlugInUse {
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
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPageDeleteNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.DeletePage(ctx, 1, 9999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
