// Package domain holds the plain Go types that represent core SB entities
// the way handlers and the content layer want to use them — no SQL, no HTTP.
package domain

import (
	"database/sql"
	"regexp"
	"time"
)

// slugPattern matches valid entry slug values: lowercase alphanumerics
// joined by single hyphens, 1-100 chars. Kept intentionally narrow so
// the slug survives in URLs without percent-encoding.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// IsValidSlug reports whether s is an acceptable entry slug. Empty is
// reported as invalid — callers treating "no slug" as a distinct state
// must skip this check for that case.
func IsValidSlug(s string) bool {
	if len(s) == 0 || len(s) > 100 {
		return false
	}
	return slugPattern.MatchString(s)
}

// CommentMode is a per-weblog policy controlling whether the public comment
// form accepts submissions and whether those submissions are published
// immediately.
type CommentMode string

const (
	// CommentOpen — comments go live as soon as they pass spam checks.
	CommentOpen CommentMode = "open"
	// CommentModerated — comments are saved as waiting and require admin
	// approval before appearing publicly. Default for new weblogs.
	CommentModerated CommentMode = "moderated"
	// CommentClosed — the public form is not rendered and POSTs are rejected.
	CommentClosed CommentMode = "closed"
)

func (m CommentMode) Valid() bool {
	switch m {
	case CommentOpen, CommentModerated, CommentClosed:
		return true
	}
	return false
}

type Weblog struct {
	ID          int64
	Title       string
	Description string
	BaseURL     string
	Lang        string
	CommentMode CommentMode
	// SpamWords is the newline-separated banned-words list used to silently
	// drop obviously spammy comments. Kept as raw text so the admin UI (when
	// it lands) can round-trip user input without losing whitespace.
	SpamWords string
	// IPBlacklist is a newline-separated list of IP addresses or CIDR ranges
	// that are silently dropped before a comment is persisted.
	// Each line is either a bare address ("198.51.100.5" / "2001:db8::1")
	// or a CIDR block ("198.51.100.0/24"). Stored as raw text so the admin
	// UI round-trips comments / blank lines without losing them.
	IPBlacklist string
	// OGBGImagePath is the stored_path of the uploaded image used as
	// the background of every entry's Open Graph card. Empty means
	// "fall back to the embedded SB default". Entry rows
	// carry their own override field so per-article branding wins over
	// the site-wide default when set.
	OGBGImagePath string
	// OGTextColor is the single color applied to both the entry title
	// and site-name strings on the OG card. Hex literal ("#RRGGBB" for
	// opaque, "#RRGGBBAA" for alpha — set "#00000000" to hide text
	// entirely over a photo background). Empty = use the built-in
	// two-tone default so cards authored before this field existed
	// keep their original look.
	OGTextColor string
	// LLMSEnabled flips the `/llms.txt` + `/llms-full.txt` public routes
	// on or off. Off by default — the admin opts in explicitly
	// so self-hosted writers who'd rather not feed AI crawlers stay out
	// of the loop, while authors who want agent discoverability get a
	// core-level feature no mainstream blog engine ships today.
	LLMSEnabled bool
	// AutoRebuildOnPublish triggers `rebuild.Build` automatically after
	// the admin saves an entry (create/update/delete) or a category
	// mutation. Off by default — enabling it requires SB_REBUILD_OUT to
	// be writable. Errors are logged and surfaced as a flash; the save
	// itself never rolls back on a failed rebuild.
	AutoRebuildOnPublish bool
	// SitemapEnabled controls whether /sitemap.xml is served and
	// generated during static rebuild. On by default.
	SitemapEnabled bool
	// RobotsEnabled controls whether /robots.txt is served and
	// generated during static rebuild. On by default.
	RobotsEnabled bool
	// ArchiveTemplateID pins a specific template for archive (year/month/
	// category) pages. 0 means "use the active template". ProfileTemplateID
	// plays the same role for the profile view.
	ArchiveTemplateID int64
	ProfileTemplateID int64
	// Date format strings for each template-tag context, expanded by
	// internal/dateformat. Empty means "use the package's Default*
	// constant" — a fresh install renders with sane ISO-ish defaults
	// until the author visits the design-settings screen.
	DateFormatEntry   string
	TimeFormatEntry   string
	DateFormatComment string
	DateFormatList    string
	DateFormatArchive string
	// Display-count + sort preferences surfaced on the design-settings
	// screen.
	//   EntriesPerPage: home/category/archive/tag list page size.
	//   EntrySortOrder: "desc" (newest first, SB3 default) or "asc".
	//   CommentSortOrder: "asc" (oldest first, SB3 default) or "desc".
	EntriesPerPage   int
	EntrySortOrder   string
	CommentSortOrder string
}

// User roles. SB3 had 管理 / 上級 / 一般 (admin / power / regular); we
// preserve the same three tiers so admin UI permissions match the
// user's mental model from SB3.
//
//	RoleAdmin: full access including the /admin/users management UI.
//	           Also the only role allowed to create / delete users.
//	RolePower: all menus except user management.
//	RoleRegular: entry + upload + own-profile only — no category,
//	             tag, template edit, or design settings access.
const (
	RoleAdmin   = 1
	RolePower   = 2
	RoleRegular = 3
)

type User struct {
	ID          int64
	WID         int64
	Name        string
	DisplayName string
	Email       string
	Role        int
	// Description is the profile bio rendered through {profile_description}.
	// HTML / Markdown both accepted; the renderer picks the right engine
	// based on DescriptionFormat.
	Description string
	// DescriptionFormat controls how Description is rendered at
	// template time. Same identifiers as entries.format ("html" /
	// "markdown"); empty falls back to "html".
	DescriptionFormat string
	// ListVisible controls whether the user appears in the public
	// {profile_area} block. Authors turn this off for utility accounts
	// that shouldn't be surfaced as an entry author.
	ListVisible bool
	// SortOrder drives the admin user-list ordering + the public
	// profile loop. Editable via drag-and-drop on /admin/users.
	SortOrder int
	// AIKind is the per-user AI provider selector —
	// "openai-compat" / "claude" / "" (disabled). Kept as a string
	// here so the domain package doesn't depend on internal/ai.
	AIKind string
	// AIBaseURL is the provider endpoint (e.g. LM Studio local URL).
	// Only meaningful when AIKind == "openai-compat".
	AIBaseURL string
	// AIModel is the provider-specific model id.
	AIModel string
	// AIAPIKeyEnc holds the encrypted API key ciphertext (AES-GCM via
	// SB_AI_SECRET). Decrypted only at call time by internal/ai —
	// handlers never see the plaintext.
	AIAPIKeyEnc string
	// AIAutoAlt toggles whether uploaded images get an alt-text
	// suggestion generated automatically. Defaults to true; users who
	// don't want the API hit can turn it off on /admin/profile.
	AIAutoAlt bool
	// AITimeoutSeconds overrides the per-feature default request
	// timeout when > 0. Lets local-LM users running slow CPU
	// inference (qwen3-thinking, gemma 27B+) raise the ceiling
	// without code changes; cloud users can leave it at 0 (default).
	// Bounded at the form layer (1..600).
	AITimeoutSeconds int
}

// IsAdmin reports whether this user is the site-administrator tier
// — a thin wrapper around the Role constant so handler gate checks
// read naturally.
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// CanManageUsers is the capability check the /admin/users routes
// consult. Currently equivalent to IsAdmin, but kept as a named
// predicate so future roles (e.g. delegated moderators) can widen
// the check without sweeping the handler code.
func (u User) CanManageUsers() bool { return u.Role == RoleAdmin }

// CanManageDesign reports whether this user may touch categories /
// tags / templates / design settings. Regular users can't.
func (u User) CanManageDesign() bool {
	return u.Role == RoleAdmin || u.Role == RolePower
}

// CanDeleteComment gates the message-delete button. Regular-tier
// authors can't remove reader-submitted comments — that's a
// site-operator call, not an author one. Power + Admin pass.
func (u User) CanDeleteComment() bool {
	return u.Role == RoleAdmin || u.Role == RolePower
}

// CanDeleteEntry returns true when the user may remove the given
// entry. Admins and power users may delete anyone's; regular
// authors may only delete entries they wrote themselves.
func (u User) CanDeleteEntry(authorID int64) bool {
	if u.Role == RoleAdmin || u.Role == RolePower {
		return true
	}
	return u.Role == RoleRegular && u.ID == authorID
}

// CanEditEntry follows the same rule as CanDeleteEntry: regular
// authors may only update their own entries; power and admin may
// edit any.
func (u User) CanEditEntry(authorID int64) bool {
	if u.Role == RoleAdmin || u.Role == RolePower {
		return true
	}
	return u.Role == RoleRegular && u.ID == authorID
}

// CanDeleteImage mirrors CanDeleteEntry: admin + power can remove
// any uploaded image; a regular author can only remove images they
// uploaded themselves.
func (u User) CanDeleteImage(uploadedBy int64) bool {
	if u.Role == RoleAdmin || u.Role == RolePower {
		return true
	}
	return u.Role == RoleRegular && u.ID == uploadedBy
}

type Category struct {
	ID        int64
	WID       int64
	ParentID  int64
	Name      string
	Slug      string
	SortOrder int
	// Description is freeform text rendered as {category_description}
	// on the category page. Authors use it for a short intro paragraph
	// above the entry list.
	Description string
	// DescriptionFormat picks the renderer for Description — "html"
	// (default, raw passthrough) or "markdown".
	DescriptionFormat string
	// TemplateID, when non-zero, overrides the rendering template for
	// this category page. 0 means "fall through to the archive-template
	// pin, then to the active template" — same resolution chain the
	// archive pages already use.
	TemplateID int64
	// Hidden, when true, drops the category from public listing surfaces
	// (home / archive / tag / feed / sidebar category_list / prev-next
	// navigation) and tells the static rebuild to skip its snapshot.
	// The individual entry permalink stays live so authors can keep
	// linking to the post; the dynamic /category/<key>/ route also keeps
	// responding 200 so a hidden archive remains reachable via a direct
	// link.
	Hidden bool
}

// MCPToken is a bearer credential the admin issues for MCP clients
// (Claude Desktop, LM Studio, Cursor, etc.) to hit the HTTP /mcp
// endpoint remotely. The raw token is returned once at creation time
// (like a GitHub PAT); only a sha256 hash is persisted. Prefix stores
// the first few bytes of the raw token so the admin list can
// disambiguate without revealing the secret.
type MCPToken struct {
	ID        int64
	WID       int64
	Name      string
	TokenHash string
	Prefix    string
	Scope     MCPScope
	// AuthorID binds the token to one user so every write tool
	// attributes new / updated entries to a real person. Required on
	// every row; migration 0027 defaults pre-existing tokens to the
	// seed admin (user id 1) so they keep a stable attribution.
	// Read-scope tokens carry a bound author too — it has no functional
	// effect today but keeps the column populated in case a future
	// feature needs it.
	AuthorID   int64
	CreatedAt  int64
	LastUsedAt int64
	RevokedAt  int64
}

// MCPScope gates what a token may do on the /mcp JSON-RPC surface.
// Two values ship today: "read" (every tools/* that doesn't mutate)
// and "write" (adds create_entry / update_entry / publish_entry). The
// column defaults to "read" so any pre-existing row stays read-only
// after the scope column was introduced.
type MCPScope string

const (
	MCPScopeRead  MCPScope = "read"
	MCPScopeWrite MCPScope = "write"
)

// Valid reports whether the scope string is a value we recognise.
// Unknown values from the DB get normalised to read on read; admin
// input is validated before persisting.
func (s MCPScope) Valid() bool {
	switch s {
	case MCPScopeRead, MCPScopeWrite:
		return true
	}
	return false
}

// CanWrite is the one permission check every mutation tool needs.
// Kept as a method so read sites stay literal (MCPScopeRead) while
// write checks read as `tok.Scope.CanWrite()`.
func (s MCPScope) CanWrite() bool { return s == MCPScopeWrite }

// Active reports whether the token is currently usable. Revoked tokens
// return false and should never authorise a request.
func (t MCPToken) Active() bool { return t.RevokedAt == 0 }

// LinkKind distinguishes SB3's two link-list row shapes. A group row has
// no URL and instead holds child link rows whose ParentID points at it.
const (
	LinkKindLink  = "link"
	LinkKindGroup = "group"
)

// Link is one row of the blogroll-style link list (SB3's `sb_link`
// table). Groups (Kind == LinkKindGroup) hold nothing of their own —
// they're rendered as a `<li><span>Name</span><ul>…children…</ul></li>`
// wrapper, where children are links whose ParentID equals the group's
// ID. Root-level links (Kind == LinkKindLink, ParentID == 0) render as
// plain `<li><a>Name</a></li>`. One level of grouping is supported;
// groups cannot nest inside groups.
type Link struct {
	ID          int64
	WID         int64
	Name        string // anchor text / group label
	URL         string // href (empty for groups)
	Description string // anchor title attribute
	Target      string // anchor target attribute
	Kind        string // LinkKindLink or LinkKindGroup
	ParentID    int64  // 0 for groups and root-level links; group id for members
	SortOrder   int
	Disp        int // 0 = visible, 1 = hidden
	CreatedAt   int64
	UpdatedAt   int64
}

// IsGroup returns true when this row is the container for other links.
func (l Link) IsGroup() bool { return l.Kind == LinkKindGroup }

// EntryStatus mirrors SB3's entry_stat convention: draft/published/closed.
type EntryStatus int

const (
	EntryDraft     EntryStatus = 0
	EntryPublished EntryStatus = 1
	EntryClosed    EntryStatus = -1
)

// Uncategorized matches SB3's convention where category id == -1 means
// "no category selected".
const Uncategorized int64 = -1

type Entry struct {
	ID         int64
	WID        int64
	AuthorID   int64
	CategoryID int64
	Title      string
	// Slug is an optional custom permalink identifier. When empty the
	// entry is served at /entry/<id>/; when non-empty the canonical URL
	// becomes /entry/<slug>/ and the numeric id URL 301s to it so
	// previously-cached reader links still resolve. Format: lowercase
	// alphanumerics + single hyphens between segments (see IsValidSlug).
	Slug string
	// Keywords is a comma-separated SEO keyword list, surfaced as
	// {entry_keywords} for templates that render <meta name="keywords">.
	// Purely descriptive metadata; not used for querying.
	Keywords string
	Body     string // 本文
	More     string // 追記 (sequel)
	Format   string
	Status   EntryStatus
	// OGBGImagePath overrides the weblog-level default for this entry's
	// Open Graph card. Stored as an image stored_path so the renderer
	// can look up the absolute file path from ImageDir. Empty means
	// "fall through to weblog.OGBGImagePath → embedded default".
	OGBGImagePath string
	// Pinned floats the entry to the top of home and category list page 1.
	Pinned bool
	// AcceptComments lets the author opt this individual entry out of
	// comment submissions even when the weblog's CommentMode is open or
	// moderated. Defaults to true so existing entries keep their old
	// behaviour. The weblog-level CommentMode still wins: when it is
	// CommentClosed, AcceptComments has no effect.
	AcceptComments bool
	PostedAt       time.Time
	UpdatedAt      time.Time
	// LikesCount is a denormalised counter kept in sync by LikeEntry. The
	// authoritative set of "who liked" lives in the entry_likes table; we
	// read this column hot-path to avoid a COUNT(*) per render.
	LikesCount int64
	// StampsCount is the aggregate stamp count across every stamp_kind.
	// Kept in sync by StampEntry the same way LikesCount is. Per-kind
	// breakdowns are queried on demand via StampCountsByEntry.
	StampsCount int64
	// CommentsCount is the denormalised count of approved comments on
	// this entry. Kept in sync by comment CUD operations so template
	// rendering never needs a COUNT(*) per entry on list pages.
	CommentsCount int64
}

// StampKind is the short identifier for one reaction. Fixed set so URLs
// and DB rows stay stable; view layer renders the matching emoji.
type StampKind string

const (
	StampHeart StampKind = "heart"
	StampLaugh StampKind = "laugh"
	StampWow   StampKind = "wow"
	StampParty StampKind = "party"
)

// StampKinds is the canonical list of supported reactions, in display
// order. Iterate when rendering stamp buttons so additions here land on
// public templates automatically.
var StampKinds = []StampKind{StampHeart, StampLaugh, StampWow, StampParty}

func (k StampKind) Valid() bool {
	switch k {
	case StampHeart, StampLaugh, StampWow, StampParty:
		return true
	}
	return false
}

// Emoji returns the visible character for a stamp kind. Kept next to
// the enum so templates can stay free of the mapping.
func (k StampKind) Emoji() string {
	switch k {
	case StampHeart:
		return "❤"
	case StampLaugh:
		return "😂"
	case StampWow:
		return "😮"
	case StampParty:
		return "🎉"
	}
	return ""
}

// Tag is an orthogonal keyword label attached to one or more entries.
// Name is the display form; Slug is the URL-safe identifier used in
// /tag/<slug>/ and in the unique index. Unlike Category, an entry can
// carry any number of tags.
type Tag struct {
	ID        int64
	WID       int64
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Template struct {
	ID        int64
	WID       int64
	Name      string
	IsActive  bool
	MainBody  string
	EntryBody string
	CSS       string
	Info      string
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TemplateAsset is one binary file that ships alongside a template (icons,
// background images, etc.). The disk location is deterministic off
// (template_id, filename); the row just tracks metadata so the admin UI
// can list + delete entries without touching the filesystem.
type TemplateAsset struct {
	ID         int64
	TemplateID int64
	Filename   string
	MimeType   string
	SizeBytes  int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// CustomTag is a user-defined {custom_*} sbtemplate placeholder.
type CustomTag struct {
	ID        int64
	WID       int64
	Name      string
	Value     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PageStatus mirrors EntryStatus semantics but only has two states.
type PageStatus int

const (
	PageDraft     PageStatus = 0
	PagePublished PageStatus = 1
)

// MessageStatus mirrors SB3's message_stat convention for user-facing comments.
type MessageStatus int

const (
	MessageWaiting  MessageStatus = 0  // pending moderation, not public
	MessageApproved MessageStatus = 1  // public
	MessageHidden   MessageStatus = -1 // moderator closed / soft-deleted
)

// Image is one uploaded asset tracked in the images table. StoredPath and
// ThumbPath are both relative to the image root on disk (configured via
// SB_IMAGE_DIR); the URL served to readers is "/img/<stored_path>".
type Image struct {
	ID         int64
	WID        int64
	UploadedBy int64
	Kind       string // "image" | "audio" | "document" | "movie"
	Filename   string
	StoredPath string
	ThumbPath  string
	MimeType   string
	SizeBytes  int64
	Width      sql.NullInt64 // NULL = image 以外 or 未取得
	Height     sql.NullInt64
	// AltText is the descriptive text used by screen-reader / missing-
	// image fallback. Populated by the vision provider when the
	// uploader's AIAutoAlt preference is on; the image picker and
	// ImageInsert feature consume it when inserting images into
	// entries.
	AltText   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Upload kind constants. Kept in domain so every layer can reference
// them without depending on internal/images.
const (
	KindImage    = "image"
	KindAudio    = "audio"
	KindDocument = "document"
	KindMovie    = "movie"
)

// Page is a standalone flat page (not an entry) reachable at a custom
// slug path such as /about or /privacy. It does not appear in feeds,
// archives, or entry lists.
type Page struct {
	ID            int64
	WID           int64
	AuthorID      int64
	Title         string
	Body          string
	Format        string // "html" or "markdown"
	Slug          string // leading "/" included, e.g. "/about"
	TemplateID    int64  // 0 = active template
	SortOrder     int
	Status        PageStatus
	OGBGImagePath string // per-page OG background override; empty = inherit weblog default
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Message is a visitor-submitted comment attached to one Entry. Name is
// required; email and url are optional for contact / backlink display.
type Message struct {
	ID          int64
	WID         int64
	EntryID     int64
	Status      MessageStatus
	PostedAt    time.Time
	AuthorName  string
	AuthorEmail string
	AuthorURL   string
	Body        string
	IPAddress   string
	UserAgent   string
}

// Webhook is a single outbound webhook subscription. Events is the JSON
// array of subscribed event ids as stored in the DB; callers translate
// to []string via webhook.DecodeEvents.
type Webhook struct {
	ID            int64
	WID           int64
	URL           string
	Secret        string
	Events        []string // decoded from the events JSON column
	Active        bool
	PayloadFormat string // "envelope" (default, nested) or "flat" (single-level)
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// WebhookDelivery is one attempt to POST a payload to a webhook URL.
// StatusCode is nil while in flight and is filled in once the request
// finishes; DeliveredAt mirrors that — non-nil only on completion.
type WebhookDelivery struct {
	ID          int64
	WebhookID   int64
	Event       string
	DeliveryID  string
	Payload     string
	StatusCode  *int
	Error       string
	DeliveredAt *time.Time
	CreatedAt   time.Time
}
