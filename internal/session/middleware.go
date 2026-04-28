package session

import (
	"context"
	"net/http"
	"net/url"

	"github.com/serendipitynz/serenebach/internal/basepath"
	"github.com/serendipitynz/serenebach/internal/domain"
)

type contextKey int

const userKey contextKey = 0

func withUser(ctx context.Context, u *domain.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFrom returns the authenticated user previously attached by LoadUser,
// or nil if the request is unauthenticated.
func UserFrom(ctx context.Context) *domain.User {
	if v := ctx.Value(userKey); v != nil {
		if u, ok := v.(*domain.User); ok {
			return u
		}
	}
	return nil
}

// LoadUser is a cheap middleware: if the request has a valid session cookie,
// it attaches the corresponding user to the request context. Unauthenticated
// requests pass through untouched so public pages keep working.
func (m *Manager) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := m.UserFromRequest(r); u != nil {
			r = r.WithContext(withUser(r.Context(), u))
		}
		next.ServeHTTP(w, r)
	})
}

// RequireUser rejects unauthenticated access with a 302 to loginPath,
// preserving the original target in ?next= for post-login redirection.
// loginPath is the bare path without the base path prefix (e.g. "/admin/login");
// the deployment base path is prepended automatically from context.
func RequireUser(loginPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if UserFrom(r.Context()) == nil {
				base := basepath.FromContext(r.Context())
				dest := base + loginPath
				if r.URL.Path != "" && r.URL.Path != loginPath {
					dest += "?next=" + url.QueryEscape(r.URL.RequestURI())
				}
				http.Redirect(w, r, dest, http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
