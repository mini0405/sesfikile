package boarding_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"sesfikile/backend/internal/boarding"
)

func samplePayload() boarding.PassPayload {
	now := time.Now()
	return boarding.PassPayload{
		CommuterID: uuid.New(),
		RouteID:    uuid.New(),
		FromStopID: uuid.New(),
		ToStopID:   uuid.New(),
		FareCents:  1500,
		IssuedAt:   now,
		ExpiresAt:  now.Add(3 * time.Minute),
		Nonce:      uuid.NewString(),
	}
}

func TestSignThenVerifyRoundTrips(t *testing.T) {
	signer := boarding.NewSigner("test-secret")
	payload := samplePayload()

	token, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	got, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if got.CommuterID != payload.CommuterID || got.RouteID != payload.RouteID ||
		got.FromStopID != payload.FromStopID || got.ToStopID != payload.ToStopID ||
		got.FareCents != payload.FareCents || got.Nonce != payload.Nonce {
		t.Fatalf("round-tripped payload mismatch: got %+v, want %+v", got, payload)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	token, err := boarding.NewSigner("secret-a").Sign(samplePayload())
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	if _, err := boarding.NewSigner("secret-b").Verify(token); err == nil {
		t.Fatal("expected verify to fail with a different secret")
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	signer := boarding.NewSigner("test-secret")
	token, err := signer.Sign(samplePayload())
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	tampered := []byte(token)
	// Flip a byte in the payload segment (before the "." separator).
	for i, c := range tampered {
		if c == '.' {
			break
		}
		if i == 5 {
			tampered[i] = c ^ 0x01
			break
		}
	}

	if _, err := signer.Verify(string(tampered)); err == nil {
		t.Fatal("expected verify to reject a tampered payload")
	}
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	signer := boarding.NewSigner("test-secret")
	token, err := signer.Sign(samplePayload())
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	// Flip a byte in the middle of the signature segment, not the very last
	// character — a base64url encoding's final character can carry unused
	// padding bits that don't affect the decoded byte value.
	tampered := []byte(token)
	mid := len(tampered) - 4
	tampered[mid] ^= 0x01

	if _, err := signer.Verify(string(tampered)); err == nil {
		t.Fatal("expected verify to reject a tampered signature")
	}
}

func TestVerifyRejectsMalformedToken(t *testing.T) {
	signer := boarding.NewSigner("test-secret")

	cases := []string{"", "no-dot-here", "a.b.c", ".missingpayload", "missingsig."}
	for _, c := range cases {
		if _, err := signer.Verify(c); err == nil {
			t.Fatalf("expected verify to reject malformed token %q", c)
		}
	}
}

func TestExpired(t *testing.T) {
	now := time.Now()
	payload := boarding.PassPayload{IssuedAt: now.Add(-5 * time.Minute), ExpiresAt: now.Add(-1 * time.Minute)}
	if !payload.Expired(now) {
		t.Fatal("expected payload to be expired")
	}

	fresh := boarding.PassPayload{IssuedAt: now, ExpiresAt: now.Add(3 * time.Minute)}
	if fresh.Expired(now) {
		t.Fatal("expected payload to not be expired")
	}
}
