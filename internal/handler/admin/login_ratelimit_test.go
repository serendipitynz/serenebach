package admin

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/storage"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// fakeClock returns successive time values the test controls, so the
// sliding-window math can be exercised without sleeping.
type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func newLimiterAt(t *testing.T, at time.Time) (*loginLimiter, *fakeClock) {
	t.Helper()
	h, _ := newAdminTestHandler(t)
	fc := &fakeClock{now: at}
	l := newLoginLimiter(h.Store)
	l.clock = fc.Now
	return l, fc
}

var bg = context.Background()

func TestLoginLimiterAllowsUntilThreshold(t *testing.T) {
	l, _ := newLimiterAt(t, time.Now())
	for i := 0; i < loginMaxFailures; i++ {
		if ok, _ := l.allow(bg, "1.2.3.4", "alice"); !ok {
			t.Fatalf("attempt %d should be allowed", i)
		}
		l.recordFailure(bg, "1.2.3.4", "alice")
	}
	ok, retry := l.allow(bg, "1.2.3.4", "alice")
	if ok {
		t.Errorf("should be blocked after %d failures", loginMaxFailures)
	}
	if retry <= 0 {
		t.Errorf("retry duration should be positive, got %v", retry)
	}
}

func TestLoginLimiterUnblocksAfterWindow(t *testing.T) {
	l, clock := newLimiterAt(t, time.Now())
	for i := 0; i < loginMaxFailures; i++ {
		l.recordFailure(bg, "1.2.3.4", "alice")
	}
	if ok, _ := l.allow(bg, "1.2.3.4", "alice"); ok {
		t.Fatalf("should be blocked immediately after failures")
	}
	// Slide the clock past the window — failures drop out.
	clock.now = clock.now.Add(loginWindowPeriod + time.Second)
	if ok, _ := l.allow(bg, "1.2.3.4", "alice"); !ok {
		t.Errorf("should be allowed after the window elapsed")
	}
}

func TestLoginLimiterSuccessClearsCounter(t *testing.T) {
	l, _ := newLimiterAt(t, time.Now())
	for i := 0; i < loginMaxFailures-1; i++ {
		l.recordFailure(bg, "1.2.3.4", "alice")
	}
	l.recordSuccess(bg, "1.2.3.4", "alice")
	// After a successful login, the counter resets so the next
	// attempts start from zero again.
	for i := 0; i < loginMaxFailures; i++ {
		if ok, _ := l.allow(bg, "1.2.3.4", "alice"); !ok {
			t.Fatalf("attempt %d post-success should be allowed", i)
		}
		l.recordFailure(bg, "1.2.3.4", "alice")
	}
}

func TestLoginLimiterIsolatesIPAndUser(t *testing.T) {
	l, _ := newLimiterAt(t, time.Now())
	for i := 0; i < loginMaxFailures; i++ {
		l.recordFailure(bg, "1.2.3.4", "alice")
	}
	if ok, _ := l.allow(bg, "1.2.3.4", "alice"); ok {
		t.Errorf("alice from 1.2.3.4 should be blocked")
	}
	// Different IP, same user — allowed.
	if ok, _ := l.allow(bg, "5.6.7.8", "alice"); !ok {
		t.Errorf("alice from 5.6.7.8 should be allowed (different IP)")
	}
	// Same IP, different user — allowed.
	if ok, _ := l.allow(bg, "1.2.3.4", "bob"); !ok {
		t.Errorf("bob from 1.2.3.4 should be allowed (different user)")
	}
}

func openFileStore(t *testing.T, path string) (*repo.Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return repo.New(db), db
}

// Under CGI every request is a new process with a new limiter and a new
// database handle; the only thing they share is the database file. Each
// failure here goes through its own handle and limiter to stand in for
// that, and the block must still land on the sixth attempt.
func TestLoginLimiterStateOutlivesTheProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sb.db")
	now := time.Now()
	// inProcess runs one attempt against its own handle and limiter and
	// closes the handle afterwards, as the CGI process would on exit.
	inProcess := func(attempt func(*loginLimiter)) {
		store, db := openFileStore(t, path)
		defer db.Close()
		l := newLoginLimiter(store)
		l.clock = func() time.Time { return now }
		attempt(l)
	}

	for i := 0; i < loginMaxFailures; i++ {
		inProcess(func(l *loginLimiter) {
			if ok, _ := l.allow(bg, "1.2.3.4", "alice"); !ok {
				t.Fatalf("attempt %d should be allowed", i)
			}
			l.recordFailure(bg, "1.2.3.4", "alice")
		})
	}
	inProcess(func(l *loginLimiter) {
		if ok, retry := l.allow(bg, "1.2.3.4", "alice"); ok || retry <= 0 {
			t.Errorf("fresh process after %d failures: allow = %v, retry = %v; want blocked", loginMaxFailures, ok, retry)
		}
	})
}

func TestLoginLimiterPrunesExpiredFailures(t *testing.T) {
	h, db := newAdminTestHandler(t)
	clock := &fakeClock{now: time.Now()}
	l := newLoginLimiter(h.Store)
	l.clock = clock.Now

	for _, ip := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		l.recordFailure(bg, ip, "alice")
	}
	clock.now = clock.now.Add(loginWindowPeriod + time.Second)
	l.recordFailure(bg, "4.4.4.4", "bob")

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM login_failures`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("login_failures rows = %d, want 1 (expired rows of every key pruned)", n)
	}
}

func TestLoginLimiterStoresNoRawIdentity(t *testing.T) {
	h, db := newAdminTestHandler(t)
	l := newLoginLimiter(h.Store)
	l.recordFailure(bg, "1.2.3.4", "my-secret-password")

	var key string
	if err := db.QueryRow(`SELECT key_hash FROM login_failures`).Scan(&key); err != nil {
		t.Fatalf("read key: %v", err)
	}
	if strings.Contains(key, "1.2.3.4") || strings.Contains(key, "my-secret-password") {
		t.Errorf("key_hash = %q carries the submitted identity verbatim", key)
	}
}
