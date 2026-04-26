// Package i18n is a small, dependency-free string catalogue for the
// admin UI. Japanese + English are bundled. Public-side templates stay
// in whatever language the author writes —
// they are content, not UI chrome, and a single per-weblog blog
// typically picks one language at install time.
//
// Design:
//   - Catalogues are plain JSON files ({key: value}). Keys use dot
//     notation (`entries.list.title`, `users.form.email`) so the
//     tree is self-documenting; no nesting inside the JSON itself.
//   - Locale is resolved per-request: `sb_admin_lang` cookie wins,
//     then the Accept-Language header, then a hard default of `ja`.
//   - Template renders pull the active locale off the request
//     context and call `T(key)` / `Tf(key, args...)`; missing keys
//     surface as the key literal so a drift is visible in the UI
//     rather than blank.
package i18n

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Catalogue maps dot-keyed UI strings to a single locale's text.
type Catalogue map[string]string

// Bundle pairs every supported locale with its catalogue. "ja" is
// the source-of-truth; other locales inherit any missing key.
type Bundle struct {
	Default  string               // hard fallback locale (usually "ja")
	Fallback string               // locale used when a key is missing (same as Default unless overridden)
	Locales  map[string]Catalogue // locale code → catalogue
}

// New builds a Bundle from already-decoded catalogues. For loading
// from JSON, see LoadBundleFS.
func New(def string, cats map[string]Catalogue) *Bundle {
	if def == "" {
		def = "ja"
	}
	return &Bundle{Default: def, Fallback: def, Locales: cats}
}

// LoadBundle parses each JSON blob into a catalogue. `def` is the
// default locale code. Returns an error if any blob is malformed so
// startup fails loudly rather than silently rendering partial UI.
func LoadBundle(def string, raw map[string][]byte) (*Bundle, error) {
	cats := map[string]Catalogue{}
	for code, data := range raw {
		c := Catalogue{}
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("i18n: load %q: %w", code, err)
		}
		cats[code] = c
	}
	if _, ok := cats[def]; !ok {
		return nil, fmt.Errorf("i18n: default locale %q missing from bundle", def)
	}
	return &Bundle{Default: def, Fallback: def, Locales: cats}, nil
}

// Supported reports the locale codes the bundle knows about, in
// insertion order isn't guaranteed; callers sort for display.
func (b *Bundle) Supported() []string {
	out := make([]string, 0, len(b.Locales))
	for code := range b.Locales {
		out = append(out, code)
	}
	return out
}

// T resolves `key` in the given locale, falling back to the Fallback
// locale and finally to the key literal. The literal-return means
// template developers notice missing keys without the UI breaking.
func (b *Bundle) T(locale, key string) string {
	if cat, ok := b.Locales[locale]; ok {
		if v, ok := cat[key]; ok {
			return v
		}
	}
	if fall, ok := b.Locales[b.Fallback]; ok && b.Fallback != locale {
		if v, ok := fall[key]; ok {
			return v
		}
	}
	return key
}

// Tf looks up `key` then feeds the result into fmt.Sprintf with the
// provided args. Used for messages with numeric / string
// substitutions ("%d 件選択中") where the format itself needs to be
// translated alongside the static text.
func (b *Bundle) Tf(locale, key string, args ...any) string {
	return fmt.Sprintf(b.T(locale, key), args...)
}

// ---- locale resolution ------------------------------------------------

const (
	// LangCookieName is the cookie that admin.js writes when the
	// operator changes the language <select> in 管理画面 settings.
	// Server reads it on every admin render.
	LangCookieName = "sb_admin_lang"
)

// Resolve picks the active locale for a request. Lookup order:
//
//  1. `sb_admin_lang` cookie (if value is one of the supported codes)
//  2. `Accept-Language` header — first tag that matches a supported
//     code, case-insensitive, prefix match allowed (`en-US` → `en`)
//  3. Bundle.Default
//
// Unknown / empty values in the cookie silently fall through so a
// stale cookie from a removed locale can't wedge the admin in a
// broken state.
func (b *Bundle) Resolve(r *http.Request) string {
	if c, err := r.Cookie(LangCookieName); err == nil {
		if _, ok := b.Locales[c.Value]; ok {
			return c.Value
		}
	}
	if al := r.Header.Get("Accept-Language"); al != "" {
		if code := bestAcceptLanguage(al, b); code != "" {
			return code
		}
	}
	return b.Default
}

// bestAcceptLanguage walks the Accept-Language header's q-weighted
// list and returns the first supported locale (prefix-matched). We
// don't bother parsing q-values because the tags already arrive in
// preference order in every real-world client.
func bestAcceptLanguage(header string, b *Bundle) string {
	for _, part := range strings.Split(header, ",") {
		tag := strings.TrimSpace(part)
		if idx := strings.Index(tag, ";"); idx >= 0 {
			tag = strings.TrimSpace(tag[:idx])
		}
		tag = strings.ToLower(tag)
		if tag == "" {
			continue
		}
		// Exact match: `ja` or `en`
		if _, ok := b.Locales[tag]; ok {
			return tag
		}
		// Prefix match: `en-US` → `en`
		if dash := strings.Index(tag, "-"); dash > 0 {
			if _, ok := b.Locales[tag[:dash]]; ok {
				return tag[:dash]
			}
		}
	}
	return ""
}

// ---- request context plumbing -----------------------------------------

type ctxKey struct{}

// WithLocale stashes a resolved locale onto the context so template
// funcs can read it back without re-parsing the request.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, ctxKey{}, locale)
}

// LocaleFrom reads the stashed locale, or "" when nothing is set
// (middleware didn't run, unit test, etc.). Callers combine with
// Bundle.Default to get a usable value.
func LocaleFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// JSCatalogueJSON returns a JSON-encoded object carrying every key
// that starts with prefix, resolved for the given locale. The admin
// layout emits this as a <script>window.__sbI18n = {...}</script>
// bridge so admin.js can look up pre-localized strings without
// shipping the whole bundle to the browser. Returns "{}" if nothing
// matches.
func (b *Bundle) JSCatalogueJSON(locale, prefix string) string {
	out := map[string]string{}
	cat, ok := b.Locales[locale]
	if !ok {
		cat = b.Locales[b.Fallback]
	}
	for k := range cat {
		if strings.HasPrefix(k, prefix) {
			out[k] = b.T(locale, k)
		}
	}
	// Also inherit any default-locale keys the target locale is
	// missing, matching T's fallback behaviour.
	if fall, ok := b.Locales[b.Fallback]; ok && b.Fallback != locale {
		for k := range fall {
			if _, already := out[k]; already {
				continue
			}
			if strings.HasPrefix(k, prefix) {
				out[k] = fall[k]
			}
		}
	}
	buf, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(buf)
}

// Middleware resolves the locale once per request and injects it
// into context so downstream handlers / template renders see a
// single consistent choice.
func Middleware(b *Bundle) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			loc := b.Resolve(r)
			ctx := WithLocale(r.Context(), loc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
