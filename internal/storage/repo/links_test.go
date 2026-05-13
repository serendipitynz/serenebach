package repo

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestLinkCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id := linkCRUDCreate(t, ctx, s)
	l := linkCRUDReadByID(t, ctx, s, id)
	linkCRUDUpdate(t, ctx, s, id, l)
	linkCRUDDelete(t, ctx, s, id)
}

func linkCRUDCreate(t *testing.T, ctx context.Context, s *Store) int64 {
	t.Helper()
	id, err := s.CreateLink(ctx, domain.Link{
		WID:       1,
		Name:      "Example",
		URL:       "https://example.com",
		Kind:      domain.LinkKindLink,
		SortOrder: 10,
		Disp:      0,
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
	return id
}

func linkCRUDReadByID(t *testing.T, ctx context.Context, s *Store, id int64) *domain.Link {
	t.Helper()
	l, err := s.LinkByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("LinkByID: %v", err)
	}
	assertLinkFields(t, l)
	return l
}

func assertLinkFields(t *testing.T, l *domain.Link) {
	t.Helper()
	if l.Name != "Example" {
		t.Errorf("name = %q, want Example", l.Name)
	}
	if l.URL != "https://example.com" {
		t.Errorf("url = %q, want https://example.com", l.URL)
	}
	if l.Kind != domain.LinkKindLink {
		t.Errorf("kind = %q, want link", l.Kind)
	}
	if l.SortOrder != 10 {
		t.Errorf("sort_order = %d, want 10", l.SortOrder)
	}
	if l.Disp != 0 {
		t.Errorf("disp = %d, want 0", l.Disp)
	}
}

func linkCRUDUpdate(t *testing.T, ctx context.Context, s *Store, id int64, l *domain.Link) {
	t.Helper()
	l.Name = "Example Updated"
	l.URL = "https://example.com/updated"
	l.Disp = 1
	l.SortOrder = 20
	if err := s.UpdateLink(ctx, *l); err != nil {
		t.Fatalf("UpdateLink: %v", err)
	}
	updated, err := s.LinkByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("LinkByID after update: %v", err)
	}
	assertLinkUpdated(t, updated)
}

func assertLinkUpdated(t *testing.T, updated *domain.Link) {
	t.Helper()
	if updated.Name != "Example Updated" {
		t.Errorf("name after update = %q, want Example Updated", updated.Name)
	}
	if updated.URL != "https://example.com/updated" {
		t.Errorf("url after update = %q", updated.URL)
	}
	if updated.Disp != 1 {
		t.Errorf("disp after update = %d, want 1", updated.Disp)
	}
	if updated.SortOrder != 20 {
		t.Errorf("sort_order after update = %d, want 20", updated.SortOrder)
	}
}

func linkCRUDDelete(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	if err := s.DeleteLink(ctx, 1, id); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	if _, err := s.LinkByID(ctx, 1, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestVisibleLinksFiltersByDisp(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "Visible", URL: "https://a.com",
		Kind: domain.LinkKindLink, SortOrder: 1, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink visible: %v", err)
	}
	_, err = s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "Hidden", URL: "https://b.com",
		Kind: domain.LinkKindLink, SortOrder: 2, Disp: 1,
	})
	if err != nil {
		t.Fatalf("CreateLink hidden: %v", err)
	}

	all, err := s.AllLinks(ctx, 1)
	if err != nil {
		t.Fatalf("AllLinks: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("AllLinks len = %d, want 2", len(all))
	}

	visible, err := s.VisibleLinks(ctx, 1)
	if err != nil {
		t.Fatalf("VisibleLinks: %v", err)
	}
	if len(visible) != 1 {
		t.Fatalf("VisibleLinks len = %d, want 1", len(visible))
	}
	if visible[0].Name != "Visible" {
		t.Errorf("visible name = %q, want Visible", visible[0].Name)
	}
}

func TestLinkOrdering(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	links := []domain.Link{
		{WID: 1, Name: "C", URL: "https://c.com", Kind: domain.LinkKindLink, SortOrder: 30, Disp: 0},
		{WID: 1, Name: "A", URL: "https://a.com", Kind: domain.LinkKindLink, SortOrder: 10, Disp: 0},
		{WID: 1, Name: "B", URL: "https://b.com", Kind: domain.LinkKindLink, SortOrder: 20, Disp: 0},
	}
	for i := range links {
		_, err := s.CreateLink(ctx, links[i])
		if err != nil {
			t.Fatalf("CreateLink %q: %v", links[i].Name, err)
		}
	}

	all, err := s.AllLinks(ctx, 1)
	if err != nil {
		t.Fatalf("AllLinks: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	if all[0].Name != "A" || all[1].Name != "B" || all[2].Name != "C" {
		t.Errorf("order = %q %q %q, want A B C", all[0].Name, all[1].Name, all[2].Name)
	}
}

func TestLinkWIDScoping(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id1, err := s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "W1", URL: "https://w1.com",
		Kind: domain.LinkKindLink, SortOrder: 1, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink wid=1: %v", err)
	}
	id2, err := s.CreateLink(ctx, domain.Link{
		WID: 2, Name: "W2", URL: "https://w2.com",
		Kind: domain.LinkKindLink, SortOrder: 1, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink wid=2: %v", err)
	}

	// LinkByID with correct wid
	l1, err := s.LinkByID(ctx, 1, id1)
	if err != nil {
		t.Fatalf("LinkByID wid=1: %v", err)
	}
	if l1.Name != "W1" {
		t.Errorf("name = %q, want W1", l1.Name)
	}

	// LinkByID with wrong wid should return ErrNotFound
	_, err = s.LinkByID(ctx, 2, id1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for wrong wid, got %v", err)
	}
	_, err = s.LinkByID(ctx, 1, id2)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for wrong wid, got %v", err)
	}

	// AllLinks should be scoped
	all1, err := s.AllLinks(ctx, 1)
	if err != nil {
		t.Fatalf("AllLinks wid=1: %v", err)
	}
	if len(all1) != 1 {
		t.Fatalf("AllLinks wid=1 len = %d, want 1", len(all1))
	}
	if all1[0].Name != "W1" {
		t.Errorf("name = %q, want W1", all1[0].Name)
	}

	all2, err := s.AllLinks(ctx, 2)
	if err != nil {
		t.Fatalf("AllLinks wid=2: %v", err)
	}
	if len(all2) != 1 {
		t.Fatalf("AllLinks wid=2 len = %d, want 1", len(all2))
	}
	if all2[0].Name != "W2" {
		t.Errorf("name = %q, want W2", all2[0].Name)
	}
}

func TestLinkNotFoundErrors(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// LinkByID with non-existent id
	_, err := s.LinkByID(ctx, 1, 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for LinkByID, got %v", err)
	}

	// UpdateLink with non-existent id
	err = s.UpdateLink(ctx, domain.Link{ID: 9999, WID: 1, Name: "X", URL: "https://x.com", Kind: domain.LinkKindLink})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for UpdateLink, got %v", err)
	}

	// DeleteLink with non-existent id
	err = s.DeleteLink(ctx, 1, 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for DeleteLink, got %v", err)
	}
}

func TestLinkDeleteDetachesMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create a group
	groupID, err := s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "Group", URL: "",
		Kind: domain.LinkKindGroup, SortOrder: 1, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink group: %v", err)
	}

	// Create a child link under the group
	childID, err := s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "Child", URL: "https://child.com",
		Kind: domain.LinkKindLink, ParentID: groupID, SortOrder: 1, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink child: %v", err)
	}

	// Verify child has parent_id set
	child, err := s.LinkByID(ctx, 1, childID)
	if err != nil {
		t.Fatalf("LinkByID child: %v", err)
	}
	if child.ParentID != groupID {
		t.Errorf("child.ParentID = %d, want %d", child.ParentID, groupID)
	}

	// Delete the group
	if err := s.DeleteLink(ctx, 1, groupID); err != nil {
		t.Fatalf("DeleteLink group: %v", err)
	}

	// Child should survive with parent_id=0
	after, err := s.LinkByID(ctx, 1, childID)
	if err != nil {
		t.Fatalf("LinkByID child after delete: %v", err)
	}
	if after.ParentID != 0 {
		t.Errorf("child.ParentID after group delete = %d, want 0", after.ParentID)
	}
}

func TestLinkCountInGroup(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	groupID, err := s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "Group", URL: "",
		Kind: domain.LinkKindGroup, SortOrder: 1, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink group: %v", err)
	}

	n, err := s.CountLinksInGroup(ctx, 1, groupID)
	if err != nil {
		t.Fatalf("CountLinksInGroup empty: %v", err)
	}
	if n != 0 {
		t.Errorf("empty count = %d, want 0", n)
	}

	for i := 0; i < 3; i++ {
		_, err := s.CreateLink(ctx, domain.Link{
			WID: 1, Name: "Child", URL: "https://x.com",
			Kind: domain.LinkKindLink, ParentID: groupID, SortOrder: i + 1, Disp: 0,
		})
		if err != nil {
			t.Fatalf("CreateLink child %d: %v", i, err)
		}
	}

	n, err = s.CountLinksInGroup(ctx, 1, groupID)
	if err != nil {
		t.Fatalf("CountLinksInGroup: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
}

func TestLinkDefaultSortOrder(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create without explicit SortOrder (0 means "auto")
	id1, err := s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "First", URL: "https://first.com",
		Kind: domain.LinkKindLink, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink first: %v", err)
	}
	id2, err := s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "Second", URL: "https://second.com",
		Kind: domain.LinkKindLink, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink second: %v", err)
	}

	first, _ := s.LinkByID(ctx, 1, id1)
	second, _ := s.LinkByID(ctx, 1, id2)

	if first.SortOrder != 1 {
		t.Errorf("first SortOrder = %d, want 1", first.SortOrder)
	}
	if second.SortOrder != 2 {
		t.Errorf("second SortOrder = %d, want 2", second.SortOrder)
	}
}

func TestLinkReorder(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	ids := make([]int64, 3)
	for i := 0; i < 3; i++ {
		id, err := s.CreateLink(ctx, domain.Link{
			WID: 1, Name: fmt.Sprintf("Link %d", i), URL: fmt.Sprintf("https://%d.com", i),
			Kind: domain.LinkKindLink, SortOrder: i, Disp: 0,
		})
		if err != nil {
			t.Fatalf("CreateLink %d: %v", i, err)
		}
		ids[i] = id
	}

	// Reverse the order
	if err := s.ReorderLinks(ctx, 1, []int64{ids[2], ids[1], ids[0]}); err != nil {
		t.Fatalf("ReorderLinks: %v", err)
	}

	all, _ := s.AllLinks(ctx, 1)
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	if all[0].ID != ids[2] || all[1].ID != ids[1] || all[2].ID != ids[0] {
		t.Errorf("order = %d %d %d, want %d %d %d", all[0].ID, all[1].ID, all[2].ID, ids[2], ids[1], ids[0])
	}
}

func TestLinkReorderInGroup(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	groupID, err := s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "Group", URL: "",
		Kind: domain.LinkKindGroup, SortOrder: 1, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink group: %v", err)
	}

	// Also add a root-level link to confirm it's unaffected by group reorder
	_, err = s.CreateLink(ctx, domain.Link{
		WID: 1, Name: "Root", URL: "https://root.com",
		Kind: domain.LinkKindLink, SortOrder: 1, Disp: 0,
	})
	if err != nil {
		t.Fatalf("CreateLink root: %v", err)
	}

	ids := make([]int64, 3)
	for i := 0; i < 3; i++ {
		id, err := s.CreateLink(ctx, domain.Link{
			WID: 1, Name: fmt.Sprintf("Child %d", i), URL: fmt.Sprintf("https://c%d.com", i),
			Kind: domain.LinkKindLink, ParentID: groupID, SortOrder: i, Disp: 0,
		})
		if err != nil {
			t.Fatalf("CreateLink child %d: %v", i, err)
		}
		ids[i] = id
	}

	if err := s.ReorderLinksInGroup(ctx, 1, groupID, []int64{ids[2], ids[1], ids[0]}); err != nil {
		t.Fatalf("ReorderLinksInGroup: %v", err)
	}

	all, _ := s.AllLinks(ctx, 1)
	// Order: group, then its children (by sort_order), then root
	foundChildren := 0
	for _, l := range all {
		if l.ParentID == groupID {
			foundChildren++
			if foundChildren == 1 && l.ID != ids[2] {
				t.Errorf("first child id = %d, want %d", l.ID, ids[2])
			}
		}
	}
	if foundChildren != 3 {
		t.Errorf("found %d children, want 3", foundChildren)
	}
}
