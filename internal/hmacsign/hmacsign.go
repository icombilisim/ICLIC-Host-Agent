// Package hmacsign signs outbound HTTP requests with the scheme described in
// docs/protocol.md (HMAC-SHA256 over METHOD\nPATH\nTS\nBODY-SHA256).
//
// The signature header is:
//
//	Authorization: HMAC kid=<kid>, ts=<unix_seconds>, sig=<hex>
package hmacsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Signer holds the HMAC credentials and signs requests in place by setting the
// Authorization header.
type Signer struct {
	Kid    string
	Secret []byte
	now    func() time.Time
}

// New returns a Signer using the given kid + secret. Time source is
// time.Now() — overridable in tests via NewWithClock.
func New(kid, secret string) *Signer {
	return &Signer{Kid: kid, Secret: []byte(secret), now: time.Now}
}

// NewWithClock is a test helper that lets the caller pin the clock.
func NewWithClock(kid, secret string, now func() time.Time) *Signer {
	return &Signer{Kid: kid, Secret: []byte(secret), now: now}
}

// Sign mutates req by setting the Authorization header. body is the raw bytes
// that will be sent (already serialized JSON, etc.).
func (s *Signer) Sign(req *http.Request, body []byte) {
	ts := strconv.FormatInt(s.now().Unix(), 10)
	bodyHash := sha256.Sum256(body)
	canonical := fmt.Sprintf("%s\n%s\n%s\n%s",
		req.Method,
		req.URL.RequestURI(),
		ts,
		hex.EncodeToString(bodyHash[:]),
	)
	mac := hmac.New(sha256.New, s.Secret)
	mac.Write([]byte(canonical))
	sig := hex.EncodeToString(mac.Sum(nil))
	req.Header.Set("Authorization", fmt.Sprintf("HMAC kid=%s, ts=%s, sig=%s", s.Kid, ts, sig))
}
