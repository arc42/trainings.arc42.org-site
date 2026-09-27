package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var key = []byte("0123456789abcdef0123456789abcdef")

func sealer(now *time.Time) *Sealer {
	s, err := NewSealer(key, Valid, func() time.Time { return *now })
	if err != nil {
		panic(err)
	}
	return s
}

func TestRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	s := sealer(&now)
	in := Claims{ID: "R-7F3KQ", Code: "26-12 MSA", Email: "a@example.org", LastName: "Müller", Lang: "de"}
	tok, err := s.Seal(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) > 300 {
		t.Errorf("token is %d chars; the spec promises well under 300", len(tok))
	}
	if strings.ContainsAny(tok, "+/=") {
		t.Errorf("token %q is not URL-safe", tok)
	}
	out, err := s.Open(tok)
	if err != nil {
		t.Fatal(err)
	}
	in.Issued = now.Unix()
	if out != in {
		t.Errorf("Open = %+v, want %+v", out, in)
	}
}

// Every field at its form limit, in 4-byte characters, and a code that the
// feed did not vet (it was down) must still give a link that opens. The last
// name is only there to label the BESTÄTIGT mail, whose R-id already points
// at the full first mail, so the token keeps the first 60 characters of it.
func TestTheLongestLegalSubmissionStillOpens(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	s := sealer(&now)
	in := Claims{
		ID: "R-7F3KQ", Code: strings.Repeat("𝄞", 64),
		Email:    strings.Repeat("a", 64) + "@" + strings.Repeat("b", 251) + ".org",
		LastName: strings.Repeat("𝄞", 200), Lang: "en",
		Purpose: PurposeCorrect, Corrections: 2, Dropped: 1,
	}
	tok, err := s.Seal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Open(tok)
	if err != nil {
		t.Fatalf("a %d-char token for a legal submission does not open: %v", len(tok), err)
	}
	if n := len([]rune(out.LastName)); n != 60 {
		t.Errorf("last name in the token has %d characters, want 60", n)
	}
	if out.Email != in.Email || out.Code != in.Code {
		t.Error("email or code was shortened; only the name may be")
	}
}

func TestExpiresAfterFiveDays(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	s := sealer(&now)
	tok, _ := s.Seal(Claims{ID: "R-1"})
	now = now.Add(5*24*time.Hour - time.Minute)
	if _, err := s.Open(tok); err != nil {
		t.Fatalf("still valid just before 5 days: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := s.Open(tok); !errors.Is(err, ErrExpired) {
		t.Fatalf("after 5 days: %v, want ErrExpired", err)
	}
}

func TestTamperingAndGarbageAreInvalid(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	s := sealer(&now)
	tok, _ := s.Seal(Claims{ID: "R-1"})
	flipped := []byte(tok)
	if flipped[20] == 'A' {
		flipped[20] = 'B'
	} else {
		flipped[20] = 'A'
	}
	for name, bad := range map[string]string{
		"one character changed": string(flipped),
		"empty":                 "",
		"not base64":            "%%%",
		"too short":             "AAAA",
		"huge":                  strings.Repeat("A", 5000),
	} {
		if _, err := s.Open(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestAnotherKeyCannotOpen(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	tok, _ := sealer(&now).Seal(Claims{ID: "R-1"})
	other, _ := NewSealer([]byte("ffffffffffffffffffffffffffffffff"), Valid, func() time.Time { return now })
	if _, err := other.Open(tok); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid (rotated key)", err)
	}
}
