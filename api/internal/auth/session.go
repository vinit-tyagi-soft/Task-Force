// Package auth issues and verifies TaskForce session tokens.
//
// The development login issues HMAC-signed session tokens. Enterprise OIDC will
// replace the issuer, but the verification contract (issuer, audience, expiry,
// signature, then live account state) stays the same.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	Issuer     = "taskforce-dev"
	Audience   = "taskforce-api"
	CookieName = "tf_session"
)

var ErrInvalidToken = errors.New("invalid session token")

type Claims struct {
	Subject   string `json:"sub"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

var enc = base64.RawURLEncoding

func Sign(secret []byte, c Claims) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := enc.EncodeToString(payload)
	return body + "." + enc.EncodeToString(mac(secret, body)), nil
}

func Verify(secret []byte, token string, now time.Time) (Claims, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return Claims{}, ErrInvalidToken
	}
	got, err := enc.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac(secret, body)) {
		return Claims{}, ErrInvalidToken
	}
	raw, err := enc.DecodeString(body)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if c.Issuer != Issuer || c.Audience != Audience || c.Subject == "" || now.Unix() >= c.ExpiresAt {
		return Claims{}, ErrInvalidToken
	}
	return c, nil
}

func mac(secret []byte, body string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(body))
	return h.Sum(nil)
}
