package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
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
)

// loginFailureStore is the slice of repo.Store the limiter needs.
type loginFailureStore interface {
	LoginFailuresSince(ctx context.Context, key string, since int64) (int, int64, error)
	RecordLoginFailure(ctx context.Context, key string, at, pruneBefore int64) error
	ClearLoginFailures(ctx context.Context, key string) error
}

// loginLimiter keeps its failures in the database rather than in memory:
// under CGI every request is a new process, so an in-memory counter
// restarted from zero on each attempt and limited nothing. One write per
// failed login is negligible next to the bcrypt check it guards, and a
// single path keeps the persistent server and CGI behaving alike (the
// counter now also survives a restart).
//
// A store error fails open, with a log line: the limiter must not lock
// every user out of a server whose database hiccupped, and the password
// check itself still runs.
type loginLimiter struct {
	store loginFailureStore
	clock func() time.Time
}

func newLoginLimiter(store loginFailureStore) *loginLimiter {
	return &loginLimiter{store: store, clock: time.Now}
}

// allow reports whether a login attempt is permitted right now.
// Callers invoke this with the candidate identity BEFORE verifying
// the password. When the caller subsequently learns the attempt
// failed, it must call recordFailure so future attempts count toward
// the threshold. On success, recordSuccess clears the counter.
func (l *loginLimiter) allow(ctx context.Context, ip, user string) (bool, time.Duration) {
	now := l.clock()
	count, oldest, err := l.store.LoginFailuresSince(ctx, limitKey(ip, user), now.Add(-loginWindowPeriod).Unix())
	if err != nil {
		log.Printf("admin.loginLimiter: allow: %v", err)
		return true, 0
	}
	if count < loginMaxFailures {
		return true, 0
	}
	// Blocked until the oldest failure slides out of the window.
	retry := time.Unix(oldest, 0).Add(loginWindowPeriod).Sub(now)
	if retry < 0 {
		retry = 0
	}
	return false, retry
}

func (l *loginLimiter) recordFailure(ctx context.Context, ip, user string) {
	now := l.clock()
	if err := l.store.RecordLoginFailure(ctx, limitKey(ip, user), now.Unix(), now.Add(-loginWindowPeriod).Unix()); err != nil {
		log.Printf("admin.loginLimiter: record failure: %v", err)
	}
}

func (l *loginLimiter) recordSuccess(ctx context.Context, ip, user string) {
	if err := l.store.ClearLoginFailures(ctx, limitKey(ip, user)); err != nil {
		log.Printf("admin.loginLimiter: clear: %v", err)
	}
}

// limitKey is stored, so it is hashed: the submitted name can be a
// password typed into the wrong field. The NUL separator keeps
// ("1.2.3.4", "x|y") and ("1.2.3.4|x", "y") apart.
func limitKey(ip, user string) string {
	sum := sha256.Sum256([]byte(ip + "\x00" + user))
	return hex.EncodeToString(sum[:])
}

// loginRemoteIP resolves the rate-limit IP key through the handler's
// trusted-proxy list. Forwarded headers are honoured only when the
// immediate peer is on the configured proxy CIDR; everyone else gets
// RemoteAddr so a brute-forcer cannot rotate the per-(ip,user) bucket
// by spraying X-Forwarded-For values.
func (h *Handler) loginRemoteIP(r *http.Request) string {
	return h.TrustedProxies.From(r)
}
