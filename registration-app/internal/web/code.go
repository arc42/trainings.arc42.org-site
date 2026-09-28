package web

import (
	"net/http"

	"arc42-registration/internal/token"
)

// maxCodeFailures is how many wrong codes a registration gets before the
// field locks and only the link in the mail confirms. Six digits are a
// million possibilities; five tries make guessing hopeless.
const maxCodeFailures = 5

// handleCode confirms with the 6-digit code from the registrant mail,
// typed on the "please confirm" page that stayed open, so nobody needs a
// second browser window. It also sidesteps corporate link scanners: a
// scanner can open a link, but it cannot type a code.
//
// The page's own token (PurposeCorrect) says which registration and which
// address; the code is recomputed from them (token.Sealer.Code) and compared.
// Wrong tries are counted in the store. Without the store they cannot be
// counted, so the code is refused and the link, which needs no counting,
// is the way.
func (s *Server) handleCode(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, token.ErrInvalid)
		return
	}
	c, err := s.d.Sealer.Open(r.PostForm.Get("t"))
	if err == nil && c.Purpose != token.PurposeCorrect {
		err = token.ErrInvalid
	}
	if err != nil {
		s.errorPage(w, err)
		return
	}
	ctx := r.Context()
	if s.d.Store == nil {
		s.sentPageNote(w, c, c.Email, false, "unavailable")
		return
	}
	n, err := s.d.Store.Failures(ctx, c.ID)
	if err != nil {
		s.d.Log.Printf("code %s: store unavailable: %v", c.ID, err)
		s.sentPageNote(w, c, c.Email, false, "unavailable")
		return
	}
	if n >= maxCodeFailures {
		s.sentPageNote(w, c, c.Email, false, "locked")
		return
	}
	// A dropped submission (a bot) gets the same page and counting as a
	// person, and no code ever confirms it.
	if c.Dropped != 1 && s.d.Sealer.CheckCode(c.ID, c.Email, r.PostForm.Get("code")) {
		c.Purpose = token.PurposeConfirm
		s.confirm(w, r, c, "code")
		return
	}
	n, err = s.d.Store.Failure(ctx, c.ID, s.d.Now())
	if err != nil {
		s.d.Log.Printf("code %s: store unavailable: %v", c.ID, err)
		s.sentPageNote(w, c, c.Email, false, "unavailable")
		return
	}
	if c.Dropped != 1 {
		s.d.Log.Printf("code %s: wrong code (%d of %d)", c.ID, n, maxCodeFailures)
	}
	note := "wrong"
	if n >= maxCodeFailures {
		note = "locked"
	}
	s.sentPageNote(w, c, c.Email, false, note)
}
