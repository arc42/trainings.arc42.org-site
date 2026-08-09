package web

import (
	"net/http"
	"time"

	"github.com/gorilla/securecookie"
)

const sessionCookieName = "arc42_admin_session"
const sessionTTL = 8 * time.Hour

// Session is what the encrypted cookie carries. The GitHub token lives here and
// nowhere else — the app stores no credentials server-side.
type Session struct {
	ID    string
	Login string
	Token string
}

type Sessions struct {
	codec  *securecookie.SecureCookie
	secure bool
}

// NewSessions derives the cookie codec from SESSION_KEY. secure=false is only
// for local HTTP development.
func NewSessions(key string, secure bool) *Sessions {
	hashKey := []byte(key)
	blockKey := []byte(key)[:32]
	sc := securecookie.New(hashKey, blockKey)
	sc.MaxAge(int(sessionTTL.Seconds()))
	return &Sessions{codec: sc, secure: secure}
}

func (s *Sessions) Set(w http.ResponseWriter, sess Session) error {
	encoded, err := s.codec.Encode(sessionCookieName, sess)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return nil
}

func (s *Sessions) Get(r *http.Request) (Session, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return Session{}, false
	}
	var sess Session
	if err := s.codec.Decode(sessionCookieName, c.Value, &sess); err != nil {
		return Session{}, false
	}
	return sess, sess.Token != ""
}

func (s *Sessions) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}
