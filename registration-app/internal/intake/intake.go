// Package intake turns a form submission into a Decision: accept it, drop it
// silently, or reject it back to the person. It knows the form's field names
// in both languages; nothing downstream does.
package intake

import (
	"context"
	"crypto/rand"
	"net/mail"
	"net/netip"
	"net/url"
	"strings"

	"arc42-registration/internal/feed"
)

type Registration struct {
	ID   string // set by the caller once accepted
	Lang string // "de" or "en"

	LastName  string
	FirstName string
	Email     string   // as typed
	Emails    []string // parsed, lower-cased; Emails[0] receives the registrant mail
	Code      string   // booking code, or Other

	ParticipantLast  string
	ParticipantFirst string
	ParticipantEmail string

	Billing  string
	Comments string

	Via        string
	FormSource string
}

// Other is the value of the form's "Sonstige"/"other" option.
const Other = "sonstige"

type Outcome int

const (
	Accept Outcome = iota
	Drop           // bot signal: no mail, the success page anyway
	Reject         // a person's mistake: the fail page
)

type Decision struct {
	Outcome Outcome
	Reason  string // for the log; never shown
	Reg     Registration
	Entry   feed.Entry // zero for Other or when the feed is down
	Found   bool       // Entry is meaningful
	Hints   []string
}

type Input struct {
	Form   url.Values
	Origin string
	IP     string
}

type Lookuper interface {
	Lookup(ctx context.Context, code string) (feed.Entry, feed.Result)
}

type Checker struct {
	Feed           Lookuper
	Limiter        *Limiter
	AllowedOrigins []string
}

// The form's field names. German and English forms name the visible fields
// differently, and Formspark used those names as labels, so they stay as they
// are; this is the one place that knows both spellings.
var fieldNames = map[string][]string{
	"last":     {"Nachname", "last name"},
	"first":    {"Vorname", "first name"},
	"email":    {"Email"},
	"code":     {"Kurs"},
	"p_last":   {"NachnameTN"},
	"p_first":  {"VornameTN"},
	"p_email":  {"EmailTN"},
	"billing":  {"Rechnungsadresse", "Billing address"},
	"comments": {"Bemerkungen", "Comments"},
}

var maxLen = map[string]int{
	"last": 200, "first": 200, "email": 320, "code": 64,
	"p_last": 200, "p_first": 200, "p_email": 320,
	"billing": 1000, "comments": 4000,
}

func get(f url.Values, key string) string {
	for _, name := range fieldNames[key] {
		if v := strings.TrimSpace(f.Get(name)); v != "" {
			return v
		}
	}
	return ""
}

func (c *Checker) Check(ctx context.Context, in Input) Decision {
	f := in.Form
	r := Registration{
		Lang:             "de",
		LastName:         get(f, "last"),
		FirstName:        get(f, "first"),
		Email:            get(f, "email"),
		Code:             get(f, "code"),
		ParticipantLast:  get(f, "p_last"),
		ParticipantFirst: get(f, "p_first"),
		ParticipantEmail: get(f, "p_email"),
		Billing:          get(f, "billing"),
		Comments:         get(f, "comments"),
		Via:              clip(f.Get("via"), 100),
		FormSource:       clip(f.Get("form_source"), 300),
	}
	if f.Get("language") == "en" {
		r.Lang = "en"
	}
	d := Decision{Reg: r}
	drop := func(why string) Decision { d.Outcome, d.Reason = Drop, why; return d }
	reject := func(why string) Decision { d.Outcome, d.Reason = Reject, why; return d }

	for key, max := range maxLen {
		if len([]rune(get(f, key))) > max {
			return reject("too long: " + key)
		}
	}
	// "null" is what a browser sends from a page with referrer policy
	// no-referrer (privacy extensions, hardened browsers). Dropping it would
	// lose real registrations and stop no script, which can omit the header.
	if in.Origin != "" && in.Origin != "null" && !contains(c.AllowedOrigins, strings.ToLower(in.Origin)) {
		return drop("origin " + in.Origin)
	}
	if f.Get("_gotcha") != "" || f.Get("company_website") != "" {
		return drop("honeypot")
	}
	if c.Limiter != nil && !c.Limiter.Allow(ipKey(in.IP)) {
		return drop("rate limit")
	}
	if r.LastName == "" || r.Email == "" || r.Code == "" || r.Billing == "" {
		return reject("required field missing")
	}
	emails, ok := parseEmails(r.Email)
	if !ok {
		return reject("email syntax")
	}
	d.Reg.Emails = emails

	if r.Code != Other {
		entry, res := c.Feed.Lookup(ctx, r.Code)
		switch res {
		case feed.Unknown:
			return drop("unknown code " + r.Code)
		case feed.Closed:
			d.Hints = append(d.Hints, HintClosed)
		case feed.Unavailable:
			d.Hints = append(d.Hints, HintFeedDown)
		}
		d.Entry, d.Found = entry, res != feed.Unavailable
		if entry.Status == "waitlist" {
			d.Hints = append(d.Hints, HintWaitlist)
		}
	}
	d.Hints = append(d.Hints, hintsFor(d.Reg)...)
	d.Outcome = Accept
	return d
}

// parseEmails accepts one or more comma-separated bare addresses (the form's
// email input has the `multiple` attribute). A display name, a missing dot in
// the domain, or anything net/mail cannot parse fails the whole field.
func parseEmails(s string) ([]string, bool) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		a, err := mail.ParseAddress(part)
		if err != nil || a.Name != "" || a.Address != part {
			return nil, false
		}
		_, domain, _ := strings.Cut(a.Address, "@")
		if !strings.Contains(domain, ".") || strings.HasSuffix(domain, ".") {
			return nil, false
		}
		out = append(out, strings.ToLower(a.Address))
	}
	return out, len(out) > 0
}

// ipKey is the rate-limit key for a client address: the address itself for
// IPv4, its /64 prefix for IPv6, because one IPv6 client typically controls a
// whole /64.
func ipKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.Is4() || addr.Is4In6() {
		return ip
	}
	p, err := addr.Prefix(64)
	if err != nil {
		return ip
	}
	return p.String()
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// idAlphabet leaves out 0/O, 1/I/L: the id is read aloud on the phone.
const idAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// NewID returns a registration id such as "R-7F3KQ". 31^5 is about 29
// million; at a few registrations a week a collision is not a concern, and
// the id only has to tell two mails in one inbox apart.
func NewID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return "R-" + string(b)
}
