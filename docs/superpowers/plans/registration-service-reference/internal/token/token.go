// Package token seals the few facts the confirmation step needs into the
// confirm link itself, so the service keeps no state. AES-256-GCM makes the
// token both unreadable and tamper-evident.
package token

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
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
}

var (
	ErrInvalid = errors.New("token: invalid")
	ErrExpired = errors.New("token: expired")
)

// Valid is how long a confirm link works. Spec: 5 days, long enough for a
// weekend and the back office's follow-up after 2 to 3 working days.
const Valid = 5 * 24 * time.Hour

const maxLen = 1024

type Sealer struct {
	aead cipher.AEAD
	ttl  time.Duration
	now  func() time.Time
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
	return &Sealer{aead: aead, ttl: ttl, now: now}, nil
}

func (s *Sealer) Seal(c Claims) (string, error) {
	c.Issued = s.now().Unix()
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
