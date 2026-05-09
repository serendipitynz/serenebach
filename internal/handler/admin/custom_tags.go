package admin

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

const maxCustomTags = 50
const maxCustomTagValueBytes = 65535

var customTagNamePattern = regexp.MustCompile(`^custom_[a-z][a-z0-9_]{0,49}$`)

type customTagRow struct {
	domain.CustomTag
	Preview string
}

type customTagsPageData struct {
	pageBase
	Tags       []customTagRow
	Flash      string
	Error      string
	EditID     int64
	EditName   string
	EditValue  string
	NameError  string
	ValueError string
}

func (h *Handler) customTagList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tags, err := h.Store.ListCustomTags(ctx, h.wid())
	if err != nil {
		log.Printf("admin.customTagList: %v", err)
		http.Error(w, "failed to list custom tags", http.StatusInternalServerError)
		return
	}
	rows := make([]customTagRow, 0, len(tags))
	for _, t := range tags {
		preview := t.Value
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		rows = append(rows, customTagRow{CustomTag: t, Preview: preview})
	}

	data := customTagsPageData{
		pageBase: pageBase{
			Title:      tr(r, "customTags.title"),
			ActiveMenu: "templates",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Tags: rows,
	}

	// Inline edit state driven by query params (simple no-JS fallback).
	if idStr := r.URL.Query().Get("edit"); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			for _, t := range tags {
				if t.ID == id {
					data.EditID = id
					data.EditName = t.Name
					data.EditValue = t.Value
					break
				}
			}
		}
	}
	data.Flash = r.URL.Query().Get("ok")
	data.Error = r.URL.Query().Get("err")

	renderMain(w, r, pageCustomTags, data)
}

func (h *Handler) customTagCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	value := r.PostFormValue("value")

	if !strings.HasPrefix(name, "custom_") {
		name = "custom_" + name
	}

	valid, verr := validateCustomTag(ctx, h.Store, h.wid(), 0, name, value)
	if !valid {
		http.Redirect(w, r, root(r)+"/admin/templates/custom-tags?err="+urlEncode(verr), http.StatusFound)
		return
	}

	count, err := h.Store.CountCustomTags(ctx, h.wid())
	if err != nil {
		log.Printf("admin.customTagCreate: count: %v", err)
		http.Error(w, "failed to count custom tags", http.StatusInternalServerError)
		return
	}
	if count >= maxCustomTags {
		http.Redirect(w, r, root(r)+"/admin/templates/custom-tags?err="+urlEncode(tr(r, "customTags.error.limit")), http.StatusFound)
		return
	}

	_, err = h.Store.CreateCustomTag(ctx, domain.CustomTag{
		WID:   h.wid(),
		Name:  name,
		Value: value,
	})
	if err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			http.Redirect(w, r, root(r)+"/admin/templates/custom-tags?err="+urlEncode(tr(r, "customTags.error.duplicate")), http.StatusFound)
			return
		}
		log.Printf("admin.customTagCreate: %v", err)
		http.Error(w, "failed to create custom tag", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/templates/custom-tags?ok=created", http.StatusFound)
}

func (h *Handler) customTagUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	value := r.PostFormValue("value")

	if !strings.HasPrefix(name, "custom_") {
		name = "custom_" + name
	}

	valid, verr := validateCustomTag(ctx, h.Store, h.wid(), id, name, value)
	if !valid {
		http.Redirect(w, r, root(r)+"/admin/templates/custom-tags?edit="+strconv.FormatInt(id, 10)+"&err="+urlEncode(verr), http.StatusFound)
		return
	}

	err = h.Store.UpdateCustomTag(ctx, domain.CustomTag{
		ID:    id,
		WID:   h.wid(),
		Name:  name,
		Value: value,
	})
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, repo.ErrSlugInUse) {
			http.Redirect(w, r, root(r)+"/admin/templates/custom-tags?edit="+strconv.FormatInt(id, 10)+"&err="+urlEncode(tr(r, "customTags.error.duplicate")), http.StatusFound)
			return
		}
		log.Printf("admin.customTagUpdate: %v", err)
		http.Error(w, "failed to update custom tag", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/templates/custom-tags?ok=updated", http.StatusFound)
}

func (h *Handler) customTagDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := h.Store.DeleteCustomTag(ctx, h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.customTagDelete: %v", err)
		http.Error(w, "failed to delete custom tag", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/templates/custom-tags?ok=deleted", http.StatusFound)
}

func validateCustomTag(ctx context.Context, store *repo.Store, wid, id int64, name, value string) (bool, string) {
	if !customTagNamePattern.MatchString(name) {
		return false, "customTags.error.invalidName"
	}
	if len(value) > maxCustomTagValueBytes {
		return false, "customTags.error.valueTooLong"
	}
	return true, ""
}

func urlEncode(s string) string {
	return url.QueryEscape(s)
}
