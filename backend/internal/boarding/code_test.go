package boarding

import (
	"strings"
	"testing"
)

func TestGenerateCode_LengthAndAlphabet(t *testing.T) {
	for i := 0; i < 200; i++ {
		code, err := GenerateCode()
		if err != nil {
			t.Fatalf("GenerateCode failed: %v", err)
		}
		if len(code) != codeLength {
			t.Fatalf("expected length %d, got %d (%q)", codeLength, len(code), code)
		}
		for _, r := range code {
			if !strings.ContainsRune(crockfordAlphabet, r) {
				t.Fatalf("code %q contains character %q outside the Crockford alphabet", code, r)
			}
		}
		for _, excluded := range []rune{'I', 'L', 'O', 'U', 'i', 'l', 'o', 'u'} {
			if strings.ContainsRune(code, excluded) {
				t.Fatalf("code %q contains excluded confusable character %q", code, excluded)
			}
		}
	}
}

func TestGenerateCode_NotAllIdentical(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		code, err := GenerateCode()
		if err != nil {
			t.Fatalf("GenerateCode failed: %v", err)
		}
		seen[code] = true
	}
	if len(seen) < 40 {
		t.Fatalf("expected mostly-unique codes across 50 generations, got only %d distinct", len(seen))
	}
}

func TestNormalizeCode(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"K7M29XQP", "K7M29XQP"},
		{"k7m29xqp", "K7M29XQP"},
		{"K7M2-9XQP", "K7M29XQP"},
		{" k7m2-9xqp ", "K7M29XQP"},
		{"k7m2 9xqp", "K7M29XQP"},
		{"\tK7M2-9XQP\n", "K7M29XQP"},
	}
	for _, c := range cases {
		if got := NormalizeCode(c.input); got != c.want {
			t.Fatalf("NormalizeCode(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
