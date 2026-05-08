package repo

import (
	"context"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestTagCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// CreateTag
	id, err := s.CreateTag(ctx, domain.Tag{WID: 1, Name: "Go Lang", Slug: "go-lang"})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	// TagByID
	tag, err := s.TagByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("TagByID: %v", err)
	}
	if tag.Name != "Go Lang" {
		t.Errorf("name = %q, want Go Lang", tag.Name)
	}
	if tag.Slug != "go-lang" {
		t.Errorf("slug = %q, want go-lang", tag.Slug)
	}
	if tag.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}

	// TagBySlug
	tag2, err := s.TagBySlug(ctx, 1, "go-lang")
	if err != nil {
		t.Fatalf("TagBySlug: %v", err)
	}
	if tag2.ID != id {
		t.Errorf("TagBySlug id = %d, want %d", tag2.ID, id)
	}

	// UpdateTag
	if err := s.UpdateTag(ctx, domain.Tag{
		ID: id, WID: 1, Name: "Golang", Slug: "golang",
	}); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	updated, err := s.TagByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("TagByID after update: %v", err)
	}
	if updated.Name != "Golang" {
		t.Errorf("name after update = %q, want Golang", updated.Name)
	}
	if updated.Slug != "golang" {
		t.Errorf("slug after update = %q, want golang", updated.Slug)
	}

	// DeleteTag
	if err := s.DeleteTag(ctx, 1, id); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	_, err = s.TagByID(ctx, 1, id)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestTagSlugUnique(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.CreateTag(ctx, domain.Tag{WID: 1, Name: "Tag A", Slug: "tag-a"})
	if err != nil {
		t.Fatalf("first CreateTag: %v", err)
	}

	_, err = s.CreateTag(ctx, domain.Tag{WID: 1, Name: "Tag B", Slug: "tag-a"})
	if err != ErrSlugInUse {
		t.Errorf("expected ErrSlugInUse for duplicate slug, got %v", err)
	}
}

func TestTagUpdateSlugUnique(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id1, _ := s.CreateTag(ctx, domain.Tag{WID: 1, Name: "Tag A", Slug: "tag-a"})
	id2, _ := s.CreateTag(ctx, domain.Tag{WID: 1, Name: "Tag B", Slug: "tag-b"})

	// Try to update tag 2 to use tag 1's slug
	err := s.UpdateTag(ctx, domain.Tag{ID: id2, WID: 1, Name: "Tag B", Slug: "tag-a"})
	if err != ErrSlugInUse {
		t.Errorf("expected ErrSlugInUse on update collision, got %v", err)
	}

	_ = id1
}

func TestTagOrdering(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tags := []string{"Zebra", "Alpha", "Beta"}
	for _, name := range tags {
		_, err := s.CreateTag(ctx, domain.Tag{
			WID: 1, Name: name, Slug: DeriveTagSlug(name),
		})
		if err != nil {
			t.Fatalf("CreateTag %q: %v", name, err)
		}
	}

	all, err := s.AllTags(ctx, 1)
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	if all[0].Name != "Alpha" || all[1].Name != "Beta" || all[2].Name != "Zebra" {
		t.Errorf("order = %q %q %q, want Alpha Beta Zebra", all[0].Name, all[1].Name, all[2].Name)
	}
}

func TestTagWIDScoping(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id1, _ := s.CreateTag(ctx, domain.Tag{WID: 1, Name: "Tag W1", Slug: "tag-w1"})
	id2, _ := s.CreateTag(ctx, domain.Tag{WID: 2, Name: "Tag W2", Slug: "tag-w2"})

	// TagByID with correct wid
	t1, err := s.TagByID(ctx, 1, id1)
	if err != nil {
		t.Fatalf("TagByID wid=1: %v", err)
	}
	if t1.Name != "Tag W1" {
		t.Errorf("name = %q, want Tag W1", t1.Name)
	}

	// TagByID with wrong wid should return ErrNotFound
	_, err = s.TagByID(ctx, 2, id1)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for wrong wid, got %v", err)
	}
	_, err = s.TagByID(ctx, 1, id2)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for wrong wid, got %v", err)
	}

	// AllTags should be scoped
	all1, _ := s.AllTags(ctx, 1)
	if len(all1) != 1 || all1[0].Name != "Tag W1" {
		t.Errorf("AllTags wid=1 invalid")
	}
	all2, _ := s.AllTags(ctx, 2)
	if len(all2) != 1 || all2[0].Name != "Tag W2" {
		t.Errorf("AllTags wid=2 invalid")
	}
}

func TestTagNotFoundErrors(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.TagByID(ctx, 1, 9999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for TagByID, got %v", err)
	}

	_, err = s.TagBySlug(ctx, 1, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for TagBySlug, got %v", err)
	}

	err = s.UpdateTag(ctx, domain.Tag{ID: 9999, WID: 1, Name: "X", Slug: "x"})
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for UpdateTag, got %v", err)
	}

	err = s.DeleteTag(ctx, 1, 9999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for DeleteTag, got %v", err)
	}
}

func TestEnsureTagsByName(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Creates tags that don't exist
	tags, err := s.EnsureTagsByName(ctx, 1, []string{"Go", "Rust", "Python"})
	if err != nil {
		t.Fatalf("EnsureTagsByName create: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("len = %d, want 3", len(tags))
	}

	// Deduplicates input
	tags2, err := s.EnsureTagsByName(ctx, 1, []string{"Go", "Go", "Rust"})
	if err != nil {
		t.Fatalf("EnsureTagsByName dedup: %v", err)
	}
	if len(tags2) != 2 {
		t.Fatalf("dedup len = %d, want 2", len(tags2))
	}

	// Returns existing tags without creating new ones
	tags3, err := s.EnsureTagsByName(ctx, 1, []string{"Go", "Rust", "Python"})
	if err != nil {
		t.Fatalf("EnsureTagsByName existing: %v", err)
	}
	if len(tags3) != 3 {
		t.Fatalf("existing len = %d, want 3", len(tags3))
	}

	// Verify we still have exactly 3 tags total
	all, _ := s.AllTags(ctx, 1)
	if len(all) != 3 {
		t.Errorf("AllTags len = %d, want 3 (no dupes created)", len(all))
	}
}

func TestSetEntryTags(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tagIDs := make([]int64, 3)
	for i, name := range []string{"Go", "Rust", "Python"} {
		id, err := s.CreateTag(ctx, domain.Tag{WID: 1, Name: name, Slug: DeriveTagSlug(name)})
		if err != nil {
			t.Fatalf("CreateTag %q: %v", name, err)
		}
		tagIDs[i] = id
	}

	// Create an entry (without tags first)
	entryID, err := s.CreateEntry(ctx, domain.Entry{
		WID: 1, AuthorID: 1, Title: "Test Entry", Body: "body",
		Format: "markdown", Status: domain.EntryPublished,
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	// Assign tags
	if err := s.SetEntryTags(ctx, entryID, tagIDs); err != nil {
		t.Fatalf("SetEntryTags: %v", err)
	}

	// Verify tags are attached
	entryTags, err := s.TagsByEntry(ctx, entryID)
	if err != nil {
		t.Fatalf("TagsByEntry: %v", err)
	}
	if len(entryTags) != 3 {
		t.Fatalf("tags len = %d, want 3", len(entryTags))
	}

	// Replace with subset
	if err := s.SetEntryTags(ctx, entryID, tagIDs[:1]); err != nil {
		t.Fatalf("SetEntryTags replace: %v", err)
	}
	entryTags, _ = s.TagsByEntry(ctx, entryID)
	if len(entryTags) != 1 {
		t.Errorf("tags len after replace = %d, want 1", len(entryTags))
	}

	// Count tags per entry
	count, err := s.TagEntryCount(ctx, tagIDs[0])
	if err != nil {
		t.Fatalf("TagEntryCount: %v", err)
	}
	if count != 1 {
		t.Errorf("entry count for tag 0 = %d, want 1", count)
	}
}

func TestPublishedEntriesByTagFiltering(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tagID, err := s.CreateTag(ctx, domain.Tag{WID: 1, Name: "Test", Slug: "test"})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	otherTagID, err := s.CreateTag(ctx, domain.Tag{WID: 2, Name: "Other", Slug: "other"})
	if err != nil {
		t.Fatalf("CreateTag wid=2: %v", err)
	}

	// Create published entries for WID 1 tagged "test"
	pub1, err := s.CreateEntry(ctx, domain.Entry{
		WID: 1, AuthorID: 1, Title: "Published 1", Body: "body",
		Format: "markdown", Status: domain.EntryPublished,
	})
	if err != nil {
		t.Fatalf("CreateEntry pub1: %v", err)
	}
	pub2, err := s.CreateEntry(ctx, domain.Entry{
		WID: 1, AuthorID: 1, Title: "Published 2", Body: "body",
		Format: "markdown", Status: domain.EntryPublished,
	})
	if err != nil {
		t.Fatalf("CreateEntry pub2: %v", err)
	}

	// Create draft and closed entries for WID 1 tagged "test"
	draftID, err := s.CreateEntry(ctx, domain.Entry{
		WID: 1, AuthorID: 1, Title: "Draft", Body: "body",
		Format: "markdown", Status: domain.EntryDraft,
	})
	if err != nil {
		t.Fatalf("CreateEntry draft: %v", err)
	}
	closedID, err := s.CreateEntry(ctx, domain.Entry{
		WID: 1, AuthorID: 1, Title: "Closed", Body: "body",
		Format: "markdown", Status: domain.EntryClosed,
	})
	if err != nil {
		t.Fatalf("CreateEntry closed: %v", err)
	}

	// Create an entry on WID 2 tagged "other" (different WID, same tag concept)
	otherEntryID, err := s.CreateEntry(ctx, domain.Entry{
		WID: 2, AuthorID: 1, Title: "Other WID", Body: "body",
		Format: "markdown", Status: domain.EntryPublished,
	})
	if err != nil {
		t.Fatalf("CreateEntry wid=2: %v", err)
	}

	// Assign tag to all WID 1 entries
	if err := s.SetEntryTags(ctx, pub1, []int64{tagID}); err != nil {
		t.Fatalf("SetEntryTags pub1: %v", err)
	}
	if err := s.SetEntryTags(ctx, pub2, []int64{tagID}); err != nil {
		t.Fatalf("SetEntryTags pub2: %v", err)
	}
	if err := s.SetEntryTags(ctx, draftID, []int64{tagID}); err != nil {
		t.Fatalf("SetEntryTags draft: %v", err)
	}
	if err := s.SetEntryTags(ctx, closedID, []int64{tagID}); err != nil {
		t.Fatalf("SetEntryTags closed: %v", err)
	}
	// Other WID entry tagged with otherTagID
	if err := s.SetEntryTags(ctx, otherEntryID, []int64{otherTagID}); err != nil {
		t.Fatalf("SetEntryTags other: %v", err)
	}

	// PublishedEntriesByTag should return only published entries from WID 1
	entries, err := s.PublishedEntriesByTag(ctx, 1, tagID, 100)
	if err != nil {
		t.Fatalf("PublishedEntriesByTag: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("PublishedEntriesByTag len = %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Status != domain.EntryPublished {
			t.Errorf("unexpected status %d for entry %q", e.Status, e.Title)
		}
		if e.WID != 1 {
			t.Errorf("unexpected wid %d for entry %q", e.WID, e.Title)
		}
	}

	// CountPublishedEntriesByTag should count only published from this WID
	count, err := s.CountPublishedEntriesByTag(ctx, 1, tagID)
	if err != nil {
		t.Fatalf("CountPublishedEntriesByTag: %v", err)
	}
	if count != 2 {
		t.Errorf("CountPublishedEntriesByTag = %d, want 2", count)
	}

	// PublishedEntriesByTagPage with limit=1 offset=0
	page, err := s.PublishedEntriesByTagPage(ctx, 1, tagID, 1, 0)
	if err != nil {
		t.Fatalf("PublishedEntriesByTagPage page 1: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("page 1 len = %d, want 1", len(page))
	}
	// Second page
	page2, err := s.PublishedEntriesByTagPage(ctx, 1, tagID, 1, 1)
	if err != nil {
		t.Fatalf("PublishedEntriesByTagPage page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("page 2 len = %d, want 1", len(page2))
	}
	if page[0].ID == page2[0].ID {
		t.Error("page 1 and page 2 should have different entries")
	}
}

func TestDeriveTagSlug(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Go Lang", "go-lang"},
		{"  Spaces  ", "spaces"},
		{"UpperCASE", "uppercase"},
		{"a-b-c", "a-b-c"},
	}
	for _, tc := range tests {
		got := DeriveTagSlug(tc.name)
		if got != tc.want {
			t.Errorf("DeriveTagSlug(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}

	// Non-ASCII fallback produces a t-* slug
	slug := DeriveTagSlug("日本語")
	if len(slug) < 3 || slug[:2] != "t-" {
		t.Errorf("DeriveTagSlug non-ASCII = %q, want t-*", slug)
	}
}

func TestIsValidTagSlug(t *testing.T) {
	if !IsValidTagSlug("valid-slug") {
		t.Error("expected true for valid-slug")
	}
	if IsValidTagSlug("") {
		t.Error("expected false for empty")
	}
	if IsValidTagSlug("UPPERCASE") {
		t.Error("expected false for uppercase")
	}
	if IsValidTagSlug("has spaces") {
		t.Error("expected false for spaces")
	}
	if IsValidTagSlug("-starts-hyphen") {
		t.Error("expected false for leading hyphen")
	}
	if IsValidTagSlug("ends-hyphen-") {
		t.Error("expected false for trailing hyphen")
	}
}

func TestCreateTagValidation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.CreateTag(ctx, domain.Tag{WID: 1, Name: "", Slug: "slug"})
	if err == nil {
		t.Error("expected error for empty name")
	}

	_, err = s.CreateTag(ctx, domain.Tag{WID: 1, Name: "Name", Slug: ""})
	if err == nil {
		t.Error("expected error for empty slug")
	}
}
