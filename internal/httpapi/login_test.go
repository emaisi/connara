package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestAdminSessionCookieSecurity(t *testing.T) {
	request := httptest.NewRequest("POST", "https://hub.example.test/api/auth/login", nil)
	recorder := httptest.NewRecorder()
	setAdminSessionCookie(recorder, request, "hub_admin_secret", time.Now().Add(time.Hour))
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
		t.Fatalf("insecure admin session cookie: %#v", cookies)
	}
}

func TestAdminSessionTokenAndDummyHash(t *testing.T) {
	if token := newAdminSessionToken(); !strings.HasPrefix(token, "hub_admin_") || len(token) < 40 {
		t.Fatalf("unexpected session token shape: %q", token)
	}
	if _, err := bcrypt.Cost([]byte(dummyPasswordHash)); err != nil {
		t.Fatalf("dummy hash must be a valid bcrypt hash: %v", err)
	}
}
