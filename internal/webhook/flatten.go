package webhook

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// PayloadFormat enumerates the wire shapes a subscription can opt into.
// envelope is the legacy nested JSON ({id, event, timestamp, weblog,
// data...}); flat applies the slack.dev "flatten JSON for Workflow
// Builder" rule so every value lands on a top-level key joined with
// underscores (e.g. "data.title" → "data_title", "data.tags[0]" →
// "data_tags_0").
const (
	PayloadFormatEnvelope = "envelope"
	PayloadFormatFlat     = "flat"
)

// AllPayloadFormats is the canonical enumeration the admin UI iterates
// over so adding a third format only needs a new constant + this list
// (plus the encoder branch in encodeForFormat).
var AllPayloadFormats = []string{PayloadFormatEnvelope, PayloadFormatFlat}

// IsKnownPayloadFormat reports whether s is one of the supported
// payload format ids.
func IsKnownPayloadFormat(s string) bool {
	for _, f := range AllPayloadFormats {
		if f == s {
			return true
		}
	}
	return false
}

// encodeForFormat marshals the payload according to the subscription's
// format choice. Unknown formats fall back to envelope so a stale
// schema value can't break dispatch.
func encodeForFormat(p Payload, format string) ([]byte, error) {
	switch format {
	case PayloadFormatFlat:
		flat, err := flattenPayload(p)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(flat)
		if err != nil {
			return nil, fmt.Errorf("webhook: marshal flat payload: %w", err)
		}
		if len(b) > maxPayloadBytes {
			return nil, fmt.Errorf("webhook: flat payload exceeds %d bytes", maxPayloadBytes)
		}
		return b, nil
	default:
		return encodePayload(p)
	}
}

// flattenPayload converts the envelope payload into a single-level map
// per the slack.dev recommendation:
//
//   - nested object keys are joined with "_"  (a.b → a_b)
//   - array element keys use the numeric index (a[0] → a_0)
//   - scalar values keep their JSON type (string / number / bool / null)
//
// We round-trip through encoding/json instead of reflecting over the
// Payload struct so the output exactly matches what subscribers would
// otherwise see for the envelope form, just flattened.
func flattenPayload(p Payload) (map[string]any, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("webhook: marshal payload for flatten: %w", err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("webhook: unmarshal payload for flatten: %w", err)
	}
	out := map[string]any{}
	flattenInto("", tree, out)
	return out, nil
}

// flattenInto walks the JSON tree and writes scalar leaves into out
// keyed by the underscore-joined path. Empty objects/arrays do not
// produce keys (Slack Workflow Builder ignores absent variables); a
// JSON null preserves the path so subscribers can distinguish "field
// present but null" from "field absent entirely".
func flattenInto(prefix string, node any, out map[string]any) {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			flattenInto(joinKey(prefix, k), child, out)
		}
	case []any:
		for i, child := range v {
			flattenInto(joinKey(prefix, strconv.Itoa(i)), child, out)
		}
	default:
		// Scalar (string / float64 / bool / nil) — record at the path.
		if prefix == "" {
			// A bare scalar at the root has no key. Should never happen
			// for our Payload shape (always a JSON object), but guard
			// so the caller doesn't write a "" key.
			return
		}
		out[prefix] = v
	}
}

func joinKey(prefix, leaf string) string {
	if prefix == "" {
		return leaf
	}
	return prefix + "_" + leaf
}
