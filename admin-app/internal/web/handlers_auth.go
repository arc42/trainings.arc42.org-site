package web

import (
	"net/http"

	"arc42-trainings-admin/internal/gh"
)

const stateCookieName = "arc42_admin_state"

func (s *Server) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	state := newID()
	http.SetCookie(w, &http.Cookie{
		Name: stateCookieName, Value: state, Path: "/", HttpOnly: true,
		Secure: s.cfg.Environment == "PRODUCTION", SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	http.Redirect(w, r, s.oauth.AuthURL(state), http.StatusFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	// CSRF: the state in the URL must match the one we set before redirecting.
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	token, err := s.oauth.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		s.fail(w, "could not complete GitHub sign-in", err)
		return
	}
	owner, name := s.cfg.Repo()
	login, canPush, err := gh.New(s.apiBase, owner, name, token).Viewer(r.Context())
	if err != nil {
		s.fail(w, "could not read your GitHub permissions", err)
		return
	}
	if !canPush {
		s.renderDenied(w, login)
		return
	}
	if err := s.sessions.Set(w, Session{ID: newID(), Login: login, Token: token}); err != nil {
		s.fail(w, "could not start your session", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess, ok := s.sessions.Get(r); ok {
		s.drafts.Discard(sess.ID)
	}
	s.sessions.Clear(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleKeepalive(w http.ResponseWriter, r *http.Request, sess Session, _ *gh.Client) {
	// Fires only while a draft is dirty, so fly's auto-stop cannot discard work
	// mid-session. Returns the badge markup htmx swaps in.
	d, ok := s.drafts.Get(sess.ID)
	if !ok || !d.Dirty() {
		_, _ = w.Write([]byte(""))
		return
	}
	s.render(w, "draftbar.gohtml", map[string]any{"Draft": d})
}

// renderDenied explains, to someone who is not a maintainer, why there is
// nothing here for them — and points at what they were probably looking for.
//
// This is the most-seen screen in the app: everyone who follows the site's
// "Maintainers" footer link out of curiosity lands here, and being turned away
// is the expected outcome for all but two people. It stays a 403 rendered in
// place rather than a redirect, because the one recoverable case — a maintainer
// signed into the wrong GitHub account — depends on naming the account, and a
// redirect would throw that away.
func (s *Server) renderDenied(w http.ResponseWriter, login string) {
	w.WriteHeader(http.StatusForbidden)
	s.render(w, "denied.gohtml", map[string]any{
		"Title": "For arc42 maintainers",
		"Login": login,
		"Repo":  s.cfg.GitHubRepo,
		"Bare":  true, // no nav: every link in it is a dead end from here
	})
}
