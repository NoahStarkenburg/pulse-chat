package auth

import (
	"context"
	"testing"
)

func TestUserIDContext_Roundtrip(t *testing.T) {
	ctx := withUserID(context.Background(), "user-1")
	got, ok := UserIDFromContext(ctx)
	if !ok || got != "user-1" {
		t.Errorf("UserIDFromContext = (%q, %v), want (\"user-1\", true)", got, ok)
	}
}

func TestUserIDContext_AbsentWhenUnset(t *testing.T) {
	if _, ok := UserIDFromContext(context.Background()); ok {
		t.Error("UserIDFromContext reported a user on a bare context")
	}
}
