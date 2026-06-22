package auth

import "context"

// contextKey is an unexported type so no other package can collide with or
// overwrite the value we store under it.
type contextKey int

const userIDKey contextKey = iota

// withUserID returns a copy of ctx carrying the authenticated user's ID. It is
// unexported because only RequireAuth should set it.
func withUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserIDFromContext returns the authenticated user's ID, if RequireAuth ran on
// the request. The bool is false when there is no authenticated user.
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey).(string)
	return id, ok
}
