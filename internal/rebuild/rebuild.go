// Package rebuild produces a fully static snapshot of the public site into
// a target directory. It mirrors SB3's rebuild feature: render home, every
// entry permalink, every category, and every archive period, plus the
// active template's stylesheet. Files land under `<out>/<url>/index.html`
// so any static host can serve them without rewrites.
//
// Known limitation (2026-04): only page 1 of each list route is
// written — the paginator emits `?page=N` links that static hosts
// can't serve natively. Dynamic (`task dev` / CGI / embedded
// binary) deployments paginate fully.
package rebuild

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/feed"
	"github.com/serendipitynz/serenebach/internal/llmstxt"
	"github.com/serendipitynz/serenebach/internal/sitemap"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// DefaultEntryListSize caps the number of entries rendered on the home page
// of the static build. Matches the public handler's default.
const DefaultEntryListSize = 10

// Report summarises a single build run.
type Report struct {
	Home                 bool
	Entries              int
	Pages                int
	Categories           int
	Tags                 int
	ArchiveYear          int
	ArchiveMonth         int
	CSSWritten           bool
	ImagesCopied         int
	TemplateAssetsCopied int
	RSSWritten           bool
	AtomWritten          bool
	LLMSWritten          bool // both llms.txt + llms-full.txt (0 or 2 files)
	SitemapWritten       bool
	RobotsWritten        bool
	OutDir               string
}

// Options configures a build. Zero values use sensible defaults, except
// OutDir which the caller must supply.
type Options struct {
	OutDir         string
	WID            int64
	EntryListLimit int
	// BasePath is the URL prefix under which the static site will be served
	// (e.g. "/sb4"). Used as a fallback prefix in content.Site when the
	// weblog's BaseURL is not configured. Leave empty for root deployments.
	BasePath string
	// ImageDir, when set, is copied into OutDir/img so the static snapshot
	// carries its media alongside the HTML. Empty means "skip image copy"
	// (deployments that serve images dynamically pass "").
	ImageDir string
	// TemplateDir, when set, is mirrored into OutDir/template so the
	// static snapshot carries every template's asset folder alongside
	// the HTML. Empty means "skip template asset copy".
	TemplateDir string
	// TZ is the timezone used to bucket entries into year/month
	// archive ranges. Nil falls back to time.Local so older callers
	// (and tests) keep working. cmd/serenebach forwards config.TZ so
	// the deployed binary renders identical archives regardless of
	// the host clock.
	TZ *time.Location
}

// Build generates the full static snapshot.
//
// Output is produced via a stage-then-swap strategy: every managed
// subtree (entry/, category/, tag/, archive/) and every top-level
// file (index.html, style.css, rss.xml, atom.xml, llms*.txt) is
// first written into a hidden staging directory under OutDir. Only
// after every render + write succeeds are the staged subtrees swapped
// in (rename-based, replacing the previous output). If any earlier
// step fails — DB lookup, template load, render, or write — Build
// returns the error and the existing static snapshot stays intact.
// This matters for the auto-rebuild trigger, which only logs failures
// and lets the underlying save still succeed: a transient failure
// must never tear the live static site apart.
//
// Stale-removal semantics: the swap deletes deleted/unpublished/slug-
// changed entries and removed categories/tags/archive months because
// those paths simply do not appear in the freshly-built staging tree.
// llms.txt + llms-full.txt are also removed when the weblog has opted
// out of LLMS publishing so toggling the switch off cleans up.
//
// img/ and template/ are mirrors of external directories with their
// own lifecycles and are NOT staged — copyImageTree only adds files.
// Operators who manage those dirs separately are responsible for
// cleaning them.
func Build(ctx context.Context, store *repo.Store, opts Options) (*Report, error) {
	if opts.OutDir == "" {
		return nil, fmt.Errorf("rebuild: OutDir is required")
	}
	env, err := prepareBuildEnv(ctx, store, &opts)
	if err != nil {
		return nil, err
	}
	data, err := loadBuildData(ctx, store, opts.WID, opts.TZ)
	if err != nil {
		return nil, err
	}
	site := content.NewSite(*env.weblog).WithBasePath(opts.BasePath).WithCustomTags(data.customTags).WithTZ(opts.TZ)
	finalOut := opts.OutDir
	rep := &Report{OutDir: finalOut}

	staging, err := makeStagingDir(finalOut)
	if err != nil {
		return nil, err
	}
	// Always best-effort clean staging on exit. promoteStaging removes
	// it on the happy path; this defer covers every error return.
	defer os.RemoveAll(staging)

	// Redirect writers to staging via a copy of opts so the original
	// (used for rep.OutDir + image/template copies) keeps the real path.
	stagedOpts := opts
	stagedOpts.OutDir = staging

	pageRoots, err := writeStagedPages(ctx, store, stagedOpts, site, env, data, rep)
	if err != nil {
		return nil, err
	}
	if err := writeStagedExtras(staging, site, env.tmpl, env.weblog, data, rep); err != nil {
		return nil, err
	}

	// Every render + write succeeded. Swap the staged tree into
	// place; failures inside promoteStaging leave the previous
	// snapshot intact thanks to the rename-via-backup pattern.
	if err := promoteStaging(finalOut, staging, env.weblog.LLMSEnabled, env.weblog, pageRoots); err != nil {
		return nil, err
	}

	if err := writeAdditiveAssets(ctx, store, site, opts, finalOut, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

// buildEnv is the per-Build configuration snapshot resolved from opts
// and the weblog row: the weblog itself plus the active and
// archive-pinned templates. Built once at the top of Build so the
// downstream writers can rely on stable pointers.
type buildEnv struct {
	weblog      *domain.Weblog
	tmpl        *domain.Template
	archiveTmpl *domain.Template
}

// buildData is the read-side snapshot fetched once per Build so the
// downstream writers never have to call back into the repo. Each field
// is consumed by multiple writers (entries, categories, tags, ...) —
// loading them centrally keeps the SQL fan-out at O(1) regardless of
// how many static pages the rebuild emits.
type buildData struct {
	all          []domain.Entry
	cats         map[int64]domain.Category
	users        map[int64]domain.User
	profileUsers []domain.User
	sidebar      content.SidebarData
	customTags   []domain.CustomTag
	templates    map[int64]*domain.Template
	pages        []domain.Page
	tags         []domain.Tag
	catLastMods  map[int64]time.Time
	tagLastMods  map[int64]time.Time
}

// prepareBuildEnv resolves opts defaults (WID, TZ, EntryListLimit) and
// loads the weblog row plus the active / archive-pinned templates.
// opts is mutated in place so the caller's downstream reads of opts
// (BasePath, OutDir, EntryListLimit, ...) see the resolved values.
func prepareBuildEnv(ctx context.Context, store *repo.Store, opts *Options) (*buildEnv, error) {
	if opts.WID == 0 {
		opts.WID = 1
	}
	if opts.TZ == nil {
		opts.TZ = time.Local
	}
	weblog, err := store.WeblogByID(ctx, opts.WID)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load weblog: %w", err)
	}
	// Page size: explicit opts.EntryListLimit wins (tests / callers
	// that want a deterministic value); otherwise honour the author's
	// configured entries_per_page; finally fall back to the package
	// default if the column is still zero.
	if opts.EntryListLimit <= 0 {
		opts.EntryListLimit = weblog.EntriesPerPage
	}
	if opts.EntryListLimit <= 0 {
		opts.EntryListLimit = DefaultEntryListSize
	}
	tmpl, err := store.ActiveTemplate(ctx, opts.WID)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load active template: %w", err)
	}
	// Category + archive pages optionally use a pinned template. Falls
	// back to the active one when unset (or when the pinned id is
	// stale) so a misconfigured site still renders instead of 500'ing.
	archiveTmpl := tmpl
	if weblog.ArchiveTemplateID != 0 {
		if t, err := store.TemplateByID(ctx, opts.WID, weblog.ArchiveTemplateID); err == nil {
			archiveTmpl = t
		}
	}
	return &buildEnv{weblog: weblog, tmpl: tmpl, archiveTmpl: archiveTmpl}, nil
}

// loadBuildData pre-fetches every dataset the writers will need so the
// downstream loop avoids per-page DB calls.
func loadBuildData(ctx context.Context, store *repo.Store, wid int64, tz *time.Location) (*buildData, error) {
	// Every published entry is fetched once and re-used so a large blog
	// needs a single DB scan, not one per page.
	all, err := store.AllPublishedEntries(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load entries: %w", err)
	}
	cats, users, err := resolveRefs(ctx, store, all)
	if err != nil {
		return nil, err
	}
	// Pre-fetch the profile-area iteration slice once; every writer
	// passes it straight into ListView / EntryView so the renderer
	// never has to call back into the repo.
	profileUsers, err := store.VisibleProfileUsers(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load profile users: %w", err)
	}
	// SB3 sidebar block data is rebuilt once per Build() — identical
	// across every page the rebuild emits.
	sidebar, err := loadSidebarData(ctx, store, wid, tz)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load sidebar: %w", err)
	}
	customTags, err := store.ListCustomTags(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load custom tags: %w", err)
	}
	pages, err := store.PublishedPages(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load pages: %w", err)
	}
	tags, err := store.AllTags(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load tags: %w", err)
	}
	catLastMods, err := store.SitemapCategoryLastMods(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load category lastmods: %w", err)
	}
	tagLastMods, err := store.SitemapTagLastMods(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load tag lastmods: %w", err)
	}
	return &buildData{
		all:          all,
		cats:         cats,
		users:        users,
		profileUsers: profileUsers,
		sidebar:      sidebar,
		customTags:   customTags,
		templates:    map[int64]*domain.Template{},
		pages:        pages,
		tags:         tags,
		catLastMods:  catLastMods,
		tagLastMods:  tagLastMods,
	}, nil
}

// makeStagingDir creates OutDir/.sb-rebuild-XXXX/ so every page write
// in this rebuild lands somewhere a later failure can throw away
// without touching the live snapshot. Same-FS guarantee comes from
// putting staging *inside* OutDir, which keeps later rename(2) calls
// atomic.
func makeStagingDir(finalOut string) (string, error) {
	if err := os.MkdirAll(finalOut, 0o755); err != nil {
		return "", fmt.Errorf("rebuild: mkdir out: %w", err)
	}
	staging, err := os.MkdirTemp(finalOut, ".sb-rebuild-")
	if err != nil {
		return "", fmt.Errorf("rebuild: create staging dir: %w", err)
	}
	return staging, nil
}

// writeStagedPages drives the six page-tree writers (home, entries,
// pages, categories, tags, archives) in order. Returns the flat-page
// roots so the staging-swap can hand them to promoteStaging.
func writeStagedPages(ctx context.Context, store *repo.Store, stagedOpts Options, site content.Site, env *buildEnv, data *buildData, rep *Report) (map[string]struct{}, error) {
	staging := stagedOpts.OutDir
	if err := writeHome(ctx, store, staging, site, env.tmpl, data.all, data.cats, data.users, data.profileUsers, data.sidebar, stagedOpts.EntryListLimit); err != nil {
		return nil, err
	}
	rep.Home = true

	if err := writeEntries(ctx, store, stagedOpts, site, env.tmpl, env.weblog, data, rep); err != nil {
		return nil, err
	}
	pageRoots, err := writePages(ctx, store, stagedOpts, site, data, rep)
	if err != nil {
		return nil, err
	}
	if err := writeCategories(ctx, store, stagedOpts, site, env.archiveTmpl, data, rep); err != nil {
		return nil, err
	}
	if err := writeTags(ctx, store, stagedOpts, site, env.archiveTmpl, data.cats, data.users, data.profileUsers, data.sidebar, rep); err != nil {
		return nil, err
	}
	if err := writeArchives(ctx, store, stagedOpts, site, env.archiveTmpl, data.cats, data.users, data.profileUsers, data.sidebar, rep); err != nil {
		return nil, err
	}
	return pageRoots, nil
}

// writeStagedExtras emits the assets that ride along with the staged
// page tree (template CSS at the staged root, feeds, llms manifests).
// llms*.txt only land when the weblog has opted in — the static
// snapshot mirrors whatever the dynamic routes would serve.
func writeStagedExtras(staging string, site content.Site, tmpl *domain.Template, weblog *domain.Weblog, data *buildData, rep *Report) error {
	if tmpl.CSS != "" {
		// Render so {site_parts} / {site_encoding} land as actual values
		// instead of dead literals — mirrors the dynamic /style.css
		// handler.
		body := content.RenderTemplateCSS(site, tmpl)
		if err := writeFile(filepath.Join(staging, "style.css"), []byte(body)); err != nil {
			return fmt.Errorf("rebuild: write css: %w", err)
		}
		rep.CSSWritten = true
	}
	if err := writeFeeds(staging, site, data.all, data.cats, data.users, rep); err != nil {
		return err
	}
	if weblog.LLMSEnabled {
		if err := writeLLMsTxt(staging, *weblog, data.all, rep); err != nil {
			return err
		}
	}
	if err := writeStagedSEO(staging, weblog, data, rep); err != nil {
		return err
	}
	return nil
}

// writeStagedSEO emits sitemap.xml and robots.txt into staging when
// the corresponding toggles are on and base_url is configured.
func writeStagedSEO(staging string, weblog *domain.Weblog, data *buildData, rep *Report) error {
	if weblog.SitemapEnabled && weblog.BaseURL != "" {
		if err := writeStagedSitemap(staging, weblog, data); err != nil {
			return err
		}
		rep.SitemapWritten = true
	}
	if weblog.RobotsEnabled && weblog.BaseURL != "" {
		body := sitemap.RobotsTxt(weblog)
		if err := writeFile(filepath.Join(staging, "robots.txt"), []byte(body)); err != nil {
			return err
		}
		rep.RobotsWritten = true
	}
	return nil
}

func writeStagedSitemap(staging string, weblog *domain.Weblog, data *buildData) error {
	visibleCats := make([]domain.Category, 0, len(data.cats))
	for _, c := range data.cats {
		if !c.Hidden {
			visibleCats = append(visibleCats, c)
		}
	}
	filteredEntries := make([]domain.Entry, 0, len(data.all))
	for _, e := range data.all {
		if e.CategoryID == domain.Uncategorized {
			filteredEntries = append(filteredEntries, e)
			continue
		}
		if cat, ok := data.cats[e.CategoryID]; ok && !cat.Hidden {
			filteredEntries = append(filteredEntries, e)
		}
	}
	in := sitemap.Input{
		Weblog:           weblog,
		Entries:          filteredEntries,
		Categories:       visibleCats,
		Tags:             data.tags,
		Pages:            data.pages,
		CategoryLastMods: data.catLastMods,
		TagLastMods:      data.tagLastMods,
	}
	body, _, err := sitemap.BuildFromInput(in)
	if err != nil {
		return fmt.Errorf("rebuild: sitemap: %w", err)
	}
	return writeFile(filepath.Join(staging, "sitemap.xml"), body)
}

// writeAdditiveAssets emits the assets whose lifecycle sits outside the
// staging swap: per-template stylesheets and the image/template
// mirrors. These are additive copies — a partial copy is recoverable
// on the next rebuild — so they intentionally bypass the rename-on-
// success guarantee.
func writeAdditiveAssets(ctx context.Context, store *repo.Store, site content.Site, opts Options, finalOut string, rep *Report) error {
	if err := writeTemplateCSS(ctx, store, site, opts.WID, finalOut); err != nil {
		return err
	}
	if opts.ImageDir != "" {
		n, err := copyImageTree(opts.ImageDir, filepath.Join(finalOut, "img"))
		if err != nil {
			return fmt.Errorf("rebuild: copy images: %w", err)
		}
		rep.ImagesCopied = n
	}
	if opts.TemplateDir != "" {
		n, err := copyImageTree(opts.TemplateDir, filepath.Join(finalOut, "template"))
		if err != nil {
			return fmt.Errorf("rebuild: copy templates: %w", err)
		}
		rep.TemplateAssetsCopied = n
	}
	return nil
}

// copyImageTree walks src and mirrors every regular file into dst (creating
// directories as needed). Missing src is treated as "no images yet" and
// returns (0, nil) so a fresh install can rebuild without pre-creating the
// directory.
func copyImageTree(src, dst string) (int, error) {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("image dir %q is not a directory", src)
	}
	count := 0
	err = filepath.Walk(src, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

// resolveRefs pulls the category and user lookup maps for every entry in
// a single pair of queries.
func resolveRefs(ctx context.Context, store *repo.Store, entries []domain.Entry) (map[int64]domain.Category, map[int64]domain.User, error) {
	catIDs := make([]int64, 0, len(entries))
	userIDs := make([]int64, 0, len(entries))
	seenCat, seenUser := map[int64]struct{}{}, map[int64]struct{}{}
	for _, e := range entries {
		if _, ok := seenCat[e.CategoryID]; !ok {
			seenCat[e.CategoryID] = struct{}{}
			catIDs = append(catIDs, e.CategoryID)
		}
		if _, ok := seenUser[e.AuthorID]; !ok {
			seenUser[e.AuthorID] = struct{}{}
			userIDs = append(userIDs, e.AuthorID)
		}
	}
	cats, err := store.CategoriesByIDs(ctx, catIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("rebuild: resolve categories: %w", err)
	}
	users, err := store.UsersByIDs(ctx, userIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("rebuild: resolve users: %w", err)
	}
	return cats, users, nil
}

func writeHome(ctx context.Context, store *repo.Store, outDir string, site content.Site, tmpl *domain.Template, all []domain.Entry, cats map[int64]domain.Category, users map[int64]domain.User, profileUsers []domain.User, sidebar content.SidebarData, limit int) error {
	// Sort pinned-first so the static home page matches the dynamic route.
	head := pinnedFirst(all)
	if len(head) > limit {
		head = head[:limit]
	}
	if site.EntrySortAsc() {
		reverseEntries(head)
	}
	body, err := (content.ListView{
		Site: site, Template: tmpl, Entries: head, Categories: cats, Users: users,
		Tags:         tagsForEntries(ctx, store, head),
		ProfileUsers: profileUsers,
		Sidebar:      sidebar,
		Mode:         "page",
	}).Render()
	if err != nil {
		return fmt.Errorf("rebuild: render home: %w", err)
	}
	return writeFile(filepath.Join(outDir, "index.html"), []byte(body))
}

// loadSidebarData mirrors the public handler's sidebar-inputs
// loader — every block gates on HasBlock at render time, so a
// missing input slice is always safe to pass through.
func loadSidebarData(ctx context.Context, store *repo.Store, wid int64, loc *time.Location) (content.SidebarData, error) {
	var out content.SidebarData
	periods, err := store.ArchivePeriodsWithCounts(ctx, wid, loc)
	if err != nil {
		return out, fmt.Errorf("archives: %w", err)
	}
	out.Archives = periods
	cats, err := store.AllCategoriesWithPublishedEntryCounts(ctx, wid)
	if err != nil {
		return out, fmt.Errorf("categories: %w", err)
	}
	tree := make([]content.SidebarCategory, 0, len(cats))
	for _, c := range cats {
		tree = append(tree, content.SidebarCategory{Category: c.Category, Count: c.EntryCount})
	}
	out.CategoryTree = tree
	msgs, err := store.RecentApprovedMessages(ctx, wid, 5)
	if err != nil {
		return out, fmt.Errorf("recent comments: %w", err)
	}
	out.RecentComments = msgs
	latest, err := store.RecentPublishedEntries(ctx, wid, 5)
	if err != nil {
		return out, fmt.Errorf("latest entries: %w", err)
	}
	out.LatestEntries = latest
	return out, nil
}

// reverseEntries flips the slice in place for the "日付の古いものを
// 上に" setting. Mirror of the public handler's helper — rebuild
// can't just call the public one because that package isn't in this
// one's import graph.
func reverseEntries(es []domain.Entry) {
	for i, j := 0, len(es)-1; i < j; i, j = i+1, j-1 {
		es[i], es[j] = es[j], es[i]
	}
}

// pinnedFirst returns a copy of entries sorted by pinned DESC, posted_at DESC —
// the same stable order used by the dynamic home and category pages.
func pinnedFirst(entries []domain.Entry) []domain.Entry {
	out := make([]domain.Entry, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].PostedAt.After(out[j].PostedAt)
	})
	return out
}

// tagsForEntries fetches the per-entry tag map for a rendered slice.
// Errors are logged and collapsed to an empty map — partial tag data is
// better than a failed rebuild for every writer.
func tagsForEntries(ctx context.Context, store *repo.Store, entries []domain.Entry) map[int64][]domain.Tag {
	if len(entries) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	out, err := store.TagsByEntries(ctx, ids)
	if err != nil {
		log.Printf("rebuild: tagsForEntries: %v", err)
		return nil
	}
	return out
}

// buildVisibleIndex returns (1) a slice of entries whose category is not
// hidden, sorted by posted_at DESC, id DESC — matching the order Prev/
// NextPublishedEntry uses on the SQL side; and (2) an id-to-index map
// for O(1) neighbour lookup. Hidden-category entries are intentionally
// absent so an attempt to look one up yields ok=false, mirroring the
// current adjacentEntries behaviour where hidden entries report nil
// neighbours.
func buildVisibleIndex(all []domain.Entry, cats map[int64]domain.Category) ([]domain.Entry, map[int64]int) {
	visible := make([]domain.Entry, 0, len(all))
	for _, e := range all {
		if c, ok := cats[e.CategoryID]; ok && c.Hidden {
			continue
		}
		visible = append(visible, e)
	}
	sort.SliceStable(visible, func(i, j int) bool {
		if !visible[i].PostedAt.Equal(visible[j].PostedAt) {
			return visible[i].PostedAt.After(visible[j].PostedAt)
		}
		return visible[i].ID > visible[j].ID
	})
	idx := make(map[int64]int, len(visible))
	for i, e := range visible {
		idx[e.ID] = i
	}
	return visible, idx
}

// Template resolves a template by id, caching the result so multiple
// writers (entries, categories, pages) share the same lookup. A zero
// id returns nil without querying. Errors are logged and fall back to
// nil so the caller can use its own default.
func (bd *buildData) Template(ctx context.Context, store *repo.Store, wid, id int64) *domain.Template {
	if id == 0 {
		return nil
	}
	if t, ok := bd.templates[id]; ok {
		return t
	}
	t, err := store.TemplateByID(ctx, wid, id)
	if err != nil {
		log.Printf("rebuild: template %d missing, falling back: %v", id, err)
		return nil
	}
	bd.templates[id] = t
	return t
}

// adjacentFromIndex resolves prev/next from a pre-built visible index.
// Hidden-category entries are absent from the index, so the lookup
// naturally returns nil, nil for them — matching the SQL behaviour.
//
// visible is sorted by (posted_at DESC, id DESC), so for current index i:
//
//	prev (older)  = i+1
//	next (newer)  = i-1
func adjacentFromIndex(visible []domain.Entry, idx map[int64]int, e domain.Entry, cat *domain.Category) (*domain.Entry, *domain.Entry) {
	if cat != nil && cat.Hidden {
		return nil, nil
	}
	i, ok := idx[e.ID]
	if !ok {
		return nil, nil
	}
	var prev, next *domain.Entry
	if i+1 < len(visible) {
		p := visible[i+1]
		prev = &p
	}
	if i > 0 {
		n := visible[i-1]
		next = &n
	}
	return prev, next
}

func writeEntries(ctx context.Context, store *repo.Store, opts Options, site content.Site, tmpl *domain.Template, weblog *domain.Weblog, data *buildData, rep *Report) error {
	// Entry template priority mirrors SB3:
	// entry's main category template -> active template.
	tagMap := tagsForEntries(ctx, store, data.all)
	visible, visIdx := buildVisibleIndex(data.all, data.cats)

	entryIDs := make([]int64, 0, len(data.all))
	for _, e := range data.all {
		entryIDs = append(entryIDs, e.ID)
	}
	msgMap, err := store.ApprovedMessagesByEntries(ctx, opts.WID, entryIDs)
	if err != nil {
		return fmt.Errorf("rebuild: bulk comments: %w", err)
	}

	for i := range data.all {
		e := data.all[i]
		var catPtr *domain.Category
		if c, ok := data.cats[e.CategoryID]; ok {
			catPtr = &c
		}
		var authorPtr *domain.User
		if u, ok := data.users[e.AuthorID]; ok {
			authorPtr = &u
		}

		prev, next := adjacentFromIndex(visible, visIdx, e, catPtr)

		msgs := msgMap[e.ID]
		if weblog.CommentSortOrder == "desc" {
			for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
				msgs[i], msgs[j] = msgs[j], msgs[i]
			}
		}

		entryTags := tagMap[e.ID]

		entryTmpl := tmpl
		if catPtr != nil && catPtr.TemplateID != 0 {
			if t := data.Template(ctx, store, opts.WID, catPtr.TemplateID); t != nil {
				entryTmpl = t
			}
		}

		body, err := (content.EntryView{
			Site: site, Template: entryTmpl, Entry: e,
			Category: catPtr, Author: authorPtr, Prev: prev, Next: next,
			Messages:     msgs,
			CommentMode:  weblog.CommentMode,
			Tags:         entryTags,
			ProfileUsers: data.profileUsers,
			Sidebar:      data.sidebar,
		}).Render()
		if err != nil {
			return fmt.Errorf("rebuild: render entry %d: %w", e.ID, err)
		}
		// Static path tracks the canonical permalink: slug when the entry
		// carries one, numeric id otherwise. Going through
		// Site.EntryStaticPath keeps the rebuild + dynamic router using
		// the same key logic.
		path := filepath.Join(opts.OutDir, filepath.FromSlash(site.EntryStaticPath(e)), "index.html")
		if err := writeFile(path, []byte(body)); err != nil {
			return err
		}
		rep.Entries++
	}
	return nil
}

func writeCategories(ctx context.Context, store *repo.Store, opts Options, site content.Site, tmpl *domain.Template, data *buildData, rep *Report) error {
	allCats, err := store.AllCategories(ctx, opts.WID)
	if err != nil {
		return fmt.Errorf("rebuild: list categories: %w", err)
	}
	for _, c := range allCats {
		cat := c // loop var escapes into pointer below
		// Skip hidden categories: the dynamic /category/<key>/ route
		// keeps responding for direct hits, but the static snapshot is
		// intentionally absent so an operator who flipped a category
		// hidden doesn't keep a stale public-facing archive page on the
		// CDN. promoteManagedSubtree's swap removes any previous
		// snapshot in the same run.
		if cat.Hidden {
			continue
		}
		entries, err := store.PublishedEntriesByCategoryPage(ctx, opts.WID, cat.ID, opts.EntryListLimit, 0)
		if err != nil {
			return fmt.Errorf("rebuild: category %d entries: %w", cat.ID, err)
		}
		if site.EntrySortAsc() {
			reverseEntries(entries)
		}
		// Per-category template pin beats the archive-template argument
		// passed in. Falls back to the caller's tmpl on miss so a stale
		// pin doesn't break the snapshot.
		pageTmpl := tmpl
		if cat.TemplateID != 0 {
			if t := data.Template(ctx, store, opts.WID, cat.TemplateID); t != nil {
				pageTmpl = t
			}
		}
		body, err := (content.ListView{
			Site: site, Template: pageTmpl, Entries: entries, Categories: data.cats, Users: data.users,
			Tags:         tagsForEntries(ctx, store, entries),
			Category:     &cat,
			ProfileUsers: data.profileUsers,
			Sidebar:      data.sidebar,
			PageTitle:    "Category: " + cat.Name,
			Mode:         "cat",
			ModeContext:  strconv.FormatInt(cat.ID, 10),
		}).Render()
		if err != nil {
			return fmt.Errorf("rebuild: render category %d: %w", cat.ID, err)
		}
		// Site.CategoryStaticPath keeps the rebuild + dynamic router using
		// the same key choice (slug when set, numeric id otherwise) so a
		// snapshot file lands at the same URL the live handler would serve.
		path := filepath.Join(opts.OutDir, filepath.FromSlash(site.CategoryStaticPath(c)), "index.html")
		if err := writeFile(path, []byte(body)); err != nil {
			return err
		}
		rep.Categories++
	}
	return nil
}

func writeTags(ctx context.Context, store *repo.Store, opts Options, site content.Site, tmpl *domain.Template, cats map[int64]domain.Category, users map[int64]domain.User, profileUsers []domain.User, sidebar content.SidebarData, rep *Report) error {
	allTags, err := store.AllTags(ctx, opts.WID)
	if err != nil {
		return fmt.Errorf("rebuild: list tags: %w", err)
	}
	for _, t := range allTags {
		entries, err := store.PublishedEntriesByTag(ctx, opts.WID, t.ID, opts.EntryListLimit)
		if err != nil {
			return fmt.Errorf("rebuild: tag %d entries: %w", t.ID, err)
		}
		if site.EntrySortAsc() {
			reverseEntries(entries)
		}
		body, err := (content.ListView{
			Site: site, Template: tmpl, Entries: entries, Categories: cats, Users: users,
			Tags:         tagsForEntries(ctx, store, entries),
			ProfileUsers: profileUsers,
			Sidebar:      sidebar,
			PageTitle:    "Tag: " + t.Name,
			Mode:         "tag",
			ModeContext:  t.Slug,
		}).Render()
		if err != nil {
			return fmt.Errorf("rebuild: render tag %d: %w", t.ID, err)
		}
		path := filepath.Join(opts.OutDir, "tag", t.Slug, "index.html")
		if err := writeFile(path, []byte(body)); err != nil {
			return err
		}
		rep.Tags++
	}
	return nil
}

func writeArchives(ctx context.Context, store *repo.Store, opts Options, site content.Site, tmpl *domain.Template, cats map[int64]domain.Category, users map[int64]domain.User, profileUsers []domain.User, sidebar content.SidebarData, rep *Report) error {
	periods, err := store.ArchivePeriods(ctx, opts.WID, opts.TZ)
	if err != nil {
		return fmt.Errorf("rebuild: archive periods: %w", err)
	}

	yearSeen := map[int]struct{}{}
	for _, p := range periods {
		// Year index — write once per year on first occurrence.
		if _, done := yearSeen[p.Year]; !done {
			yearSeen[p.Year] = struct{}{}
			from := time.Date(p.Year, time.January, 1, 0, 0, 0, 0, opts.TZ)
			to := from.AddDate(1, 0, 0)
			entries, err := store.PublishedEntriesInRange(ctx, opts.WID, from, to, opts.EntryListLimit)
			if err != nil {
				return fmt.Errorf("rebuild: archive %d entries: %w", p.Year, err)
			}
			if site.EntrySortAsc() {
				reverseEntries(entries)
			}
			body, err := (content.ListView{
				Site: site, Template: tmpl, Entries: entries, Categories: cats, Users: users,
				Tags:         tagsForEntries(ctx, store, entries),
				ProfileUsers: profileUsers,
				Sidebar:      sidebar,
				PageTitle:    "Archive: " + strconv.Itoa(p.Year),
				Mode:         "arc",
				ModeContext:  strconv.Itoa(p.Year),
			}).Render()
			if err != nil {
				return fmt.Errorf("rebuild: render archive %d: %w", p.Year, err)
			}
			path := filepath.Join(opts.OutDir, "archive", strconv.Itoa(p.Year), "index.html")
			if err := writeFile(path, []byte(body)); err != nil {
				return err
			}
			rep.ArchiveYear++
		}

		// Month index.
		from := time.Date(p.Year, time.Month(p.Month), 1, 0, 0, 0, 0, opts.TZ)
		to := from.AddDate(0, 1, 0)
		entries, err := store.PublishedEntriesInRange(ctx, opts.WID, from, to, opts.EntryListLimit)
		if err != nil {
			return fmt.Errorf("rebuild: archive %d/%02d entries: %w", p.Year, p.Month, err)
		}
		if site.EntrySortAsc() {
			reverseEntries(entries)
		}
		body, err := (content.ListView{
			Site: site, Template: tmpl, Entries: entries, Categories: cats, Users: users,
			Tags:         tagsForEntries(ctx, store, entries),
			ProfileUsers: profileUsers,
			Sidebar:      sidebar,
			PageTitle:    "Archive: " + strconv.Itoa(p.Year) + "/" + padMonth(p.Month),
			Mode:         "arc",
			ModeContext:  fmt.Sprintf("%04d%s", p.Year, padMonth(p.Month)),
		}).Render()
		if err != nil {
			return fmt.Errorf("rebuild: render archive %d/%02d: %w", p.Year, p.Month, err)
		}
		path := filepath.Join(opts.OutDir, "archive", strconv.Itoa(p.Year), padMonth(p.Month), "index.html")
		if err := writeFile(path, []byte(body)); err != nil {
			return err
		}
		rep.ArchiveMonth++
	}
	return nil
}

// writeTemplateCSS mirrors every template's CSS column to
// <out>/template/<id>/style.css so static deployments can serve the
// per-template stylesheet URLs emitted by {site_css}. Templates with
// an empty CSS column are skipped to avoid writing empty files.
func writeTemplateCSS(ctx context.Context, store *repo.Store, site content.Site, wid int64, outDir string) error {
	templates, err := store.ListTemplatesForAdmin(ctx, wid)
	if err != nil {
		return fmt.Errorf("rebuild: list templates for css: %w", err)
	}
	for _, t := range templates {
		if t.CSS == "" {
			continue
		}
		path := filepath.Join(outDir, "template", strconv.FormatInt(t.ID, 10), "style.css")
		body := content.RenderTemplateCSS(site, &t)
		if err := writeFile(path, []byte(body)); err != nil {
			return fmt.Errorf("rebuild: write template %d css: %w", t.ID, err)
		}
	}
	return nil
}

// pagesManifestName is the hidden file that records the set of flat-page
// root directories managed by the previous rebuild. It lives in both
// staging and finalOut so promoteExtraDirs can prune stale directories
// without touching operator-managed top-level directories.
const pagesManifestName = ".sb-pages-manifest"

// writePagesManifest writes one root directory per line, sorted, so the
// file is deterministic across rebuilds.
func writePagesManifest(outDir string, roots map[string]struct{}) error {
	if len(roots) == 0 {
		// Write an empty file so a previous manifest is overwritten.
		return writeFile(filepath.Join(outDir, pagesManifestName), []byte{})
	}
	lines := make([]string, 0, len(roots))
	for r := range roots {
		lines = append(lines, r)
	}
	sort.Strings(lines)
	return writeFile(filepath.Join(outDir, pagesManifestName), []byte(strings.Join(lines, "\n")+"\n"))
}

// readPagesManifest reads the manifest at path and returns the set of
// roots it contains. If the file does not exist, an empty set is returned.
func readPagesManifest(path string) (map[string]struct{}, error) {
	set := make(map[string]struct{})
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return set, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			set[line] = struct{}{}
		}
	}
	return set, nil
}

// writeFeeds emits rss.xml + atom.xml alongside the HTML snapshot. A
// feed write failure is fatal for the same reason a home-page failure is
// — partial snapshots are worse than no rebuild. Feed content is capped
// inside the builder, so handing it the full entry list is safe.
func writePages(ctx context.Context, store *repo.Store, opts Options, site content.Site, data *buildData, rep *Report) (map[string]struct{}, error) {
	pages, err := store.PublishedPages(ctx, opts.WID)
	if err != nil {
		return nil, fmt.Errorf("rebuild: list pages: %w", err)
	}
	roots := make(map[string]struct{}, len(pages))
	for _, p := range pages {
		var pageTmpl *domain.Template
		if p.TemplateID != 0 {
			pageTmpl = data.Template(ctx, store, opts.WID, p.TemplateID)
		}
		if pageTmpl == nil {
			pageTmpl, err = store.ActiveTemplate(ctx, opts.WID)
			if err != nil {
				return nil, fmt.Errorf("rebuild: load active template for page %d: %w", p.ID, err)
			}
		}
		body, err := (content.PageView{
			Site:         site,
			Template:     pageTmpl,
			Page:         p,
			ProfileUsers: data.profileUsers,
			Sidebar:      data.sidebar,
		}).Render()
		if err != nil {
			return nil, fmt.Errorf("rebuild: render page %d: %w", p.ID, err)
		}
		// slug is "/about" → "about/index.html"
		root := filepath.FromSlash(p.Slug[1:])
		path := filepath.Join(opts.OutDir, root, "index.html")
		if err := writeFile(path, []byte(body)); err != nil {
			return nil, err
		}
		rep.Pages++
		roots[root] = struct{}{}
	}
	if err := writePagesManifest(opts.OutDir, roots); err != nil {
		return nil, fmt.Errorf("rebuild: write pages manifest: %w", err)
	}
	return roots, nil
}

func writeFeeds(outDir string, site content.Site, all []domain.Entry, cats map[int64]domain.Category, users map[int64]domain.User, rep *Report) error {
	opts := feed.Options{
		Site: site, Entries: all, Users: users, Categories: cats,
	}
	rss, err := feed.BuildRSS(opts)
	if err != nil {
		return fmt.Errorf("rebuild: build rss: %w", err)
	}
	if err := writeFile(filepath.Join(outDir, "rss.xml"), rss); err != nil {
		return err
	}
	rep.RSSWritten = true
	atom, err := feed.BuildAtom(opts)
	if err != nil {
		return fmt.Errorf("rebuild: build atom: %w", err)
	}
	if err := writeFile(filepath.Join(outDir, "atom.xml"), atom); err != nil {
		return err
	}
	rep.AtomWritten = true
	return nil
}

// writeLLMsTxt emits /llms.txt + /llms-full.txt when the weblog has
// opted in. Mirrors the dynamic route content so agents that rely on
// the static snapshot see the exact same output as live callers.
func writeLLMsTxt(outDir string, weblog domain.Weblog, all []domain.Entry, rep *Report) error {
	in := llmstxt.Input{Weblog: weblog, Entries: all}
	if err := writeFile(filepath.Join(outDir, "llms.txt"), []byte(llmstxt.Index(in))); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(outDir, "llms-full.txt"), []byte(llmstxt.Full(in))); err != nil {
		return err
	}
	rep.LLMSWritten = true
	return nil
}

// promoteStaging atomically (per-subtree) replaces the live page
// output under finalOut with the freshly-rendered tree under staging.
//
// The flow for each managed subtree (entry/, category/, tag/,
// archive/):
//
//  1. If staging has it, rename the existing finalOut/<sub> aside
//     into a random-named backup directory under finalOut (same-FS
//     rename is atomic and reversible), rename staging/<sub> into
//     finalOut/<sub>, then remove the backup.
//  2. If staging does NOT have it (e.g. the DB has zero entries
//     this run), remove finalOut/<sub> so deleted-everything is
//     tracked.
//
// On rename failure we restore the backup so the live snapshot keeps
// the previous content. Once subtree promotion is done, the top-level
// files (index.html, style.css, rss.xml, atom.xml, llms*.txt) are
// renamed file-by-file — file rename overwrites are atomic on POSIX,
// so each file flips in place. Finally, when the LLMS toggle is off
// any leftover llms*.txt are removed so flipping the switch off
// cleans up after itself.
func promoteStaging(finalOut, staging string, llmsEnabled bool, weblog *domain.Weblog, pruneSet map[string]struct{}) error {
	// Load the previous build's page-root manifest so we can prune
	// stale directories that were generated in an earlier run but no
	// longer exist (deleted, unpublished, or renamed pages).
	oldRoots, err := readPagesManifest(filepath.Join(finalOut, pagesManifestName))
	if err != nil {
		return fmt.Errorf("rebuild: read old pages manifest: %w", err)
	}

	for _, sub := range []string{"entry", "category", "tag", "archive"} {
		if err := promoteManagedSubtree(staging, finalOut, sub); err != nil {
			return err
		}
	}

	if err := promoteTopLevelFiles(staging, finalOut); err != nil {
		return err
	}

	// Flat pages land as top-level directories (e.g. "about/"). Promote
	// every directory that isn't one of the known managed subtrees or
	// the asset mirrors.
	if err := promoteExtraDirs(staging, finalOut, pruneSet, oldRoots); err != nil {
		return err
	}

	if err := promotePagesManifest(staging, finalOut); err != nil {
		return err
	}

	if err := cleanupLLMs(finalOut, llmsEnabled); err != nil {
		return err
	}
	return cleanupSEOFiles(finalOut, weblog.SitemapEnabled, weblog.RobotsEnabled, weblog.BaseURL)
}

// promoteManagedSubtree swaps the staged copy of a known subtree
// (entry, category, tag, archive) into finalOut via the same
// backup-and-rename pattern used for flat pages. A missing staged
// subtree means "the DB has no rows of this type" — the live copy is
// deleted so removals propagate to the static snapshot.
func promoteManagedSubtree(staging, finalOut, sub string) error {
	stagedPath := filepath.Join(staging, sub)
	finalPath := filepath.Join(finalOut, sub)

	stagedExists := dirExists(stagedPath)
	finalExists := dirExists(finalPath)

	if !stagedExists {
		if finalExists {
			if err := os.RemoveAll(finalPath); err != nil {
				return fmt.Errorf("rebuild: prune %s: %w", finalPath, err)
			}
		}
		return nil
	}

	var backupDir string
	if finalExists {
		bd, err := os.MkdirTemp(finalOut, ".sb-backup-")
		if err != nil {
			return fmt.Errorf("rebuild: create backup dir: %w", err)
		}
		backupDir = bd
		backupPath := filepath.Join(bd, "dir")
		if err := os.Rename(finalPath, backupPath); err != nil {
			_ = os.RemoveAll(bd)
			return fmt.Errorf("rebuild: backup %s: %w", finalPath, err)
		}
	}
	if err := os.Rename(stagedPath, finalPath); err != nil {
		// Restore previous output so the live snapshot is not left
		// partially overwritten.
		if finalExists {
			_ = os.Rename(filepath.Join(backupDir, "dir"), finalPath)
		}
		return fmt.Errorf("rebuild: promote %s: %w", finalPath, err)
	}
	if finalExists {
		_ = os.RemoveAll(backupDir)
	}
	return nil
}

// promoteTopLevelFiles renames the well-known top-level files
// (index.html, style.css, the two feeds, both llms manifests) one at a
// time. Single-file rename is atomic on POSIX, so each file flips in
// place without needing a backup step. Missing staged files are
// expected (e.g. llms*.txt when the toggle is off) and skipped.
func promoteTopLevelFiles(staging, finalOut string) error {
	for _, name := range []string{"index.html", "style.css", "rss.xml", "atom.xml", "llms.txt", "llms-full.txt", "sitemap.xml", "robots.txt"} {
		stagedPath := filepath.Join(staging, name)
		finalPath := filepath.Join(finalOut, name)
		if _, err := os.Stat(stagedPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("rebuild: stat staged %s: %w", name, err)
		}
		if err := os.Rename(stagedPath, finalPath); err != nil {
			return fmt.Errorf("rebuild: promote %s: %w", finalPath, err)
		}
	}
	return nil
}

// promotePagesManifest carries the new flat-page manifest into finalOut
// so the next rebuild knows which directories it managed. A missing
// staged manifest is not fatal — older rebuild paths or test fixtures
// may not emit one.
func promotePagesManifest(staging, finalOut string) error {
	stagedManifest := filepath.Join(staging, pagesManifestName)
	finalManifest := filepath.Join(finalOut, pagesManifestName)
	if _, err := os.Stat(stagedManifest); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("rebuild: stat pages manifest: %w", err)
	}
	if err := os.Rename(stagedManifest, finalManifest); err != nil {
		return fmt.Errorf("rebuild: promote pages manifest: %w", err)
	}
	return nil
}

// cleanupLLMs deletes the llms*.txt files when the toggle has been
// turned off so flipping the switch off propagates to the static
// snapshot. Missing files are fine — they may never have existed.
func cleanupLLMs(finalOut string, llmsEnabled bool) error {
	if llmsEnabled {
		return nil
	}
	for _, name := range []string{"llms.txt", "llms-full.txt"} {
		if err := os.Remove(filepath.Join(finalOut, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rebuild: remove %s: %w", name, err)
		}
	}
	return nil
}

// cleanupSEOFiles removes sitemap.xml / robots.txt when the corresponding
// toggle has been turned off or base_url is empty so the static snapshot
// stays consistent with the dynamic route state.
func cleanupSEOFiles(finalOut string, sitemapEnabled, robotsEnabled bool, baseURL string) error {
	if !sitemapEnabled || baseURL == "" {
		if err := os.Remove(filepath.Join(finalOut, "sitemap.xml")); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rebuild: remove sitemap.xml: %w", err)
		}
	}
	if !robotsEnabled || baseURL == "" {
		if err := os.Remove(filepath.Join(finalOut, "robots.txt")); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rebuild: remove robots.txt: %w", err)
		}
	}
	return nil
}

// promoteExtraDirs promotes each flat-page root from staging into
// finalOut at the full-root granularity recorded in pruneSet.  This
// means a page at /service/pricing replaces *only*
// finalOut/service/pricing, leaving sibling directories such as
// service/downloads untouched.
//
// Nested roots (e.g. /service and /service/pricing) are handled safely
// by promoting deepest roots first and preserving active children that
// already exist in finalOut across the parent rename.
//
// After promotion, directories that were tracked in oldRoots but are
// no longer in pruneSet are removed so deleted / unpublished / renamed
// flat pages are cleaned up.  A stale parent is skipped when an active
// descendant still lives inside it, preventing accidental deletion of
// active children.  Operator-managed directories that were never
// tracked in oldRoots are left untouched.
func promoteExtraDirs(staging, finalOut string, pruneSet, oldRoots map[string]struct{}) error {
	// Promote deepest roots first so child directories are moved out of
	// their parents in staging before the parent is promoted.
	rootsByDepth := make([]string, 0, len(pruneSet))
	for r := range pruneSet {
		rootsByDepth = append(rootsByDepth, r)
	}
	sort.Slice(rootsByDepth, func(i, j int) bool {
		return depth(rootsByDepth[i]) > depth(rootsByDepth[j])
	})

	for _, root := range rootsByDepth {
		if err := promoteOneExtraDir(staging, finalOut, root, pruneSet); err != nil {
			return err
		}
	}

	return pruneStaleRoots(finalOut, pruneSet, oldRoots)
}

// promoteOneExtraDir promotes a single flat-page root from staging to
// finalOut, preserving any already-promoted active children that live
// under it (since promoteExtraDirs walks deepest-first, child roots
// may already exist in finalPath when we get here).
func promoteOneExtraDir(staging, finalOut, root string, pruneSet map[string]struct{}) error {
	stagedPath := filepath.Join(staging, filepath.FromSlash(root))
	finalPath := filepath.Join(finalOut, filepath.FromSlash(root))

	// Ensure the parent directory exists in finalOut so the rename
	// below has a target. MkdirAll is a no-op when the directory
	// already exists (e.g. operator-managed dirs).
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return fmt.Errorf("rebuild: mkdir parent %s: %w", finalPath, err)
	}

	// Identify active children that already live in finalPath (they
	// were promoted in earlier iterations because we sort deepest
	// first). We must preserve them across the parent rename.
	childrenToPreserve := activeChildrenOf(root, finalPath, pruneSet)

	var backupDir string
	finalExists := dirExists(finalPath)
	if finalExists {
		bd, err := os.MkdirTemp(finalOut, ".sb-backup-")
		if err != nil {
			return fmt.Errorf("rebuild: create backup dir: %w", err)
		}
		backupDir = bd
		backupPath := filepath.Join(bd, "dir")
		if err := os.Rename(finalPath, backupPath); err != nil {
			_ = os.RemoveAll(bd)
			return fmt.Errorf("rebuild: backup %s: %w", finalPath, err)
		}
	}
	if err := os.Rename(stagedPath, finalPath); err != nil {
		if finalExists {
			_ = os.Rename(filepath.Join(backupDir, "dir"), finalPath)
		}
		return fmt.Errorf("rebuild: promote %s: %w", finalPath, err)
	}

	if err := restorePreservedChildren(backupDir, finalPath, root, childrenToPreserve); err != nil {
		return err
	}

	if finalExists {
		_ = os.RemoveAll(backupDir)
	}
	return nil
}

// activeChildrenOf returns the subset of pruneSet rooted under `root`
// whose finalPath equivalents are already on disk — i.e. children that
// were promoted in earlier deepest-first iterations and must survive
// the parent's rename.
func activeChildrenOf(root, finalPath string, pruneSet map[string]struct{}) []string {
	var out []string
	for child := range pruneSet {
		if child == root {
			continue
		}
		if !strings.HasPrefix(child, root+"/") {
			continue
		}
		childRel := child[len(root)+1:]
		if dirExists(filepath.Join(finalPath, filepath.FromSlash(childRel))) {
			out = append(out, child)
		}
	}
	return out
}

// restorePreservedChildren moves the preserved child dirs back into the
// freshly-promoted parent. The backup dir is laid out as
// <backupDir>/dir/<childRel>, mirroring what was previously at finalPath.
func restorePreservedChildren(backupDir, finalPath, root string, children []string) error {
	for _, child := range children {
		childRel := child[len(root)+1:]
		backupChild := filepath.Join(filepath.Join(backupDir, "dir"), filepath.FromSlash(childRel))
		finalChild := filepath.Join(finalPath, filepath.FromSlash(childRel))
		if !dirExists(backupChild) {
			continue
		}
		// Ensure the intermediate directories exist in the
		// freshly-promoted parent.
		if err := os.MkdirAll(filepath.Dir(finalChild), 0o755); err != nil {
			return fmt.Errorf("rebuild: mkdir child parent %s: %w", finalChild, err)
		}
		// Remove any empty placeholder that the parent staging dir
		// may have left behind.
		_ = os.RemoveAll(finalChild)
		if err := os.Rename(backupChild, finalChild); err != nil {
			return fmt.Errorf("rebuild: restore child %s: %w", finalChild, err)
		}
	}
	return nil
}

// pruneStaleRoots removes flat-page directories that were tracked in the
// previous manifest (`oldRoots`) but are no longer active (`pruneSet`).
// A stale root with an active descendant only loses its index.html so
// the descendant survives; operator-managed siblings on disk that were
// never tracked are left untouched.
func pruneStaleRoots(finalOut string, pruneSet, oldRoots map[string]struct{}) error {
	for root := range oldRoots {
		if _, stillActive := pruneSet[root]; stillActive {
			continue
		}
		stalePath := filepath.Join(finalOut, filepath.FromSlash(root))
		if !dirExists(stalePath) {
			continue
		}
		if hasActiveDescendant(root, pruneSet) {
			staleIndex := filepath.Join(stalePath, "index.html")
			if err := os.Remove(staleIndex); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("rebuild: prune stale index %s: %w", staleIndex, err)
			}
			continue
		}
		if err := os.RemoveAll(stalePath); err != nil {
			return fmt.Errorf("rebuild: prune %s: %w", stalePath, err)
		}
	}
	return nil
}

// hasActiveDescendant reports whether any element of pruneSet sits
// strictly under `root` on the URL path.
func hasActiveDescendant(root string, pruneSet map[string]struct{}) bool {
	for active := range pruneSet {
		if strings.HasPrefix(active, root+"/") {
			return true
		}
	}
	return false
}

// depth counts path segments.
func depth(p string) int {
	if p == "" {
		return 0
	}
	return strings.Count(p, "/") + 1
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("rebuild: mkdir %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("rebuild: write %s: %w", path, err)
	}
	return nil
}

func padMonth(m int) string {
	if m < 10 {
		return "0" + strconv.Itoa(m)
	}
	return strconv.Itoa(m)
}
