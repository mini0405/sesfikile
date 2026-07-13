package boarding

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

var (
	ErrMalformedToken   = errors.New("malformed pass token")
	ErrInvalidSignature = errors.New("invalid pass signature")
)

// Signer signs and verifies boarding pass tokens with a server-side HMAC
// secret (config.BoardingHMACSecret). A token is self-contained — a
// base64url-encoded JSON payload, a ".", and a base64url-encoded HMAC-SHA256
// signature over the payload segment — so it fits directly into a QR code
// with no server-side lookup needed to verify it.
type Signer struct {
	secret []byte
}

func NewSigner(secret string) Signer {
	return Signer{secret: []byte(secret)}
}

func (s Signer) signBytes(payloadSegment string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payloadSegment))
	return mac.Sum(nil)
}

// Sign encodes and signs a pass payload, returning the compact token string.
func (s Signer) Sign(payload PassPayload) (string, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadSegment := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sigSegment := base64.RawURLEncoding.EncodeToString(s.signBytes(payloadSegment))
	return payloadSegment + "." + sigSegment, nil
}

// Verify checks the token's HMAC signature (constant-time compare) and
// decodes its payload. It does NOT check expiry — callers check
// PassPayload.Expired separately, so an expired-but-authentic pass can be
// rejected with a distinct error/status from a forged one.
func (s Signer) Verify(token string) (PassPayload, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return PassPayload{}, ErrMalformedToken
	}
	payloadSegment, sigSegment := parts[0], parts[1]

	givenMAC, err := base64.RawURLEncoding.DecodeString(sigSegment)
	if err != nil {
		return PassPayload{}, ErrMalformedToken
	}
	expectedMAC := s.signBytes(payloadSegment)
	if !hmac.Equal(givenMAC, expectedMAC) {
		return PassPayload{}, ErrInvalidSignature
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadSegment)
	if err != nil {
		return PassPayload{}, ErrMalformedToken
	}
	var payload PassPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return PassPayload{}, ErrMalformedToken
	}
	return payload, nil
}
