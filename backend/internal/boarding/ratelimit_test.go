package boarding

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCodeRateLimiter_AllowsUpToMaxThenBlocks(t *testing.T) {
	limiter := newCodeRateLimiter(time.Minute, 3)
	driver := uuid.New()

	for i := 0; i < 3; i++ {
		if !limiter.Allow(driver) {
			t.Fatalf("expected attempt %d to be allowed", i+1)
		}
	}
	if limiter.Allow(driver) {
		t.Fatalf("expected 4th attempt within the window to be blocked")
	}
}

func TestCodeRateLimiter_PerDriverIndependent(t *testing.T) {
	limiter := newCodeRateLimiter(time.Minute, 1)
	a, b := uuid.New(), uuid.New()

	if !limiter.Allow(a) {
		t.Fatalf("expected driver a's first attempt to be allowed")
	}
	if limiter.Allow(a) {
		t.Fatalf("expected driver a's second attempt to be blocked")
	}
	if !limiter.Allow(b) {
		t.Fatalf("expected driver b's first attempt to be allowed independently of driver a")
	}
}

func TestCodeRateLimiter_WindowExpiry(t *testing.T) {
	limiter := newCodeRateLimiter(10*time.Millisecond, 1)
	driver := uuid.New()

	if !limiter.Allow(driver) {
		t.Fatalf("expected first attempt to be allowed")
	}
	if limiter.Allow(driver) {
		t.Fatalf("expected second immediate attempt to be blocked")
	}
	time.Sleep(20 * time.Millisecond)
	if !limiter.Allow(driver) {
		t.Fatalf("expected attempt after the window elapsed to be allowed again")
	}
}
