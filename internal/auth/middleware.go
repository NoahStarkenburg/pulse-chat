package auth

import "net/http"

// SessionCookieName is the cookie that carries the opaque session token.
const SessionCookieName = "pulse_session"

// RequireAuth returns middleware that admits a request only if it carries a
// valid session cookie. On success it stores the authenticated user ID in the
// request context (read it with UserIDFromContext) and calls next. Otherwise it
// responds 401 and does not call next.
//
// It works for the WebSocket upgrade too: the upgrade is an ordinary HTTP GET,
// so the cookie is present on it like any other request.
func RequireAuth(sessions SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			userID, ok := sessions.Validate(r.Context(), cookie.Value)
			if !ok {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(withUserID(r.Context(), userID)))
		})
	}
}
