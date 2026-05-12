package public

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// LegacyStaticMiddleware intercepts SB3 static-archive URLs and 301s
// them to Go canonical paths. Off-pattern requests pass through to
// the next handler.
//
// Pattern surface (driven by Handler.LegacyURL):
//
//	{base_path}{log_path}{id_prefix}{N}{suffix}    → /entry/{key}/
//	{base_path}{log_path}{name}{suffix}            → /entry/{key}/  (legacy_file)
//	{base_path}{category_dir}/                     → /category/{id}/
//
// Trailing-slash semantics matter for the category dir match
// (categories are "{dir}/" with the slash, and the bare log_path
// itself must not match — that's the archive index, not a category).
// Because chi's StripSlashes middleware would otherwise eat the
// trailing slash before we see it, this middleware MUST be installed
// before StripSlashes.
//
// Monthly archive (legacy_archive_type == "Monthly") is documented as
// out of scope — the redirect path can't recover the per-entry anchor
// fragment that lived inside the YYYYMM.html page. The .html branches
// stay disabled when the imported blog ran on Monthly.
func (h *Handler) LegacyStaticMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !h.LegacyURL.HasAny() {
			next.ServeHTTP(w, r)
			return
		}
		if h.tryLegacyEntryHTML(w, r) {
			return
		}
		if h.tryLegacyCategoryDir(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tryLegacyEntryHTML attempts to match {base}{log}{key}{suffix} and
// 301 to the entry's canonical URL. Returns true when the response
// has been written.
func (h *Handler) tryLegacyEntryHTML(w http.ResponseWriter, r *http.Request) bool {
	cfg := h.LegacyURL
	// Monthly out of scope; HTML pattern requires log_path + suffix.
	if cfg.ArchiveType == "Monthly" || cfg.LogPath == "" || cfg.Suffix == "" {
		return false
	}
	rest, ok := stripLegacyPrefix(r.URL.Path, cfg.BasePath, cfg.LogPath)
	if !ok {
		return false
	}
	if !strings.HasSuffix(rest, cfg.Suffix) {
		return false
	}
	key := strings.TrimSuffix(rest, cfg.Suffix)
	if key == "" || strings.Contains(key, "/") {
		// Empty (= bare suffix at log root) or sub-path don't match any SB3 entry.
		return false
	}
	// id-form first: {prefix}{N}. legacy_file values can't start with
	// {prefix}+digits-only on a properly-administered SB3 (admin
	// restricts entry_file to \w+) so this ordering is safe.
	hasIDPrefix := cfg.IDPrefix != "" && strings.HasPrefix(key, cfg.IDPrefix)
	if n, perr := strconv.ParseInt(strings.TrimPrefix(key, cfg.IDPrefix), 10, 64); hasIDPrefix && perr == nil {
		ref, err := h.Store.EntryByLegacyID(r.Context(), h.WID, n)
		if err == nil {
			http.Redirect(w, r, root(r)+"/entry/"+entryKeyForRef(ref)+"/", http.StatusMovedPermanently)
			return true
		}
		if !errors.Is(err, repo.ErrNotFound) {
			http.Error(w, "lookup failed", http.StatusInternalServerError)
			return true
		}
		// Not found by legacy_id falls through to legacy_file in case
		// the operator stored "eid123" as a custom save name. This is
		// an edge case but cheap to support.
	}
	ref, err := h.Store.EntryByLegacyFile(r.Context(), h.WID, key)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return false
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return true
	}
	http.Redirect(w, r, root(r)+"/entry/"+entryKeyForRef(ref)+"/", http.StatusMovedPermanently)
	return true
}

// tryLegacyCategoryDir attempts to match {base_path}{category_dir}/
// where {category_dir} differs from the global {log_path} (otherwise
// the bare log root would be claimed by a category). Returns true
// when the response has been written.
func (h *Handler) tryLegacyCategoryDir(w http.ResponseWriter, r *http.Request) bool {
	cfg := h.LegacyURL
	rest, ok := stripBasePath(r.URL.Path, cfg.BasePath)
	if !ok {
		return false
	}
	if !strings.HasSuffix(rest, "/") {
		return false
	}
	dir := rest // includes trailing slash
	// A category dir equal to the global log_path is the "no custom
	// dir" default. Skip it so the redirect doesn't fight the
	// archive root or claim every Individual entry that lives there.
	if dir == cfg.LogPath {
		return false
	}
	id, err := h.Store.CategoryIDByLegacyDir(r.Context(), h.WID, dir)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return false
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return true
	}
	http.Redirect(w, r, root(r)+"/category/"+strconv.FormatInt(id, 10)+"/", http.StatusMovedPermanently)
	return true
}

// stripLegacyPrefix removes base_path (default "/") then log_path
// from the request URL and returns the remainder. The remainder has
// no leading slash. Both prefixes must be present in order for the
// URL to be considered "inside" the legacy log dir.
func stripLegacyPrefix(path, base, log string) (string, bool) {
	rest, ok := stripBasePath(path, base)
	if !ok {
		return "", false
	}
	if log == "" {
		return rest, true
	}
	if !strings.HasPrefix(rest, log) {
		return "", false
	}
	return rest[len(log):], true
}

// stripBasePath drops the base_path prefix from a request URL that
// always begins with "/". The result has no leading slash, ready to
// be compared against importer-stored values like "log/" or "travel/".
// base of "/" or "" is treated as the no-prefix case.
func stripBasePath(path, base string) (string, bool) {
	if !strings.HasPrefix(path, "/") {
		return "", false
	}
	if base == "" || base == "/" {
		return path[1:], true
	}
	if !strings.HasPrefix(path, base) {
		return "", false
	}
	return path[len(base):], true
}
