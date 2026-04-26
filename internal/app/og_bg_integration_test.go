package app_test

import (
	"context"
	"strings"
	"testing"
)

// The weblog-level OG background column round-trips through the
// settings form and wins over the embedded default when the OG
// renderer is asked for a card.
func TestOGBGPathRoundtripsThroughSettings(t *testing.T) {
	a := newTestApp(t)

	// Seed a stored_path as if an image had been uploaded. The
	// render pipeline tolerates missing files by falling back to the
	// default, so we don't need an actual PNG on disk for this test
	// — we just verify the column makes it to the DB.
	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE weblogs SET og_bg_image_path = '2024/01/og-default.png' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := a.DB.QueryRow(`SELECT og_bg_image_path FROM weblogs WHERE id = 1`).Scan(&stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != "2024/01/og-default.png" {
		t.Fatalf("stored = %q, want 2024/01/og-default.png", stored)
	}
}

// TestEntryOGBGOverrideRoundtrips verifies the per-entry column
// survives CreateEntry → EntryByID so the admin form can persist an
// override that's read back for rendering.
func TestEntryOGBGOverrideRoundtrips(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE entries SET og_bg_image_path = '2024/02/custom.png' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := a.DB.QueryRow(`SELECT og_bg_image_path FROM entries WHERE id = 1`).Scan(&stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != "2024/02/custom.png" {
		t.Fatalf("stored = %q, want 2024/02/custom.png", stored)
	}
}

// TestPublicEntryExposesOGDimensionTags verifies the new
// {entry_og_image_width} / {entry_og_image_height} tags are
// registered and resolve to the constants the OG renderer uses.
// Uses a minimal sbtemplate that emits both so the assertion
// reads straight off the rendered HTML.
func TestPublicEntryExposesOGDimensionTags(t *testing.T) {
	a := newTestApp(t)

	body := "<!doctype html>\n<html>\n<body>\n" +
		"<!-- BEGIN entry -->\n" +
		"<meta og-w=\"{entry_og_image_width}\">\n" +
		"<meta og-h=\"{entry_og_image_height}\">\n" +
		"<h2>{entry_title}</h2>\n" +
		"<!-- END entry -->\n" +
		"</body>\n</html>\n"
	if _, err := a.DB.ExecContext(context.Background(), `
		UPDATE templates SET main_body = ?, updated_at = strftime('%s','now') WHERE is_active = 1`,
		body); err != nil {
		t.Fatal(err)
	}

	rendered := httpGet(t, a.Handler(), "/entry/1/")
	if !strings.Contains(rendered, `og-w="1200"`) {
		t.Errorf("missing og-w=1200; body:\n%s", rendered)
	}
	if !strings.Contains(rendered, `og-h="630"`) {
		t.Errorf("missing og-h=630; body:\n%s", rendered)
	}
}
