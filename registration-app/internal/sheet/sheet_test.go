package sheet

import (
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
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arc42-registration/internal/intake"
	"arc42-registration/internal/store"
)

var t0 = time.Date(2026, 10, 30, 23, 30, 0, 0, time.UTC) // 31.10., 00:30 in Berlin (CET)

func record(ps ...intake.Participant) store.Record {
	return store.Record{
		Reg: intake.Registration{ID: "R-7F3KQ", Code: "26-12 MSA", LastName: "Huber", FirstName: "Sabine",
			Email: "hr@example.org", Emails: []string{"hr@example.org"}, Company: "Beispiel GmbH", Participants: ps},
		CreatedAt: t0, Price: 2690,
	}
}

// The sheet's header row as the back office set it up, plus K and L.
func TestLayoutMatchesTheSheet(t *testing.T) {
	want := "Anrede|Name|Vorname|Firma|Infos nach Einladung|email Teilnehmer|email Sekretariat|" +
		"Datum der Anmeldung|Preis|in echtes Termin-Sheet übernommen von|Kurs|Anmeldung-ID"
	if got := strings.Join(Headers(), "|"); got != want {
		t.Errorf("headers\n%s\nwant\n%s", got, want)
	}
}

func TestRows(t *testing.T) {
	cases := []struct {
		name string
		rec  store.Record
		want [][]any
	}{
		{"registrant attends alone", record(),
			[][]any{{"", "Huber", "Sabine", "Beispiel GmbH", "", "hr@example.org", "", "31.10.2026", 2690, "", "26-12 MSA", "R-7F3KQ"}}},
		{"secretary registers two", record(
			intake.Participant{Salutation: "Frau", Last: "Meier", First: "Eva", Email: "eva@example.org"},
			intake.Participant{Salutation: "Herr", Last: "Lang", First: "Tim"}),
			[][]any{
				{"Frau", "Meier", "Eva", "Beispiel GmbH", "", "eva@example.org", "hr@example.org", "31.10.2026", 2690, "", "26-12 MSA", "R-7F3KQ"},
				{"Herr", "Lang", "Tim", "Beispiel GmbH", "", "", "hr@example.org", "31.10.2026", 2690, "", "26-12 MSA", "R-7F3KQ"},
			}},
		{"registrant attends with a colleague", record(
			intake.Participant{Salutation: "Frau", Last: "huber", First: "sabine"},
			intake.Participant{Last: "Lang", First: "Tim", Email: "tim@example.org"}),
			[][]any{
				{"Frau", "huber", "sabine", "Beispiel GmbH", "", "hr@example.org", "", "31.10.2026", 2690, "", "26-12 MSA", "R-7F3KQ"},
				{"", "Lang", "Tim", "Beispiel GmbH", "", "tim@example.org", "hr@example.org", "31.10.2026", 2690, "", "26-12 MSA", "R-7F3KQ"},
			}},
		{"participant gives the registrant's own address", record(
			intake.Participant{Last: "Huber-Lang", First: "Sabine", Email: "HR@example.org"}),
			[][]any{{"", "Huber-Lang", "Sabine", "Beispiel GmbH", "", "HR@example.org", "", "31.10.2026", 2690, "", "26-12 MSA", "R-7F3KQ"}}},
	}
	for _, c := range cases {
		if got := Rows(c.rec); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s:\n got %v\nwant %v", c.name, got, c.want)
		}
	}
	other := record()
	other.Reg.Code, other.Price = intake.Other, 0
	if r := Rows(other)[0]; r[8] != "" || r[10] != "Sonstige" {
		t.Errorf("Sonstige: price %v code %v", r[8], r[10])
	}
}

type fakeGoogle struct {
	t           *testing.T
	key         *rsa.PublicKey
	tokens      int
	appends     []string // request paths
	bodies      [][][]any
	status      int  // of the append; 0 is 200
	statusOnce  bool // status for the next append only
	tokenStatus int  // of the token endpoint; 0 is 200
	tokenURL    string
}

func (f *fakeGoogle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/token":
		f.tokens++
		if f.tokenStatus != 0 {
			w.WriteHeader(f.tokenStatus)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid JWT Signature."}`))
			return
		}
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			f.t.Errorf("grant_type %q", r.PostForm.Get("grant_type"))
		}
		f.checkJWT(r.PostForm.Get("assertion"))
		_, _ = w.Write([]byte(`{"access_token":"ya29.test","expires_in":3599,"token_type":"Bearer"}`))
	case strings.HasPrefix(r.URL.Path, "/v4/spreadsheets/"):
		if r.Header.Get("Authorization") != "Bearer ya29.test" {
			f.t.Errorf("append without the token: %q", r.Header.Get("Authorization"))
		}
		f.appends = append(f.appends, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		var body struct{ Values [][]any }
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.bodies = append(f.bodies, body.Values)
		if f.status != 0 {
			w.WriteHeader(f.status)
			if f.statusOnce {
				f.status, f.statusOnce = 0, false
			}
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"updates":{"updatedRows":1}}`))
	default:
		f.t.Errorf("unexpected request %s", r.URL)
	}
}

// checkJWT verifies what Google verifies: the RS256 signature with the
// account's key, issuer, audience, scope, and an hour's lifetime.
func (f *fakeGoogle) checkJWT(s string) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		f.t.Fatalf("assertion %q", s)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(f.key, crypto.SHA256, sum[:], sig); err != nil {
		f.t.Errorf("signature: %v", err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Iss, Scope, Aud string
		Iat, Exp        int64
	}
	_ = json.Unmarshal(raw, &c)
	if c.Iss != "registration-sheet@test.iam.gserviceaccount.com" || c.Scope != scope || c.Aud != f.tokenURL || c.Exp-c.Iat != 3600 {
		f.t.Errorf("claims %+v", c)
	}
}

func setup(t *testing.T) (*Sheet, *fakeGoogle) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	f := &fakeGoogle{t: t, key: &key.PublicKey}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.tokenURL = srv.URL + "/token"
	keyJSON, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "registration-sheet@test.iam.gserviceaccount.com",
		"private_key": pemKey, "token_uri": f.tokenURL,
	})
	s, err := New(keyJSON, "1Jtiv-test", "Tabelle1")
	if err != nil {
		t.Fatal(err)
	}
	s.API, s.HTTP = srv.URL, srv.Client()
	now := t0
	s.Now = func() time.Time { return now }
	return s, f
}

func TestAppend(t *testing.T) {
	s, f := setup(t)
	ctx := context.Background()
	rec := record(intake.Participant{Last: "Meier", First: "Eva"}, intake.Participant{Last: "Lang", First: "Tim"})
	for i := 0; i < 2; i++ {
		if err := s.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	if f.tokens != 1 {
		t.Errorf("%d token requests, want 1: the token is cached", f.tokens)
	}
	want := "/v4/spreadsheets/1Jtiv-test/values/%27Tabelle1%27%21A:L:append?valueInputOption=RAW&insertDataOption=INSERT_ROWS"
	if len(f.appends) != 2 || f.appends[0] != want {
		t.Errorf("append %v\nwant %s", f.appends, want)
	}
	// Both participants in one request: a registration is in completely
	// or not at all. JSON numbers come back as float64.
	if len(f.bodies[0]) != 2 || f.bodies[0][1][1] != "Lang" || f.bodies[0][0][8] != float64(2690) {
		t.Errorf("body %v", f.bodies[0])
	}
}

// A 4xx from the Sheets API needs a person; a 5xx or 429 is weather.
func TestRefusalsAreToldApartFromWeather(t *testing.T) {
	s, f := setup(t)
	for status, refused := range map[int]bool{403: true, 404: true, 400: true, 500: false, 503: false, 429: false} {
		f.status = status
		err := s.Append(context.Background(), record())
		var r *Refused
		if err == nil || errors.As(err, &r) != refused {
			t.Errorf("%d: err %v, refused %v, want %v", status, err, errors.As(err, &r), refused)
		}
		if refused && (r.Stage != StageAppend || r.Status != status) {
			t.Errorf("%d: %+v", status, r)
		}
	}
}

// A 401 on the append means the cached token lapsed: one retry with a new
// token, and no refusal if that works.
func TestAnExpiredTokenIsRenewedNotReported(t *testing.T) {
	s, f := setup(t)
	if err := s.Append(context.Background(), record()); err != nil {
		t.Fatal(err)
	}
	f.status, f.statusOnce = http.StatusUnauthorized, true
	if err := s.Append(context.Background(), record()); err != nil {
		t.Fatalf("after one 401: %v", err)
	}
	if f.tokens != 2 || len(f.appends) != 3 {
		t.Errorf("%d tokens, %d appends; want 2 and 3", f.tokens, len(f.appends))
	}
}

// The key deleted in the Cloud console: the token endpoint says
// invalid_grant, and that is a refusal at the token stage.
func TestADeadKeyIsARefusal(t *testing.T) {
	s, f := setup(t)
	f.tokenStatus = http.StatusBadRequest
	err := s.Append(context.Background(), record())
	var r *Refused
	if !errors.As(err, &r) || r.Stage != StageToken || r.Status != 400 || !strings.Contains(r.Detail, "invalid_grant") {
		t.Errorf("err = %v", err)
	}
	if len(f.appends) != 0 {
		t.Error("appended without a token")
	}
}

func TestNewRefusesWhatWouldFailLater(t *testing.T) {
	for name, js := range map[string]string{
		"not json":     "xkeysib-oops",
		"oauth client": `{"type":"authorized_user","client_email":"x@y"}`,
		"no key":       `{"type":"service_account","client_email":"x@y","private_key":"none"}`,
	} {
		if _, err := New([]byte(js), "id", "Tabelle1"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
