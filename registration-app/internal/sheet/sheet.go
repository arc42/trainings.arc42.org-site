// Package sheet appends confirmed registrations to the back office's Google
// Sheet "arc42-neue-Anmeldungen", one row per participant (spec 2026-10-25,
// section 5). Peter and Gernot copy each new row from there into the sheet
// of the course date by hand; the sheet's own header says so.
//
// Never on the critical path: the confirm handler appends after the mail is
// out, in the background, and the weekly summary run retries what failed.
// A row that went in twice is the accepted failure (the id column shows it);
// a registration missing from the sheet is listed in the summary.
//
// Standard library only, as the rest of the service: the service-account
// JWT is signed here (RS256) and exchanged for an access token at Google's
// token endpoint, then the rows go to the Sheets API's values.append.
package sheet

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the production image is FROM scratch and has no zoneinfo

	"arc42-registration/internal/intake"
	"arc42-registration/internal/store"
)

// columns is the sheet's layout, A to L, as agreed with the back office on
// 30 Oct 2026. Changing the sheet's columns changes this list and nothing
// else. E and J are the back office's own: the app leaves them empty. K and
// L are not in the copied part: they tell which date's sheet a row belongs
// to and tie it to its BESTÄTIGT mail.
var columns = []struct {
	header string
	value  func(row) any
}{
	{"Anrede", func(r row) any { return r.p.Salutation }},
	{"Name", func(r row) any { return r.p.Last }},
	{"Vorname", func(r row) any { return r.p.First }},
	{"Firma", func(r row) any { return r.rec.Reg.Company }},
	{"Infos nach Einladung", func(row) any { return "" }},
	{"email Teilnehmer", func(r row) any { return r.p.Email }},
	{"email Sekretariat", func(r row) any { return r.secretary }},
	{"Datum der Anmeldung", func(r row) any { return r.rec.CreatedAt.In(berlin).Format("02.01.2006") }},
	// A number, so the sheet can add it up; empty when not known.
	{"Preis", func(r row) any {
		if r.rec.Price > 0 {
			return r.rec.Price
		}
		return ""
	}},
	{"in echtes Termin-Sheet übernommen von", func(row) any { return "" }},
	{"Kurs", func(r row) any {
		if r.rec.Reg.Code == intake.Other {
			return "Sonstige"
		}
		return r.rec.Reg.Code
	}},
	{"Anmeldung-ID", func(r row) any { return r.rec.Reg.ID }},
}

// Headers is the header row the layout expects, for the README and tests.
func Headers() []string {
	h := make([]string, len(columns))
	for i, c := range columns {
		h[i] = c.header
	}
	return h
}

type row struct {
	rec       store.Record
	p         intake.Participant
	secretary string // the registrant's address, when they registered someone else
}

// Rows is the registration as sheet rows, one per participant.
//
// The registrant is the "Sekretariat" (someone registering for others: HR,
// an assistant) exactly when a participant is someone else. Without any
// participant block the registrant attends and is the one row. A
// participant block that repeats the registrant (same name and no other
// address, or the same address) is the registrant attending among others.
func Rows(rec store.Record) [][]any {
	g := rec.Reg
	var rs []row
	if len(g.Participants) == 0 {
		rs = []row{{rec: rec, p: intake.Participant{Last: g.LastName, First: g.FirstName, Email: g.Email}}}
	}
	for _, p := range g.Participants {
		if isRegistrant(g, p) {
			if p.Email == "" {
				p.Email = g.Email
			}
			rs = append(rs, row{rec: rec, p: p})
		} else {
			rs = append(rs, row{rec: rec, p: p, secretary: g.Email})
		}
	}
	out := make([][]any, len(rs))
	for i, r := range rs {
		out[i] = make([]any, len(columns))
		for j, c := range columns {
			out[i][j] = c.value(r)
		}
	}
	return out
}

func isRegistrant(g intake.Registration, p intake.Participant) bool {
	if p.Email != "" {
		for _, e := range g.Emails {
			if strings.EqualFold(e, p.Email) {
				return true
			}
		}
		return false
	}
	return strings.EqualFold(p.Last, g.LastName) && strings.EqualFold(p.First, g.FirstName)
}

// berlin is the back office's clock: "Datum der Anmeldung" is its date.
var berlin = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Berlin"); err == nil {
		return loc
	}
	return time.UTC
}()

const scope = "https://www.googleapis.com/auth/spreadsheets"

// Sheet appends to one tab of one spreadsheet as a service account. The
// spreadsheet is shared with the account's address as editor; the account
// has no other rights anywhere.
type Sheet struct {
	ID, Tab string
	// API and TokenURL are Google's; tests point them at a fake.
	API      string
	TokenURL string
	HTTP     *http.Client
	Now      func() time.Time

	email string
	key   *rsa.PrivateKey

	mu      sync.Mutex
	token   string
	expires time.Time
}

// New reads the service account's JSON key as Google hands it out
// (GOOGLE_SA_KEY). It fails on anything that would only fail at the first
// confirmation otherwise.
func New(keyJSON []byte, id, tab string) (*Sheet, error) {
	var k struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal(keyJSON, &k); err != nil {
		return nil, fmt.Errorf("sheet: GOOGLE_SA_KEY is not JSON: %w", err)
	}
	if k.Type != "service_account" || k.ClientEmail == "" {
		return nil, errors.New("sheet: GOOGLE_SA_KEY is not a service account key (type service_account, client_email)")
	}
	block, _ := pem.Decode([]byte(k.PrivateKey))
	if block == nil {
		return nil, errors.New("sheet: GOOGLE_SA_KEY has no PEM private_key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("sheet: private_key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("sheet: private_key is not RSA")
	}
	if id == "" || tab == "" {
		return nil, errors.New("sheet: spreadsheet id and tab are required")
	}
	tokenURL := k.TokenURI
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}
	return &Sheet{
		ID: id, Tab: tab, API: "https://sheets.googleapis.com", TokenURL: tokenURL,
		HTTP: &http.Client{Timeout: 10 * time.Second}, Now: time.Now,
		email: k.ClientEmail, key: key,
	}, nil
}

// Email is the service account's address, for the start-up log line: it is
// the address the spreadsheet must be shared with.
func (s *Sheet) Email() string { return s.email }

// Append adds the registration's rows below the last row of the tab, in one
// request, so a registration is in the sheet completely or not at all.
// RAW: nothing is parsed as a formula or a date. A name typed as
// "=HYPERLINK(...)" stays text.
//
// A 401 is retried once with a fresh token: an access token can lapse
// between the cache check and Google reading it, and that alone is no
// reason to alarm anybody. A refusal that needs a person comes back as
// *Refused; everything else (network, timeout, 5xx, 429) is transient and
// left to the summary run's retry.
func (s *Sheet) Append(ctx context.Context, rec store.Record) error {
	return s.retried(ctx, s.Tab, len(columns), Rows(rec))
}

// DeliveryTab is the tab Brevo's delivery failures go to (spec 2026-10-30),
// a second tab of the same spreadsheet. Its name is fixed: the back office
// creates it once, and a missing tab is a 400, mailed like any refusal.
const DeliveryTab = "Zustellung"

// Delivery is one mail Brevo could not deliver, as a row of DeliveryTab.
type Delivery struct {
	At   time.Time
	Kind string // KindRegistrant or KindBackoffice
	// The registration the mail belonged to; empty for a back-office mail
	// that belongs to none (the summary, the sheet alert).
	ID, Code, Last, First string
	Email                 string // the recipient
	Event, Reason         string // Brevo's event name and the remote answer
}

const (
	KindRegistrant = "Anmeldung"
	KindBackoffice = "Back-Office"
)

// deliveryColumns is the layout of DeliveryTab, A to I.
var deliveryColumns = []struct {
	header string
	value  func(Delivery) any
}{
	{"Zeit", func(d Delivery) any { return d.At.In(berlin).Format("02.01.2006 15:04") }},
	{"Typ", func(d Delivery) any { return d.Kind }},
	{"Anmeldung-ID", func(d Delivery) any { return d.ID }},
	{"Kurs", func(d Delivery) any {
		if d.Code == intake.Other {
			return "Sonstige"
		}
		return d.Code
	}},
	{"Name", func(d Delivery) any { return d.Last }},
	{"Vorname", func(d Delivery) any { return d.First }},
	{"E-Mail", func(d Delivery) any { return d.Email }},
	{"Ereignis", func(d Delivery) any { return d.Event }},
	{"Grund", func(d Delivery) any { return d.Reason }},
}

// DeliveryHeaders is the header row DeliveryTab expects, for the README
// and tests.
func DeliveryHeaders() []string {
	h := make([]string, len(deliveryColumns))
	for i, c := range deliveryColumns {
		h[i] = c.header
	}
	return h
}

// AppendDelivery adds one row to DeliveryTab, with Append's rules.
func (s *Sheet) AppendDelivery(ctx context.Context, d Delivery) error {
	row := make([]any, len(deliveryColumns))
	for i, c := range deliveryColumns {
		row[i] = c.value(d)
	}
	return s.retried(ctx, DeliveryTab, len(deliveryColumns), [][]any{row})
}

// retried is append with one more try on a 401 (see Append).
func (s *Sheet) retried(ctx context.Context, tab string, width int, values [][]any) error {
	err := s.append(ctx, tab, width, values)
	var r *Refused
	if errors.As(err, &r) && r.Stage == StageAppend && r.Status == http.StatusUnauthorized {
		s.mu.Lock()
		s.token = ""
		s.mu.Unlock()
		err = s.append(ctx, tab, width, values)
	}
	return err
}

func (s *Sheet) append(ctx context.Context, tab string, width int, values [][]any) error {
	tok, err := s.accessToken(ctx)
	if err != nil {
		return err
	}
	// The tab name quoted, as A1 notation wants for names with spaces.
	rng := "'" + strings.ReplaceAll(tab, "'", "''") + "'!A:" + string(rune('A'+width-1))
	// OVERWRITE, not INSERT_ROWS: the row goes into the first empty row
	// below the table. Inserting a row is a change to the tab's structure,
	// which Google refuses when the back office protects the header row
	// ("You are trying to edit a protected cell", 31 Oct 2026).
	u := s.API + "/v4/spreadsheets/" + url.PathEscape(s.ID) + "/values/" + url.PathEscape(rng) +
		":append?valueInputOption=RAW&insertDataOption=OVERWRITE"
	body, err := json.Marshal(map[string]any{"values": values})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("sheet: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusOK {
		return refusal(StageAppend, resp, raw)
	}
	return nil
}

// Where a refusal came from: Google's token endpoint, which judges the key,
// or the Sheets API, which judges access to the spreadsheet and the tab.
const (
	StageToken  = "token"
	StageAppend = "append"
	StageKey    = "key" // GOOGLE_SA_KEY unreadable at start; no request made
)

// Refused is a failure no retry will fix: the key was deleted, revoked or
// disabled, the spreadsheet is no longer shared with the account, it was
// deleted, or the tab was renamed. Someone has to act, so the service mails
// the back office at once (Alerting) instead of leaving it to the summary.
type Refused struct {
	Stage  string
	Status int    // HTTP status
	Detail string // Google's error body, cut short; it carries no secret
}

func (r *Refused) Error() string {
	return fmt.Sprintf("sheet: %s refused: %d %s: %s", r.Stage, r.Status, http.StatusText(r.Status), r.Detail)
}

// refusal sorts a non-200 answer: a 4xx other than 429 is Refused, the
// rest an ordinary, transient error.
func refusal(stage string, resp *http.Response, raw []byte) error {
	detail := strings.Join(strings.Fields(string(raw)), " ")
	if len(detail) > 300 {
		detail = detail[:300]
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return &Refused{Stage: stage, Status: resp.StatusCode, Detail: detail}
	}
	return fmt.Errorf("sheet: %s: %s: %s", stage, resp.Status, detail)
}

// accessToken returns a cached token, or signs a JWT and exchanges it. A
// token lives an hour; it is renewed a minute early.
func (s *Sheet) accessToken(ctx context.Context) (string, error) {
	now := s.Now()
	s.mu.Lock()
	if s.token != "" && now.Before(s.expires) {
		defer s.mu.Unlock()
		return s.token, nil
	}
	s.mu.Unlock()

	assertion, err := s.jwt(now)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("sheet: token: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusOK {
		return "", refusal(StageToken, resp, raw)
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &t); err != nil || t.AccessToken == "" {
		return "", fmt.Errorf("sheet: token: unreadable response")
	}
	s.mu.Lock()
	s.token, s.expires = t.AccessToken, now.Add(time.Duration(t.ExpiresIn)*time.Second-time.Minute)
	s.mu.Unlock()
	return t.AccessToken, nil
}

// jwt is the signed assertion of RFC 7523 that Google's token endpoint takes
// from a service account.
func (s *Sheet) jwt(now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iss": s.email, "scope": scope, "aud": s.TokenURL,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}
	signing := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("sheet: signing: %w", err)
	}
	return signing + "." + enc.EncodeToString(sig), nil
}
