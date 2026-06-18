package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireAuth_NoCookieIs401(t *testing.T) {
	h := RequireAuth(NewSessionStore())(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireAuth_InvalidTokenIs401(t *testing.T) {
	h := RequireAuth(NewSessionStore())(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "bogus"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireAuth_ValidTokenPassesAndInjectsUserID(t *testing.T) {
	sessions := NewSessionStore()
	token, _ := sessions.Issue("user-1")

	var gotID string
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID, gotOK = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	RequireAuth(sessions)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !gotOK || gotID != "user-1" {
		t.Errorf("injected user ID = (%q, %v), want (\"user-1\", true)", gotID, gotOK)
	}
}
