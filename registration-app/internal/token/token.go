// Package token seals the few facts the confirmation step needs into the
// confirm link itself, so the service keeps no state. AES-256-GCM makes the
// token both unreadable and tamper-evident.
package token

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Claims is deliberately small: the full registration is already in the
// back office's first mail. A short link survives mail clients that wrap
// long URLs, and link scanners log URLs.
type Claims struct {
	ID       string `json:"i"` // registration id, e.g. R-7F3KQ
	Code     string `json:"c"` // booking code, or "sonstige"
	Email    string `json:"e"`
	LastName string `json:"n"`
	Lang     string `json:"l"`
	Issued   int64  `json:"t"` // unix seconds
	// Purpose separates the two kinds of token. The confirm link in the
	// mail carries PurposeConfirm; the page shown after submitting carries
	// PurposeCorrect so the person can fix a mistyped address. A correction
	// token must never confirm: it is on a page a bot sees, the confirm
	// link is only in the mailbox.
	Purpose     string `json:"p,omitempty"`
	Corrections int    `json:"k,omitempty"` // how often the address was corrected
	// Dropped marks the token of a dropped submission, whose correction form
	// must send nothing. Always serialised (no omitempty), so a dropped and
	// an accepted submission produce tokens of the same length.
	Dropped int `json:"d"`
}

const (
	PurposeConfirm = "confirm"
	PurposeCorrect = "correct"
)

var (
	ErrInvalid = errors.New("token: invalid")
	ErrExpired = errors.New("token: expired")
)

// Valid is how long a confirm link works. Spec: 5 days, long enough for a
// weekend and the back office's follow-up after 2 to 3 working days.
const Valid = 5 * 24 * time.Hour

// maxLen bounds what Open will even try to decode. The longest legal
// submission (every form field at its limit, in 4-byte characters) seals to
// about 1300 characters; see TestTheLongestLegalSubmissionStillOpens.
const maxLen = 2048

// maxNameInToken is how much of the last name the token keeps. It only labels
// the BESTÄTIGT mail, whose R-id points at the first mail with the full name;
// keeping all 200 allowed characters made the longest links fail to open.
const maxNameInToken = 60

type Sealer struct {
	aead    cipher.AEAD
	codeKey []byte // HMAC key for Code, derived from the token key
	ttl     time.Duration
	now     func() time.Time
}

func NewSealer(key []byte, ttl time.Duration, now func() time.Time) (*Sealer, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// A separate key for the codes, so the same bytes are never used for
	// AES-GCM and for HMAC. Derived, so there is still only one secret.
	ck := sha256.Sum256(append([]byte("arc42-registration code v1:"), key...))
	return &Sealer{aead: aead, codeKey: ck[:], ttl: ttl, now: now}, nil
}

// Seal stamps the issue time unless the claims already carry one: a token
// re-issued on the correction page keeps the age of the submission, so
// re-issuing cannot extend how long it works.
func (s *Sealer) Seal(c Claims) (string, error) {
	if c.Issued == 0 {
		c.Issued = s.now().Unix()
	}
	if r := []rune(c.LastName); len(r) > maxNameInToken {
		c.LastName = string(r[:maxNameInToken])
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(s.aead.Seal(nonce, nonce, plain, nil)), nil
}

func (s *Sealer) Open(tok string) (Claims, error) {
	if tok == "" || len(tok) > maxLen {
		return Claims{}, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return Claims{}, ErrInvalid
	}
	plain, err := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], nil)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	var c Claims
	if err := json.Unmarshal(plain, &c); err != nil {
		return Claims{}, ErrInvalid
	}
	issued := time.Unix(c.Issued, 0)
	now := s.now()
	if issued.After(now.Add(time.Minute)) {
		return Claims{}, ErrInvalid
	}
	if now.Sub(issued) > s.ttl {
		return Claims{}, ErrExpired
	}
	return c, nil
}

// Age is how long ago the claims were first issued.
func (s *Sealer) Age(c Claims) time.Duration {
	return s.now().Sub(time.Unix(c.Issued, 0))
}

// Code is the 6-digit confirmation code for a registration, shown in the
// registrant mail and typed on the "please confirm" page. It is computed,
// not stored: an HMAC of registration id and address, so a corrected
// address gets a new code, and nobody without the token key can derive one.
// Six digits are guessable only by trying; the web layer counts wrong tries
// per registration (store.Failure) and locks after a few.
func (s *Sealer) Code(id, email string) string {
	m := hmac.New(sha256.New, s.codeKey)
	m.Write([]byte(id + "\x00" + strings.ToLower(email)))
	return fmt.Sprintf("%06d", binary.BigEndian.Uint64(m.Sum(nil)[:8])%1_000_000)
}

// CheckCode compares a typed code with the right one, ignoring the spaces
// and dashes people paste along ("482 913").
func (s *Sealer) CheckCode(id, email, typed string) bool {
	typed = strings.NewReplacer(" ", "", "-", "", "\u00a0", "").Replace(strings.TrimSpace(typed))
	if len(typed) != 6 || strings.Trim(typed, "0123456789") != "" {
		return false
	}
	return hmac.Equal([]byte(typed), []byte(s.Code(id, email)))
}
