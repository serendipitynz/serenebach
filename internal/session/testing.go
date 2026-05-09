package session

import (
	"context"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// WithUser injects u into ctx so handlers that call UserFrom see it.
// Intended for use in tests that drive handlers directly without a
// full session cookie round-trip.
func WithUser(ctx context.Context, u *domain.User) context.Context {
	return withUser(ctx, u)
}
