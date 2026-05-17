package webhook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestFlattenPayloadEntryShape(t *testing.T) {
	weblog := domain.Weblog{ID: 1, Title: "My Blog", BaseURL: "https://example.com/"}
	entry := domain.Entry{
		ID:       42,
		Slug:     "hello",
		Title:    "Hello",
		Status:   domain.EntryPublished,
		PostedAt: time.Date(2026, 5, 16, 12, 34, 56, 0, time.UTC),
	}
	author := domain.User{ID: 7, Name: "admin", DisplayName: "Admin"}
	p := EntryPayload(weblog, entry, author, "https://example.com/entry/hello/",
		[]string{"雑記", "技術"}, []string{"go", "serenebach"}, EventEntryPublished)

	got, err := flattenPayload(p)
	if err != nil {
		t.Fatalf("flattenPayload: %v", err)
	}

	// Spot-check the key shapes the flat format promises:
	//   - top-level scalars survive,
	//   - nested objects collapse with "_" joins,
	//   - arrays use numeric indices in the same join.
	wantKV := map[string]any{
		"event":             "entry.published",
		"weblog_title":      "My Blog",
		"weblog_url":        "https://example.com/",
		"data_url":          "https://example.com/entry/hello/",
		"data_status":       "published",
		"data_author_name":  "Admin",
		"data_categories_0": "雑記",
		"data_categories_1": "技術",
		"data_tags_0":       "go",
		"data_tags_1":       "serenebach",
	}
	for k, want := range wantKV {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}

	assertFlatPayloadScalarsOnly(t, got)
}

// assertFlatPayloadScalarsOnly round-trips the map through JSON and
// asserts every decoded value is a scalar (nil/bool/float64/string),
// proving the flatten step removed every nested object or array.
//
// We can't sniff "[" / "{" against the encoded bytes naively, because
// the injected text/content summary may legitimately contain those
// characters ("[My Blog]"). Decoding gives a clean per-value check.
func assertFlatPayloadScalarsOnly(t *testing.T, got map[string]any) {
	t.Helper()
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal flat: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("re-unmarshal flat: %v", err)
	}
	for k, v := range decoded {
		switch v.(type) {
		case nil, bool, float64, string:
			// Scalar — OK.
		default:
			t.Errorf("flat key %q has non-scalar value %T: %v", k, v, v)
		}
	}
}

func TestFlattenPayloadScalarTypesPreserved(t *testing.T) {
	// Bool, float, and null must survive flatten with their JSON type
	// intact (subscribers that decode into typed structs should still
	// see numbers as numbers, etc.).
	p := Payload{
		ID:        "abc",
		Event:     EventEntryPublished,
		Timestamp: "2026-05-16T12:34:56Z",
		Data: map[string]any{
			"count":  float64(3),
			"is_new": true,
			"label":  nil,
		},
	}
	got, err := flattenPayload(p)
	if err != nil {
		t.Fatalf("flattenPayload: %v", err)
	}
	if v, ok := got["data_count"].(float64); !ok || v != 3 {
		t.Errorf("data_count = %v (%T), want 3 (float64)", got["data_count"], got["data_count"])
	}
	if v, ok := got["data_is_new"].(bool); !ok || !v {
		t.Errorf("data_is_new = %v (%T), want true", got["data_is_new"], got["data_is_new"])
	}
	// nil preserved as nil so callers can distinguish "present and
	// null" from "key missing entirely".
	if v, present := got["data_label"]; !present || v != nil {
		t.Errorf("data_label = %v / present=%v, want nil/true", v, present)
	}
}

func TestEncodeForFormatBranches(t *testing.T) {
	p := Payload{
		ID: "x", Event: EventEntryPublished,
		Timestamp: "2026-05-16T12:34:56Z",
		Data:      map[string]any{"n": float64(1)},
	}
	envelope, err := encodeForFormat(p, PayloadFormatEnvelope)
	if err != nil {
		t.Fatalf("encodeForFormat envelope: %v", err)
	}
	if !strings.Contains(string(envelope), `"data":{`) {
		t.Errorf("envelope JSON should keep data as nested object, got %s", envelope)
	}
	flat, err := encodeForFormat(p, PayloadFormatFlat)
	if err != nil {
		t.Fatalf("encodeForFormat flat: %v", err)
	}
	if !strings.Contains(string(flat), `"data_n":1`) {
		t.Errorf("flat JSON should contain data_n, got %s", flat)
	}
	// Unknown format falls back to envelope (defensive against stale DB).
	fallback, err := encodeForFormat(p, "unknown-future")
	if err != nil {
		t.Fatalf("encodeForFormat unknown: %v", err)
	}
	if !strings.Contains(string(fallback), `"data":{`) {
		t.Errorf("unknown format should fall back to envelope, got %s", fallback)
	}
}

func TestIsKnownPayloadFormat(t *testing.T) {
	for _, ok := range []string{"envelope", "flat"} {
		if !IsKnownPayloadFormat(ok) {
			t.Errorf("IsKnownPayloadFormat(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "slack", "discord", "ENVELOPE"} {
		if IsKnownPayloadFormat(bad) {
			t.Errorf("IsKnownPayloadFormat(%q) = true, want false", bad)
		}
	}
}
