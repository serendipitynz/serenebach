package admin

import (
	"testing"
	"time"
)

// fakeClock returns successive time values the test controls, so the
// sliding-window math can be exercised without sleeping.
type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func newLimiterAt(t time.Time) (*loginLimiter, *fakeClock) {
	fc := &fakeClock{now: t}
	l := newLoginLimiter()
	l.clock = fc.Now
	return l, fc
}

func TestLoginLimiterAllowsUntilThreshold(t *testing.T) {
	l, _ := newLimiterAt(time.Now())
	for i := 0; i < loginMaxFailures; i++ {
		if ok, _ := l.allow("1.2.3.4", "alice"); !ok {
			t.Fatalf("attempt %d should be allowed", i)
		}
		l.recordFailure("1.2.3.4", "alice")
	}
	ok, retry := l.allow("1.2.3.4", "alice")
	if ok {
		t.Errorf("should be blocked after %d failures", loginMaxFailures)
	}
	if retry <= 0 {
		t.Errorf("retry duration should be positive, got %v", retry)
	}
}

func TestLoginLimiterUnblocksAfterWindow(t *testing.T) {
	l, clock := newLimiterAt(time.Now())
	for i := 0; i < loginMaxFailures; i++ {
		l.recordFailure("1.2.3.4", "alice")
	}
	if ok, _ := l.allow("1.2.3.4", "alice"); ok {
		t.Fatalf("should be blocked immediately after failures")
	}
	// Slide the clock past the window — failures drop out.
	clock.now = clock.now.Add(loginWindowPeriod + time.Second)
	if ok, _ := l.allow("1.2.3.4", "alice"); !ok {
		t.Errorf("should be allowed after the window elapsed")
	}
}

func TestLoginLimiterSuccessClearsCounter(t *testing.T) {
	l, _ := newLimiterAt(time.Now())
	for i := 0; i < loginMaxFailures-1; i++ {
		l.recordFailure("1.2.3.4", "alice")
	}
	l.recordSuccess("1.2.3.4", "alice")
	// After a successful login, the counter resets so the next
	// attempts start from zero again.
	for i := 0; i < loginMaxFailures; i++ {
		if ok, _ := l.allow("1.2.3.4", "alice"); !ok {
			t.Fatalf("attempt %d post-success should be allowed", i)
		}
		l.recordFailure("1.2.3.4", "alice")
	}
}

func TestLoginLimiterIsolatesIPAndUser(t *testing.T) {
	l, _ := newLimiterAt(time.Now())
	for i := 0; i < loginMaxFailures; i++ {
		l.recordFailure("1.2.3.4", "alice")
	}
	if ok, _ := l.allow("1.2.3.4", "alice"); ok {
		t.Errorf("alice from 1.2.3.4 should be blocked")
	}
	// Different IP, same user — allowed.
	if ok, _ := l.allow("5.6.7.8", "alice"); !ok {
		t.Errorf("alice from 5.6.7.8 should be allowed (different IP)")
	}
	// Same IP, different user — allowed.
	if ok, _ := l.allow("1.2.3.4", "bob"); !ok {
		t.Errorf("bob from 1.2.3.4 should be allowed (different user)")
	}
}
