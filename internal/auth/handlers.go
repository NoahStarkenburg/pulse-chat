package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
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

// Handlers serves the authentication HTTP endpoints.
type Handlers struct {
	users    *UserStore
	sessions *SessionStore
	logger   *slog.Logger
	secure   bool // set the cookie Secure flag (true behind HTTPS)
}

// NewHandlers constructs the auth HTTP handlers. secure should be true in
// production so the session cookie is only sent over HTTPS.
func NewHandlers(users *UserStore, sessions *SessionStore, logger *slog.Logger, secure bool) *Handlers {
	return &Handlers{users: users, sessions: sessions, logger: logger, secure: secure}
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

	user, err := h.users.Create(username, hash)
	if err != nil {
		if errors.Is(err, ErrUserExists) {
			http.Error(w, "username already taken", http.StatusConflict)
			return
		}
		h.logger.Error("creating user", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if !h.issueSessionCookie(w, user.ID) {
		return
	}
	writeJSON(w, http.StatusCreated, userResponse{ID: user.ID, Username: user.Username})
}

// Login validates credentials and issues a session cookie. Mounted on POST /login.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}

	username := normalizeUsername(creds.Username)
	user, err := h.users.ByUsername(username)
	if err != nil {
		// Verify against a dummy hash anyway so timing does not reveal whether
		// the username exists, and return the same error as a wrong password.
		_ = VerifyPassword(creds.Password, string(dummyPasswordHash))
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err := VerifyPassword(creds.Password, user.PasswordHash); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if !h.issueSessionCookie(w, user.ID) {
		return
	}
	writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Username: user.Username})
}

// Logout invalidates the session and clears the cookie. Mounted on POST /logout.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		h.sessions.Delete(cookie.Value)
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
	user, err := h.users.ByID(userID)
	if err != nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Username: user.Username})
}

// issueSessionCookie creates a session and sets the cookie. It returns false
// (after writing an error response) if the session could not be created.
func (h *Handlers) issueSessionCookie(w http.ResponseWriter, userID string) bool {
	token, err := h.sessions.Issue(userID)
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
