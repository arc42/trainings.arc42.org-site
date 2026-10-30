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
	"unicode"

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

	// Participants are the people attending, in the order of the form. Empty
	// when the registrant left the participant block empty: then the
	// registrant attends. Each one becomes a row in the back-office sheet.
	Participants []Participant

	Company  string // Firma, once per registration
	Billing  string
	Comments string

	// How the registrant heard about the course: a dropdown value in the
	// form's language ("Buch", "Conference", ...) and optional free text.
	// Both optional; for the back office only.
	HeardVia, HeardViaDetail string

	Via        string
	FormSource string
}

// Participant is one attending person. Salutation is "Frau", "Herr" or
// empty, in both languages: the option value reaches the back office, the
// English form only labels it "Ms"/"Mr".
type Participant struct {
	Salutation, Last, First, Email string
}

// MaxParticipants is how many participant blocks the form's "+ weitere
// Person" button adds at most. More arrives only from a script.
const MaxParticipants = 10

// salutations are the option values of the AnredeTN select. Anything else is
// discarded rather than rejected: it is only ever wrong from a script.
var salutations = map[string]bool{"Frau": true, "Herr": true}

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
	"last":  {"Nachname", "last name"},
	"first": {"Vorname", "first name"},
	"email": {"Email"},
	"code":  {"Kurs"},
	// The participant fields repeat: every block the form's "+ weitere
	// Person" button adds carries the same four names, and the browser posts
	// them in order, empty ones included, so the n-th values belong
	// together. One block posts exactly what the form posted before.
	"p_salutation": {"AnredeTN"},
	"p_last":       {"NachnameTN"},
	"p_first":      {"VornameTN"},
	"p_email":      {"EmailTN"},
	"company":      {"Firma", "Company"},
	"billing":      {"Rechnungsadresse", "Billing address"},
	"comments":     {"Bemerkungen", "Comments"},
	// Names readable as they are: Formspark lists fields by name in its
	// notification, and the live forms still post there until go-live.
	"heard":        {"Aufmerksam durch", "Heard via"},
	"heard_detail": {"Aufmerksam durch (Details)", "Heard via (details)"},
	// Hidden, filled by the page's script on load; empty from a bot that
	// posts without running it. Same name in both forms. One spam point.
	"js_ok": {"js_ok"},
}

var maxLen = map[string]int{
	"last": 200, "first": 200, "email": 320, "code": 64,
	"p_salutation": 10, "p_last": 200, "p_first": 200, "p_email": 320,
	"company": 200, "billing": 1000, "comments": 4000,
	"heard": 60, "heard_detail": 200,
}

// MaxFormBytes is the body cap for /submit, derived from maxLen so the two
// cannot drift. The field limits are the rule people meet, as a readable
// "too long" on the fail page; the cap only stops what no legal form can be.
// A 4-byte character form-encodes to 12 bytes; 8 KB covers field names, the
// short unlimited fields (via, form_source, language) and the honeypots.
// A fixed 32 KB used to cut in first: a Chinese comment inside its
// 4000-character limit got a bare 413.
// The participant fields count MaxParticipants times.
func MaxFormBytes() int64 {
	n := 0
	for key, max := range maxLen {
		n += max * repeats(key)
	}
	return int64(n*12 + 8<<10)
}

func repeats(key string) int {
	if strings.HasPrefix(key, "p_") {
		return MaxParticipants
	}
	return 1
}

func get(f url.Values, key string) string {
	for _, name := range fieldNames[key] {
		if v := strings.TrimSpace(f.Get(name)); v != "" {
			return v
		}
	}
	return ""
}

// getAll returns every value of a field, trimmed, in the order posted.
func getAll(f url.Values, key string) []string {
	var out []string
	for _, name := range fieldNames[key] {
		for _, v := range f[name] {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

// participants pairs the n-th values of the four participant fields. A
// block left empty is no participant; a salutation alone does not make one.
func participants(f url.Values) []Participant {
	sal, last, first, email := getAll(f, "p_salutation"), getAll(f, "p_last"), getAll(f, "p_first"), getAll(f, "p_email")
	n := max(len(sal), len(last), len(first), len(email))
	var out []Participant
	for i := 0; i < n; i++ {
		p := Participant{Salutation: at(sal, i), Last: at(last, i), First: at(first, i), Email: at(email, i)}
		if p.Last == "" && p.First == "" && p.Email == "" {
			continue
		}
		if !salutations[p.Salutation] {
			p.Salutation = ""
		}
		out = append(out, p)
	}
	return out
}

func at(vs []string, i int) string {
	if i < len(vs) {
		return vs[i]
	}
	return ""
}

func (c *Checker) Check(ctx context.Context, in Input) Decision {
	f := in.Form
	r := Registration{
		Lang:           "de",
		LastName:       get(f, "last"),
		FirstName:      get(f, "first"),
		Email:          get(f, "email"),
		Code:           get(f, "code"),
		Participants:   participants(f),
		Company:        get(f, "company"),
		Billing:        get(f, "billing"),
		Comments:       get(f, "comments"),
		HeardVia:       get(f, "heard"),
		HeardViaDetail: get(f, "heard_detail"),
		Via:            clip(f.Get("via"), 100),
		FormSource:     clip(f.Get("form_source"), 300),
	}
	if f.Get("language") == "en" {
		r.Lang = "en"
	}
	d := Decision{Reg: r}
	drop := func(why string) Decision { d.Outcome, d.Reason = Drop, why; return d }
	reject := func(why string) Decision { d.Outcome, d.Reason = Reject, why; return d }

	for key, max := range maxLen {
		vs := getAll(f, key)
		if len(vs) > repeats(key) {
			return reject("too many values: " + key)
		}
		for _, v := range vs {
			if len([]rune(v)) > max {
				return reject("too long: " + key)
			}
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

	// No real booking code contains a control character. Checked before the
	// feed, because while the feed is down every code passes unchecked, and
	// this one would end up in the back-office subject line.
	if strings.IndexFunc(r.Code, unicode.IsControl) >= 0 {
		return drop("control character in code")
	}
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

	// Rejected, not dropped: a person who pasted a link learns why on the
	// fail page and sends again without it. A bot that learns to leave the
	// link out still has to pass the score below.
	if hasLink(r.Comments) || hasLink(r.Billing) || hasLink(r.Company) {
		return reject("link in comments, billing or company")
	}
	for _, n := range r.names() {
		if hasLink(n) {
			return reject("link in a name field")
		}
	}

	sig := signals(d.Reg, get(f, "js_ok") != "")
	if len(sig) >= dropAt {
		return drop("score: " + strings.Join(sig, ","))
	}
	d.Hints = append(d.Hints, sig...)
	d.Hints = append(d.Hints, hintsFor(d.Reg)...)
	d.Outcome = Accept
	return d
}

// names are every name field: the registrant's and each participant's.
func (r Registration) names() []string {
	ns := []string{r.LastName, r.FirstName}
	for _, p := range r.Participants {
		ns = append(ns, p.Last, p.First)
	}
	return ns
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

// IPKey exposes ipKey for the correction endpoint, which shares the limiter.
func IPKey(ip string) string { return ipKey(ip) }

// ParseAddress accepts exactly one bare address, by the same rules as the
// form's email field. It returns it lower-cased.
func ParseAddress(s string) (string, bool) {
	emails, ok := parseEmails(strings.TrimSpace(s))
	if !ok || len(emails) != 1 {
		return "", false
	}
	return emails[0], true
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
