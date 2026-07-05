package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// Length bounds. The password maximum is bcrypt's 72-byte input limit.
const (
	minUsernameLen = 3
	maxUsernameLen = 32
	minPasswordLen = 8
	maxPasswordLen = 72
)

// LoginLimiter throttles authentication attempts by client IP, so brute-force
// guessing and signup spam from one source are slowed before any user is known.
// *cache.Cache satisfies it.
type LoginLimiter interface {
	AllowLogin(ctx context.Context, ip string) (bool, error)
}

// Handlers serves the authentication HTTP endpoints.
type Handlers struct {
	users    UserStore
	sessions SessionStore
	limiter  LoginLimiter
	logger   *slog.Logger
	secure   bool // set the cookie Secure flag (true behind HTTPS)
}

// NewHandlers constructs the auth HTTP handlers. secure should be true in
// production so the session cookie is only sent over HTTPS.
func NewHandlers(users UserStore, sessions SessionStore, limiter LoginLimiter, logger *slog.Logger, secure bool) *Handlers {
	return &Handlers{users: users, sessions: sessions, limiter: limiter, logger: logger, secure: secure}
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// Signup creates a user and logs them in. Mounted on POST /signup.
func (h *Handlers) Signup(w http.ResponseWriter, r *http.Request) {
	if !h.allowAttempt(w, r) {
		return
	}
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}
	username := normalizeUsername(creds.Username)
	if len(username) < minUsernameLen || len(username) > maxUsernameLen ||
		len(creds.Password) < minPasswordLen || len(creds.Password) > maxPasswordLen {
		http.Error(w, "username must be 3-32 characters and password 8-72", http.StatusBadRequest)
		return
	}

	hash, err := HashPassword(creds.Password)
	if err != nil {
		h.logger.Error("hashing password", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user, err := h.users.Create(r.Context(), username, hash)
	if err != nil {
		if errors.Is(err, ErrUserExists) {
			http.Error(w, "username already taken", http.StatusConflict)
			return
		}
		h.logger.Error("creating user", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if !h.issueSessionCookie(r.Context(), w, user.ID) {
		return
	}
	writeJSON(w, http.StatusCreated, userResponse{ID: user.ID, Username: user.Username})
}

// Login validates credentials and issues a session cookie. Mounted on POST /login.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	if !h.allowAttempt(w, r) {
		return
	}
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}

	username := normalizeUsername(creds.Username)
	user, err := h.users.ByUsername(r.Context(), username)
	if err != nil {
		// Verify against a dummy hash anyway so timing does not reveal whether
		// the username exists, and return the same error as a wrong password.
		_ = VerifyPassword(creds.Password, string(dummyPasswordHash))
		h.logger.Info("failed login", "username", username, "remote", r.RemoteAddr, "reason", "unknown user")
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err := VerifyPassword(creds.Password, user.PasswordHash); err != nil {
		h.logger.Info("failed login", "username", username, "remote", r.RemoteAddr, "reason", "bad password")
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if !h.issueSessionCookie(r.Context(), w, user.ID) {
		return
	}
	writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Username: user.Username})
}

// Logout invalidates the session and clears the cookie. Mounted on POST /logout.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		if err := h.sessions.Delete(r.Context(), cookie.Value); err != nil {
			h.logger.Warn("deleting session on logout failed", "err", err)
		}
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the authenticated user. Mount behind RequireAuth (GET /me).
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	user, err := h.users.ByID(r.Context(), userID)
	if err != nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Username: user.Username})
}

// issueSessionCookie creates a session and sets the cookie. It returns false
// (after writing an error response) if the session could not be created.
func (h *Handlers) issueSessionCookie(ctx context.Context, w http.ResponseWriter, userID string) bool {
	token, err := h.sessions.Issue(ctx, userID)
	if err != nil {
		h.logger.Error("issuing session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return true
}

func (h *Handlers) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1, // delete immediately
	})
}

// allowAttempt enforces the per-IP authentication rate limit. It writes 429 and
// returns false when the limit is exceeded. On a limiter error it fails open
// (allows the attempt) and logs, like the message limiter: rate limiting is a
// protective measure, not a correctness gate, so a limiter outage should not
// lock everyone out of login.
func (h *Handlers) allowAttempt(w http.ResponseWriter, r *http.Request) bool {
	allowed, err := h.limiter.AllowLogin(r.Context(), clientIP(r))
	if err != nil {
		h.logger.Warn("auth rate limiter unavailable; allowing attempt", "err", err)
		return true
	}
	if !allowed {
		http.Error(w, "too many attempts; please slow down", http.StatusTooManyRequests)
		return false
	}
	return true
}

// clientIP returns the client's IP for rate limiting. It uses the direct remote
// address; behind a proxy or load balancer the real client IP must instead come
// from a trusted X-Forwarded-For header (see PULSE_TRUSTED_PROXY_CIDRS, Phase 7),
// or every client would share the proxy's address and one shared limit.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// normalizeUsername canonicalizes a username so lookups are case-insensitive
// and unaffected by surrounding whitespace.
func normalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// decodeCredentials reads a JSON {username, password} body, bounding its size
// and rejecting unknown fields. On failure it writes a 400 and returns false.
func decodeCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	var c credentials
	if err := dec.Decode(&c); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return credentials{}, false
	}
	return c, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
