package web

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"arc42-trainings-admin/internal/config"
	"arc42-trainings-admin/internal/gh"
	"arc42-trainings-admin/internal/model"
)

//go:embed templates/*.gohtml static/*
var assets embed.FS

const dataPath = "_data/trainings.yml"
const schemaPath = "api/trainings.schema.json"

type Server struct {
	cfg      config.Config
	sessions *Sessions
	drafts   *Drafts
	oauth    gh.OAuth
	apiBase  string
	set      map[string]*template.Template
}

// pages lists the page templates that need their own template.Template, since
// html/template resolves {{define "content"}} globally: parsing every page
// into one *template.Template would let the last-parsed "content" block win
// for every page. Each entry here is parsed from layout + draftbar + the page
// file alone.
var pages = []string{
	"list.gohtml", "dateform.gohtml", "login.gohtml", "denied.gohtml", "error.gohtml",
	"propose.gohtml", "conflict.gohtml", "published.gohtml",
	"courselist.gohtml", "courseform.gohtml",
}

func NewServer(cfg config.Config, apiBase, publicURL string) (*Server, error) {
	set := map[string]*template.Template{}
	for _, p := range pages {
		t, err := template.New(p).Funcs(templateFuncs()).ParseFS(assets,
			"templates/layout.gohtml", "templates/draftbar.gohtml",
			"templates/_fields.gohtml", "templates/"+p)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		set[p] = t
	}
	// handleKeepalive renders draftbar.gohtml standalone, so it needs its own
	// entry parsed from just that file.
	draftbarOnly, err := template.New("draftbar.gohtml").Funcs(templateFuncs()).ParseFS(assets, "templates/draftbar.gohtml")
	if err != nil {
		return nil, fmt.Errorf("parse draftbar.gohtml: %w", err)
	}
	set["draftbar.gohtml"] = draftbarOnly

	return &Server{
		cfg:      cfg,
		sessions: NewSessions(cfg.SessionKey, cfg.Environment == "PRODUCTION"),
		drafts:   NewDrafts(),
		oauth: gh.OAuth{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Redirect:     publicURL + "/auth/callback",
		},
		apiBase: apiBase,
		set:     set,
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /static/", http.FileServerFS(assets))

	mux.HandleFunc("GET /auth/github", s.handleAuthStart)
	mux.HandleFunc("GET /auth/callback", s.handleAuthCallback)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)

	// Everything else requires a signed-in user with push permission.
	mux.Handle("GET /{$}", s.authed(s.handleList))
	mux.Handle("GET /dates/new", s.authed(s.handleDateForm))
	mux.Handle("GET /dates/{id}", s.authed(s.handleDateForm))
	mux.Handle("POST /dates/{id}", s.authed(s.handleDateSave))
	mux.Handle("POST /dates/{id}/delete", s.authed(s.handleDateDelete))
	mux.Handle("GET /courses", s.authed(s.handleCourseList))
	mux.Handle("GET /courses/new", s.authed(s.handleCourseForm))
	mux.Handle("GET /courses/{id}", s.authed(s.handleCourseForm))
	mux.Handle("POST /courses/new", s.authed(s.handleCourseSave))
	mux.Handle("POST /courses/{id}", s.authed(s.handleCourseSave))
	mux.Handle("GET /propose", s.authed(s.handlePropose))
	mux.Handle("POST /propose", s.authed(s.handleProposeSubmit))
	mux.Handle("POST /discard", s.authed(s.handleDiscard))
	mux.Handle("GET /keepalive", s.authed(s.handleKeepalive))
	return mux
}

// ctxKey scopes request-scoped values in future handlers.
type ctxKey int

const (
	ctxSession ctxKey = iota
	ctxClient
)

type authedFunc func(http.ResponseWriter, *http.Request, Session, *gh.Client)

// authed requires a valid session and builds a per-request GitHub client from
// the user's own token — the app never acts with a shared credential.
func (s *Server) authed(next authedFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.sessions.Get(r)
		if !ok {
			// A state-changing request from an expired session must not look
			// like it worked. The draft was keyed by the old session id, so the
			// edits are already gone — saying so is the honest answer, and a
			// 200 here would let "Open pull request" appear to succeed while
			// nothing was published.
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(http.StatusUnauthorized)
				s.render(w, "login.gohtml", map[string]any{
					"Title":       "Session expired",
					"NoticeTitle": "That change was not saved",
					"Notice": "Your session ended before it was submitted, and unpublished " +
						"edits live only for the length of a session, so any draft is gone. " +
						"Nothing was published and nothing in the repository changed.",
					"Bare": true,
				})
				return
			}
			s.render(w, "login.gohtml", map[string]any{"Title": "Sign in"})
			return
		}
		owner, name := s.cfg.Repo()
		client := gh.New(s.apiBase, owner, name, sess.Token)
		next(w, r, sess, client)
	})
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	t, ok := s.set[name]
	if !ok {
		log.Printf("render: unknown template %q", name)
		http.Error(w, "template missing", http.StatusInternalServerError)
		return
	}
	// Render into a buffer first. html/template streams, so executing straight
	// into the ResponseWriter means a mid-render error leaves a half-written
	// page already committed with a 200 — which is how a broken form silently
	// shipped a heading, one <select> and nothing else.
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "The page could not be rendered. This is a bug; see the server log.",
			http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func (s *Server) fail(w http.ResponseWriter, msg string, err error) {
	log.Printf("%s: %v", msg, err)
	w.WriteHeader(http.StatusInternalServerError)
	s.render(w, "error.gohtml", map[string]any{"Title": "Something went wrong", "Message": msg})
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// Deliberately `any`, not `string`. The templates are driven by
		// map[string]any, where a key the handler never set yields an untyped
		// nil — and a strictly-typed helper turns that omission into a
		// mid-render abort rather than an empty comparison.
		"eqStr": func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
		// codeToken travels with each course option so the browser can derive
		// the booking code without a round trip.
		"codeToken": func(courseID any) string { return model.CodeToken(fmt.Sprint(courseID)) },
		// has and join also take `any`: values reaching a shared partial go
		// through dict, which erases []string to interface{}, and a nil slice
		// is the normal case for a blank form.
		"has": func(list any, v string) bool {
			ss, _ := list.([]string)
			for _, item := range ss {
				if item == v {
					return true
				}
			}
			return false
		},
		// join renders a trainer list back into the "additional names" field.
		"join": func(list any, sep string) string {
			ss, _ := list.([]string)
			return strings.Join(ss, sep)
		},
		// dict lets one partial take several named values.
		"dict": func(pairs ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(pairs); i += 2 {
				m[fmt.Sprint(pairs[i])] = pairs[i+1]
			}
			return m
		},
	}
}
