package helpdocs

import "testing"

// Lookup with the default locale returns the requested page directly,
// no fallback marker.
func TestLookupDefaultLocale(t *testing.T) {
	p, fellBack := Lookup("getting-started", "ja")
	if p == nil {
		t.Fatal("expected getting-started ja page")
	}
	if fellBack {
		t.Errorf("fellBack = true, want false for default locale lookup")
	}
	if p.Locale != "ja" {
		t.Errorf("locale = %q, want ja", p.Locale)
	}
}

// Lookup with a translated locale returns the localised page without
// the fallback flag.
func TestLookupTranslatedLocale(t *testing.T) {
	p, fellBack := Lookup("getting-started", "en")
	if p == nil {
		t.Fatal("expected getting-started en page")
	}
	if fellBack {
		t.Errorf("fellBack = true, want false for translated locale")
	}
	if p.Locale != "en" {
		t.Errorf("locale = %q, want en", p.Locale)
	}
}

// Lookup against a locale that ships no catalogue at all falls back
// to the default locale and reports fellBack=true. We use "fr" here
// because the loader only knows about ja and en today; if a future
// translation lands for fr, swap to another unsupported tag.
func TestLookupFallsBackToDefaultLocale(t *testing.T) {
	p, fellBack := Lookup("getting-started", "fr")
	if p == nil {
		t.Fatal("expected ja fallback page when locale is unknown")
	}
	if !fellBack {
		t.Errorf("fellBack = false, want true for unknown locale")
	}
	if p.Locale != "ja" {
		t.Errorf("locale = %q, want ja (fallback)", p.Locale)
	}
}

// Lookup of an unknown slug returns nil even when the locale is
// supported. The handler treats nil as 404.
func TestLookupUnknownSlugReturnsNil(t *testing.T) {
	p, _ := Lookup("does-not-exist", "ja")
	if p != nil {
		t.Errorf("got page %+v, want nil for unknown slug", p)
	}
}

// Index for a non-default locale merges the requested locale's
// translations on top of the default catalogue, so the sidebar can
// always render every slug. Untranslated slugs surface with the
// default locale's title; translated slugs use the requested
// locale's title.
func TestIndexMergesTranslations(t *testing.T) {
	enIndex := Index("en")
	jaIndex := Index("ja")
	if len(enIndex) != len(jaIndex) {
		t.Fatalf("len(en index) = %d, want %d (matches ja)",
			len(enIndex), len(jaIndex))
	}
	// Every en index entry whose slug is also in en/ should resolve
	// to a Page.Locale of "en"; the rest should fall back to "ja".
	for _, p := range enIndex {
		if p.Locale != "en" && p.Locale != "ja" {
			t.Errorf("unexpected locale %q on slug %q", p.Locale, p.Slug)
		}
	}
}
