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
		w.WriteHeader(http.StatusForbidden)
		s.render(w, "denied.gohtml", map[string]any{
			"Title": "No access", "Login": login, "Repo": s.cfg.GitHubRepo,
		})
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
