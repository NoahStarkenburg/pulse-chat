package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer wires the auth routes exactly as main does and returns a running
// httptest server.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	users := NewUserStore()
	sessions := NewSessionStore()
	h := NewHandlers(users, sessions, testLogger(), false)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /signup", h.Signup)
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("POST /logout", h.Logout)
	mux.Handle("GET /me", RequireAuth(sessions)(http.HandlerFunc(h.Me)))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newClient returns an HTTP client with a cookie jar so the session cookie
// flows between requests like a browser.
func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar}
}

func do(t *testing.T, c *http.Client, method, url, body string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func post(t *testing.T, c *http.Client, url, body string) *http.Response {
	t.Helper()
	return do(t, c, http.MethodPost, url, body)
}

func get(t *testing.T, c *http.Client, url string) *http.Response {
	t.Helper()
	return do(t, c, http.MethodGet, url, "")
}

func TestAuth_SignupLoginMeLogoutFlow(t *testing.T) {
	srv := newTestServer(t)
	c := newClient(t)

	resp := post(t, c, srv.URL+"/signup", `{"username":"alice","password":"password123"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup status = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	// /me uses the cookie the jar stored from signup.
	resp = get(t, c, srv.URL+"/me")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status = %d, want 200", resp.StatusCode)
	}
	var who userResponse
	_ = json.NewDecoder(resp.Body).Decode(&who)
	resp.Body.Close()
	if who.Username != "alice" {
		t.Errorf("me username = %q, want alice", who.Username)
	}

	resp = post(t, c, srv.URL+"/logout", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()

	resp = get(t, c, srv.URL+"/me")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("me after logout = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAuth_LoginFromSeparateClient(t *testing.T) {
	srv := newTestServer(t)
	resp := post(t, newClient(t), srv.URL+"/signup", `{"username":"bob","password":"password123"}`)
	resp.Body.Close()

	c := newClient(t) // fresh jar, no cookie yet
	resp = post(t, c, srv.URL+"/login", `{"username":"bob","password":"password123"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = get(t, c, srv.URL+"/me")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("me after login = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAuth_DuplicateSignupIsConflict(t *testing.T) {
	srv := newTestServer(t)
	resp := post(t, newClient(t), srv.URL+"/signup", `{"username":"carol","password":"password123"}`)
	resp.Body.Close()
	resp = post(t, newClient(t), srv.URL+"/signup", `{"username":"carol","password":"password123"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate signup = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAuth_WrongPasswordIsUnauthorized(t *testing.T) {
	srv := newTestServer(t)
	resp := post(t, newClient(t), srv.URL+"/signup", `{"username":"dave","password":"password123"}`)
	resp.Body.Close()
	resp = post(t, newClient(t), srv.URL+"/login", `{"username":"dave","password":"wrongpassword"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAuth_UnknownUserLoginIsUnauthorized(t *testing.T) {
	srv := newTestServer(t)
	resp := post(t, newClient(t), srv.URL+"/login", `{"username":"ghost","password":"password123"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown user login = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAuth_ShortPasswordRejected(t *testing.T) {
	srv := newTestServer(t)
	resp := post(t, newClient(t), srv.URL+"/signup", `{"username":"erin","password":"short"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("short password = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAuth_SessionCookieIsHardened(t *testing.T) {
	srv := newTestServer(t)
	resp := post(t, newClient(t), srv.URL+"/signup", `{"username":"frank","password":"password123"}`)
	defer resp.Body.Close()

	var cookie *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == SessionCookieName {
			cookie = ck
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie set on signup")
	}
	if !cookie.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Error("session cookie SameSite is not Lax")
	}
}
