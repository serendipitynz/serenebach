package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// Sign returns the X-SB-Signature header value for a payload body. The
// format is "sha256=<hex>" so receivers can switch on the prefix if
// future versions introduce a different algorithm.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether the given signature header matches the body
// under the secret. Comparison runs in constant time. The function is
// not used inside Serene Bach itself — it lives here as a
// reference implementation receivers can mirror.
func Verify(secret, header string, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(header[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	want := mac.Sum(nil)
	return subtle.ConstantTimeCompare(got, want) == 1
}
