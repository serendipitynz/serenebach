// Package basepath carries the deployment base path (e.g. "/blog") through
// context.Context so URL builders can prepend it without threading an extra
// parameter through every handler and template helper. The value is set
// once at request-entry middleware and read everywhere downstream.
package basepath

import "context"

type contextKey struct{}

// NewContext returns a copy of ctx with base stored under the basepath key.
// base should be a clean path prefix with no trailing slash (e.g. "/sb4").
func NewContext(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, contextKey{}, base)
}

// FromContext returns the base path stored by NewContext, or "" if none was set.
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(contextKey{}).(string); ok {
		return v
	}
	return ""
}
