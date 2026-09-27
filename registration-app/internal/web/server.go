// Package web is the HTTP surface: POST /submit takes the form, GET /confirm
// shows a page with a button, POST /confirm confirms. Nothing here keeps
// state between requests.
package web

import (
	"context"
	"embed"
	"html/template"
	"log"
	"net"
	"net/http"
	"time"

	"arc42-registration/internal/config"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/send"
	"arc42-registration/internal/token"
)

//go:embed pages/*.html
var pageFiles embed.FS

var pages = template.Must(template.ParseFS(pageFiles, "pages/*.html"))

type Deps struct {
	Cfg     config.Config
	Checker *intake.Checker
	Feed    intake.Lookuper
	Sealer  *token.Sealer
	Sender  send.Sender
	NewID   func() string
	Log     *log.Logger
	// RecipientLimiter caps confirmation mails per registrant address, so a
	// script rotating IPs cannot make the service mail one inbox without
	// end. nil means no cap. The back office still gets every submission.
	RecipientLimiter *intake.Limiter
	// CorrectionLimiter counts corrections per registration id. The token on
	// the page is stateless and can be replayed; this is what holds a
	// registration to maxCorrections. In memory: a restart forgets, and the
	// correction window bounds what that can cost.
	CorrectionLimiter *intake.Limiter
}

type Server struct{ d Deps }

func New(d Deps) *Server {
	if d.NewID == nil {
		d.NewID = intake.NewID
	}
	return &Server{d: d}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("arc42 registration service. The form is on https://trainings.arc42.org/anmeldung/.\n"))
	})
	mux.HandleFunc("POST /submit", s.handleSubmit)
	mux.HandleFunc("GET /confirm", s.handleConfirmPage)
	mux.HandleFunc("POST /confirm", s.handleConfirm)
	mux.HandleFunc("POST /correct", s.handleCorrect)
	return mux
}

// sendTimeout bounds one mail send including its retry, so a Mailjet outage
// turns into the fail page within seconds rather than a hanging browser.
const sendTimeout = 15 * time.Second

func (s *Server) send(ctx context.Context, m send.Message) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return s.d.Sender.Send(ctx, m)
}

// Pages on the site the service redirects to, per language.
var sitePages = map[string]map[string]string{
	"success":   {"de": "/anmeldung-erfolg/", "en": "/registration-success/"},
	"fail":      {"de": "/anmeldung-fail/", "en": "/registration-fail/"},
	"confirmed": {"de": "/anmeldung-bestaetigt/", "en": "/registration-confirmed/"},
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, page, lang string) {
	if lang != "en" {
		lang = "de"
	}
	http.Redirect(w, r, s.d.Cfg.SiteURL+sitePages[page][lang], http.StatusSeeOther)
}

// clientIP prefers Fly-Client-IP, which fly's proxy sets to the real client
// address. It is only trustworthy behind fly; locally RemoteAddr is used.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("Fly-Client-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// noStore keeps the confirm pages out of caches and, via no-referrer, keeps
// the token in their URL from leaking to any page they link to.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}
