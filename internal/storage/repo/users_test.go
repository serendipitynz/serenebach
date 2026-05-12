package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestUserCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// CreateUser
	id, err := s.CreateUser(ctx, domain.User{
		WID:         1,
		Name:        "testuser",
		DisplayName: "Test User",
		Email:       "test@example.com",
		Role:        domain.RoleRegular,
		Description: "Hello world",
		SortOrder:   1,
	}, "hashed-password")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	// UserByID
	u, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Name != "testuser" {
		t.Errorf("name = %q, want testuser", u.Name)
	}
	if u.DisplayName != "Test User" {
		t.Errorf("display_name = %q, want Test User", u.DisplayName)
	}
	if u.Role != domain.RoleRegular {
		t.Errorf("role = %d, want %d", u.Role, domain.RoleRegular)
	}
	if u.Description != "Hello world" {
		t.Errorf("description = %q, want Hello world", u.Description)
	}

	// UpdateUser
	u.DisplayName = "Test User Updated"
	u.Role = domain.RolePower
	u.Description = "Updated bio"
	if err := s.UpdateUser(ctx, *u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	updated, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("UserByID after update: %v", err)
	}
	if updated.DisplayName != "Test User Updated" {
		t.Errorf("display_name after update = %q, want Test User Updated", updated.DisplayName)
	}
	if updated.Role != domain.RolePower {
		t.Errorf("role after update = %d, want %d", updated.Role, domain.RolePower)
	}
	if updated.Description != "Updated bio" {
		t.Errorf("description after update = %q, want Updated bio", updated.Description)
	}

	// DeleteUser
	if err := s.DeleteUser(ctx, 1, id); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	_, err = s.UserByID(ctx, id)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestUserNameUnique(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.CreateUser(ctx, domain.User{
		WID: 1, Name: "dupname", DisplayName: "A", Email: "a@a.com",
		Role: domain.RoleRegular,
	}, "hash1")
	if err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}

	_, err = s.CreateUser(ctx, domain.User{
		WID: 1, Name: "dupname", DisplayName: "B", Email: "b@b.com",
		Role: domain.RoleRegular,
	}, "hash2")
	if !errors.Is(err, ErrUserNameInUse) {
		t.Errorf("expected ErrUserNameInUse, got %v", err)
	}
}

func TestUserUpdateNameUnique(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id1, _ := s.CreateUser(ctx, domain.User{
		WID: 1, Name: "user_a", DisplayName: "A", Email: "a@a.com",
		Role: domain.RoleRegular,
	}, "hash")
	id2, _ := s.CreateUser(ctx, domain.User{
		WID: 1, Name: "user_b", DisplayName: "B", Email: "b@b.com",
		Role: domain.RoleRegular,
	}, "hash")

	_ = id1
	_ = id2

	err := s.UpdateUser(ctx, domain.User{
		ID: id2, WID: 1, Name: "user_a", DisplayName: "B", Email: "b@b.com",
		Role: domain.RoleRegular,
	})
	if !errors.Is(err, ErrUserNameInUse) {
		t.Errorf("expected ErrUserNameInUse on update, got %v", err)
	}
}

func TestUserOrdering(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	ids := make([]int64, 3)
	for i, name := range []string{"user_c", "user_a", "user_b"} {
		id, err := s.CreateUser(ctx, domain.User{
			WID: 1, Name: name, DisplayName: name, Email: name + "@test.com",
			Role: domain.RoleRegular, SortOrder: i,
		}, "hash")
		if err != nil {
			t.Fatalf("CreateUser %q: %v", name, err)
		}
		ids[i] = id
	}

	all, err := s.ListUsers(ctx, 1)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	// Should be ordered by sort_order, then id
	if all[0].SortOrder != 0 || all[1].SortOrder != 1 || all[2].SortOrder != 2 {
		t.Errorf("sort order: %d %d %d, want 0 1 2",
			all[0].SortOrder, all[1].SortOrder, all[2].SortOrder)
	}

	// Reorder
	if err := s.ReorderUsers(ctx, 1, []int64{ids[2], ids[1], ids[0]}); err != nil {
		t.Fatalf("ReorderUsers: %v", err)
	}
	reordered, _ := s.ListUsers(ctx, 1)
	if reordered[0].ID != ids[2] || reordered[1].ID != ids[1] || reordered[2].ID != ids[0] {
		t.Errorf("reorder failed: %d %d %d, want %d %d %d",
			reordered[0].ID, reordered[1].ID, reordered[2].ID, ids[2], ids[1], ids[0])
	}
}

func TestVisibleProfileUsersFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.CreateUser(ctx, domain.User{
		WID: 1, Name: "visible_user", DisplayName: "V", Email: "v@v.com",
		Role: domain.RoleRegular, ListVisible: true, SortOrder: 1,
	}, "hash")
	if err != nil {
		t.Fatalf("CreateUser visible: %v", err)
	}
	_, err = s.CreateUser(ctx, domain.User{
		WID: 1, Name: "hidden_user", DisplayName: "H", Email: "h@h.com",
		Role: domain.RoleRegular, ListVisible: false, SortOrder: 2,
	}, "hash")
	if err != nil {
		t.Fatalf("CreateUser hidden: %v", err)
	}

	all, _ := s.ListUsers(ctx, 1)
	if len(all) != 2 {
		t.Fatalf("ListUsers len = %d, want 2", len(all))
	}

	visible, err := s.VisibleProfileUsers(ctx, 1)
	if err != nil {
		t.Fatalf("VisibleProfileUsers: %v", err)
	}
	if len(visible) != 1 {
		t.Fatalf("VisibleProfileUsers len = %d, want 1", len(visible))
	}
	if visible[0].Name != "visible_user" {
		t.Errorf("visible name = %q, want visible_user", visible[0].Name)
	}
}

func TestUserWIDScoping(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id1, _ := s.CreateUser(ctx, domain.User{
		WID: 1, Name: "wid1_user", DisplayName: "U1", Email: "u1@u.com",
		Role: domain.RoleRegular,
	}, "hash")
	id2, _ := s.CreateUser(ctx, domain.User{
		WID: 2, Name: "wid2_user", DisplayName: "U2", Email: "u2@u.com",
		Role: domain.RoleRegular,
	}, "hash")

	_ = id1
	_ = id2

	// ListUsers should be scoped by WID
	all1, _ := s.ListUsers(ctx, 1)
	if len(all1) != 1 || all1[0].Name != "wid1_user" {
		t.Errorf("ListUsers wid=1 invalid")
	}
	all2, _ := s.ListUsers(ctx, 2)
	if len(all2) != 1 || all2[0].Name != "wid2_user" {
		t.Errorf("ListUsers wid=2 invalid")
	}
}

func TestUserNotFoundErrors(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.UserByID(ctx, 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for UserByID, got %v", err)
	}

	err = s.UpdateUser(ctx, domain.User{
		ID: 9999, Name: "x", DisplayName: "X", Email: "x@x.com",
		Role: domain.RoleRegular,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for UpdateUser, got %v", err)
	}

	err = s.UpdateUserAIConfig(ctx, 9999, domain.User{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for UpdateUserAIConfig, got %v", err)
	}

	err = s.UpdateUserPassword(ctx, 9999, "hash")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for UpdateUserPassword, got %v", err)
	}

	err = s.DeleteUser(ctx, 1, 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for DeleteUser, got %v", err)
	}
}

func TestUserAIConfig(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, _ := s.CreateUser(ctx, domain.User{
		WID: 1, Name: "ai_user", DisplayName: "AI", Email: "ai@ai.com",
		Role: domain.RoleRegular,
	}, "hash")

	err := s.UpdateUserAIConfig(ctx, id, domain.User{
		AIKind:           "openai-compat",
		AIBaseURL:        "http://localhost:1234/v1",
		AIModel:          "qwen2-5",
		AIAPIKeyEnc:      "encrypted-key",
		AIAutoAlt:        true,
		AITimeoutSeconds: 120,
	})
	if err != nil {
		t.Fatalf("UpdateUserAIConfig: %v", err)
	}

	u, _ := s.UserByID(ctx, id)
	if u.AIKind != "openai-compat" {
		t.Errorf("ai_kind = %q, want openai-compat", u.AIKind)
	}
	if u.AIBaseURL != "http://localhost:1234/v1" {
		t.Errorf("ai_base_url = %q", u.AIBaseURL)
	}
	if u.AIModel != "qwen2-5" {
		t.Errorf("ai_model = %q", u.AIModel)
	}
	if u.AIAPIKeyEnc != "encrypted-key" {
		t.Errorf("ai_api_key_enc = %q", u.AIAPIKeyEnc)
	}
	if !u.AIAutoAlt {
		t.Error("expected ai_auto_alt = true")
	}
	if u.AITimeoutSeconds != 120 {
		t.Errorf("ai_timeout_seconds = %d, want 120", u.AITimeoutSeconds)
	}

	// Clear AI config
	err = s.UpdateUserAIConfig(ctx, id, domain.User{
		AIKind: "", // Empty means disabled
	})
	if err != nil {
		t.Fatalf("UpdateUserAIConfig clear: %v", err)
	}
	cleared, _ := s.UserByID(ctx, id)
	if cleared.AIKind != "" {
		t.Errorf("ai_kind after clear = %q, want empty", cleared.AIKind)
	}
}

func TestUserPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, _ := s.CreateUser(ctx, domain.User{
		WID: 1, Name: "pw_user", DisplayName: "PW", Email: "pw@pw.com",
		Role: domain.RoleRegular,
	}, "initial-hash")

	err := s.UpdateUserPassword(ctx, id, "new-hash")
	if err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	// Success: no error. The hash is verified via UserByName in integration tests.
}

func TestHasAdminUserAndCountAdmins(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// No admin user yet
	has, err := s.HasAdminUser(ctx)
	if err != nil {
		t.Fatalf("HasAdminUser: %v", err)
	}
	if has {
		t.Error("expected HasAdminUser = false with no admin")
	}

	count, err := s.CountAdmins(ctx, 1)
	if err != nil {
		t.Fatalf("CountAdmins empty: %v", err)
	}
	if count != 0 {
		t.Errorf("CountAdmins = %d, want 0", count)
	}

	// Create an admin
	_, err = s.CreateUser(ctx, domain.User{
		WID: 1, Name: "admin_user", DisplayName: "Admin", Email: "admin@admin.com",
		Role: domain.RoleAdmin, SortOrder: 1,
	}, "hash")
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}

	has, err = s.HasAdminUser(ctx)
	if err != nil {
		t.Fatalf("HasAdminUser: %v", err)
	}
	if !has {
		t.Error("expected HasAdminUser = true after creating admin")
	}

	count, err = s.CountAdmins(ctx, 1)
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if count != 1 {
		t.Errorf("CountAdmins = %d, want 1", count)
	}
}

func TestUserDeleteClearsSessions(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, _ := s.CreateUser(ctx, domain.User{
		WID: 1, Name: "del_user", DisplayName: "DU", Email: "du@du.com",
		Role: domain.RoleRegular,
	}, "hash")

	// Create a session
	err := s.CreateSession(ctx, "session-token-123", id, time.Now().Add(24*time.Hour).Unix())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Verify session exists
	u, err := s.SessionUser(ctx, "session-token-123")
	if err != nil {
		t.Fatalf("SessionUser before delete: %v", err)
	}
	if u.ID != id {
		t.Errorf("session user id = %d, want %d", u.ID, id)
	}

	// Delete the user
	if err := s.DeleteUser(ctx, 1, id); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// Session should also be gone
	_, err = s.SessionUser(ctx, "session-token-123")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for session after user delete, got %v", err)
	}
}

func TestUserListVisibleDefault(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, err := s.CreateUser(ctx, domain.User{
		WID: 1, Name: "default_vis", DisplayName: "DV", Email: "dv@dv.com",
		Role: domain.RoleRegular,
	}, "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	u, _ := s.UserByID(ctx, id)
	if u.ListVisible {
		t.Error("expected ListVisible = false by default")
	}

	// Toggle list_visible on
	u.ListVisible = true
	if err := s.UpdateUser(ctx, *u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	updated, _ := s.UserByID(ctx, id)
	if !updated.ListVisible {
		t.Error("expected ListVisible = true after update")
	}

	// VisibleProfileUsers should now include this user
	visible, _ := s.VisibleProfileUsers(ctx, 1)
	if len(visible) != 1 || visible[0].ID != id {
		t.Errorf("VisibleProfileUsers should contain the user")
	}
}
