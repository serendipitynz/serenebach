package webhook

import "testing"

func TestSignAndVerifyRoundTrip(t *testing.T) {
	body := []byte(`{"event":"entry.published","id":"abc"}`)
	header := Sign("topsecret", body)
	if header == "" {
		t.Fatalf("Sign returned empty header")
	}
	if got, want := header[:7], "sha256="; got != want {
		t.Errorf("Sign prefix = %q, want %q", got, want)
	}
	if !Verify("topsecret", header, body) {
		t.Errorf("Verify rejected a freshly-signed body")
	}
}

func TestVerifyRejectsTamperedBody(t *testing.T) {
	body := []byte(`{"event":"entry.published","id":"abc"}`)
	header := Sign("topsecret", body)
	tampered := append([]byte{}, body...)
	tampered[len(tampered)-2] = 'Z'
	if Verify("topsecret", header, tampered) {
		t.Errorf("Verify accepted a tampered body")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	body := []byte(`{"event":"entry.published","id":"abc"}`)
	header := Sign("right-secret", body)
	if Verify("wrong-secret", header, body) {
		t.Errorf("Verify accepted a wrong-secret signature")
	}
}

func TestVerifyRejectsMalformedHeader(t *testing.T) {
	body := []byte(`{"event":"x"}`)
	for _, bad := range []string{"", "md5=abcd", "sha256=not-hex", "sha256="} {
		if Verify("s", bad, body) {
			t.Errorf("Verify accepted malformed header %q", bad)
		}
	}
}
