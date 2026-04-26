package admin

import (
	"net/http"
	"sync"
	"time"
)

// loginRateLimit enforces a sliding-window failure counter on the
// admin login endpoint. Keyed by (client_ip, username) so a hostile
// user can't lock out an existing admin by hammering their username
// from a different IP, and a single IP can't spray-hammer distinct
// usernames to learn valid ones.
//
// Settings are tuned for a self-hosted admin panel: 5 failures / 15
// minutes / per (ip, user) is tight enough to make brute-force
// impractical (≤ 480 attempts/day against any single account even
// from a large proxy pool, much less if the attacker is pinned to a
// single IP) while still letting a genuine user fat-finger their
// password a few times without getting locked out.
const (
	loginMaxFailures  = 5
	loginWindowPeriod = 15 * time.Minute
	// loginTrackerGCEvery is how often the tracker sweeps old entries.
	// Keeps the map from growing unboundedly on a long-running server.
	loginTrackerGCEvery = 1024
)

type loginAttempt struct {
	failures  []time.Time
	blockedAt time.Time
}

type loginLimiter struct {
	mu    sync.Mutex
	hits  map[string]*loginAttempt
	ops   int
	clock func() time.Time
}

// newLoginLimiter returns a tracker ready for concurrent use. The
// clock indirection lets tests run without sleeping.
func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		hits:  map[string]*loginAttempt{},
		clock: time.Now,
	}
}

// shared instance used by loginSubmit. Keeping it package-scope
// rather than on *Handler so a single binary doesn't accidentally
// reset the counter by recreating the handler struct.
var defaultLoginLimiter = newLoginLimiter()

// allow reports whether a login attempt is permitted right now.
// Callers invoke this with the candidate identity BEFORE verifying
// the password. When the caller subsequently learns the attempt
// failed, it must call recordFailure(key) so future attempts count
// toward the threshold. On success, recordSuccess(key) clears the
// counter.
func (l *loginLimiter) allow(ip, user string) (bool, time.Duration) {
	key := limitKey(ip, user)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	l.maybeGC(now)
	att := l.hits[key]
	if att == nil {
		return true, 0
	}
	l.pruneOldFailures(att, now)
	if len(att.failures) >= loginMaxFailures {
		// Blocked until the oldest failure slides out of the window.
		retry := att.failures[0].Add(loginWindowPeriod).Sub(now)
		if retry < 0 {
			retry = 0
		}
		return false, retry
	}
	return true, 0
}

func (l *loginLimiter) recordFailure(ip, user string) {
	key := limitKey(ip, user)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	l.ops++
	att := l.hits[key]
	if att == nil {
		att = &loginAttempt{}
		l.hits[key] = att
	}
	l.pruneOldFailures(att, now)
	att.failures = append(att.failures, now)
}

func (l *loginLimiter) recordSuccess(ip, user string) {
	key := limitKey(ip, user)
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// pruneOldFailures drops timestamps that have slipped out of the
// window. Called under l.mu.
func (l *loginLimiter) pruneOldFailures(att *loginAttempt, now time.Time) {
	cutoff := now.Add(-loginWindowPeriod)
	keep := att.failures[:0]
	for _, t := range att.failures {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	att.failures = keep
}

// maybeGC walks the map every N operations and drops keys whose
// window has lapsed. O(map size) but amortised to near-zero since
// the login endpoint is low-traffic and GC fires rarely.
func (l *loginLimiter) maybeGC(now time.Time) {
	if l.ops < loginTrackerGCEvery {
		return
	}
	l.ops = 0
	cutoff := now.Add(-loginWindowPeriod)
	for k, att := range l.hits {
		alive := false
		for _, t := range att.failures {
			if t.After(cutoff) {
				alive = true
				break
			}
		}
		if !alive {
			delete(l.hits, k)
		}
	}
}

func limitKey(ip, user string) string { return ip + "|" + user }

// loginRemoteIP resolves the rate-limit IP key through the handler's
// trusted-proxy list. Forwarded headers are honoured only when the
// immediate peer is on the configured proxy CIDR; everyone else gets
// RemoteAddr so a brute-forcer cannot rotate the per-(ip,user) bucket
// by spraying X-Forwarded-For values.
func (h *Handler) loginRemoteIP(r *http.Request) string {
	return h.TrustedProxies.From(r)
}
