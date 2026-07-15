package boarding

import (
	"crypto/rand"
	"strings"
)

// crockfordAlphabet is Crockford's base32 alphabet — 32 characters, deliberately
// excluding I, L, O, U (visually confusable with 1, 1, 0, V/0) so a short code
// read aloud or hand-typed off a phone screen doesn't ambiguously round-trip.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// codeLength is 8 characters of Crockford base32 => 32^8 ≈ 1.1e12 possible
// codes. Combined with the pass TTL (~3 minutes) and per-driver rate limiting
// on code-based scans (see ratelimit.go), brute-forcing a live code within
// its validity window is infeasible — see docs/PROGRESS.md for the full
// reasoning.
const codeLength = 8

// GenerateCode returns a random 8-character Crockford base32 code, sourced
// from crypto/rand (not math/rand — this is a security-relevant identifier,
// not a cosmetic one).
func GenerateCode() (string, error) {
	buf := make([]byte, codeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.Grow(codeLength)
	for _, b := range buf {
		sb.WriteByte(crockfordAlphabet[int(b)%len(crockfordAlphabet)])
	}
	return sb.String(), nil
}

// NormalizeCode makes code lookup/entry case-insensitive and tolerant of the
// hyphenated display grouping (e.g. "K7M2-9XQP") and incidental whitespace —
// a commuter reads the code off their screen and a driver types or the QR
// decoder hands back a string that should match regardless of that
// formatting.
func NormalizeCode(input string) string {
	var sb strings.Builder
	sb.Grow(len(input))
	for _, r := range input {
		if r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		sb.WriteRune(r)
	}
	return strings.ToUpper(sb.String())
}
