package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func testSessions() *Sessions {
	return NewSessions("0123456789abcdef0123456789abcdef", false)
}

func TestSessionRoundTrip(t *testing.T) {
	s := testSessions()
	rec := httptest.NewRecorder()
	if err := s.Set(rec, Session{ID: "sid", Login: "gernotstarke", Token: "tok"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	got, ok := s.Get(req)
	if !ok || got.Login != "gernotstarke" || got.Token != "tok" {
		t.Fatalf("Get = %+v, %v", got, ok)
	}
}

func TestSessionCookieIsHardened(t *testing.T) {
	rec := httptest.NewRecorder()
	_ = testSessions().Set(rec, Session{ID: "sid", Login: "x", Token: "t"})
	c := rec.Result().Cookies()[0]
	if !c.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Error("cookie is not SameSite=Lax")
	}
	if c.MaxAge <= 0 || c.MaxAge > 8*60*60 {
		t.Errorf("cookie MaxAge = %d, want a positive value up to 8h", c.MaxAge)
	}
}

func TestSessionRejectsTamperedCookie(t *testing.T) {
	s := testSessions()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-real-value"})
	if _, ok := s.Get(req); ok {
		t.Error("tampered cookie was accepted")
	}
}
