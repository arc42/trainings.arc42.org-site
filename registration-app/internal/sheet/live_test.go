package sheet

// Tests against the real Google, opt-in: CI and `make reg-check` stay
// offline and skip them.
//
//   SHEET_LIVE=1 go test ./internal/sheet -run Live -v
//       a forged key for the real service account: Google's token
//       endpoint refuses it as it refuses a deleted key. Needs no secret.
//
//   SHEET_LIVE=1 GOOGLE_SA_KEY="$(cat key.json)" SHEET_ID=... go test ./internal/sheet -run Live -v
//       additionally appends one test registration (two rows, marked
//       TEST) to the real sheet, and checks that a missing tab and a
//       missing spreadsheet come back as refusals, not as crashes.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"arc42-registration/internal/intake"
	"arc42-registration/internal/store"
)

const liveAccount = "registration-sheet@arc42-registration-write-sheet.iam.gserviceaccount.com"

func live(t *testing.T) {
	if os.Getenv("SHEET_LIVE") != "1" {
		t.Skip("SHEET_LIVE=1 not set")
	}
}

// A key Google never issued, for the real account: what a deleted or
// revoked key looks like from here.
func TestLiveADeadKeyIsRefusedAndMailed(t *testing.T) {
	live(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	keyJSON, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": liveAccount,
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"token_uri":   "https://oauth2.googleapis.com/token",
	})
	s, err := New(keyJSON, "not-used", "Tabelle1")
	if err != nil {
		t.Fatal(err)
	}
	snd := &fakeSender{}
	var logs bytes.Buffer
	a := &Alerting{Sheet: s, Sender: snd, To: []string{"office@example.org"}, Log: log.New(&logs, "", 0), Every: 24 * time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = a.Append(ctx, liveRecord())
	var r *Refused
	if !errors.As(err, &r) || r.Stage != StageToken {
		t.Fatalf("err = %v, want a refusal at the token stage", err)
	}
	t.Logf("Google said: %d %s", r.Status, r.Detail)
	if len(snd.got) != 1 {
		t.Fatalf("%d alert mails, want 1; log: %s", len(snd.got), logs.String())
	}
	t.Logf("alert subject: %s\n%s", snd.got[0].Subject, snd.got[0].Text)
}

func TestLiveAppendToTheRealSheet(t *testing.T) {
	live(t)
	key, id := os.Getenv("GOOGLE_SA_KEY"), os.Getenv("SHEET_ID")
	if key == "" || id == "" {
		t.Skip("GOOGLE_SA_KEY and SHEET_ID not set")
	}
	tab := os.Getenv("SHEET_TAB")
	if tab == "" {
		tab = "Tabelle1"
	}
	s, err := New([]byte(key), id, tab)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("service account: %s", s.Email())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := s.Append(ctx, liveRecord()); err != nil {
		t.Fatalf("append: %v", err)
	}
	t.Logf("appended 2 rows (R-TEST1) to %q: please check the sheet, then delete them", tab)

	for name, where := range map[string][2]string{
		"missing tab":         {id, "gibt-es-nicht"},
		"missing spreadsheet": {"1-gibt-es-nicht-" + strings.Repeat("x", 20), tab},
	} {
		bad, err := New([]byte(key), where[0], where[1])
		if err != nil {
			t.Fatal(err)
		}
		err = bad.Append(ctx, liveRecord())
		var r *Refused
		if !errors.As(err, &r) || r.Stage != StageAppend {
			t.Errorf("%s: err = %v, want a refusal at the append stage", name, err)
			continue
		}
		t.Logf("%s: %d %s", name, r.Status, r.Detail)
	}
}

func liveRecord() store.Record {
	return store.Record{
		Reg: intake.Registration{ID: "R-TEST1", Code: "26-12 MSA", LastName: "TEST Anmeldende", FirstName: "bitte löschen",
			Email: "test-hr@example.org", Emails: []string{"test-hr@example.org"}, Company: "TEST GmbH",
			Participants: []intake.Participant{
				{Salutation: "Frau", Last: "TEST Meier", First: "Eva", Email: "test-eva@example.org"},
				{Salutation: "Herr", Last: "TEST Lang", First: "Tim"},
			}},
		CreatedAt: time.Now(), Price: 2690,
	}
}
