package test

import (
	"fmt"
	"testing"
	"unicode"

	"GopherAI/pkg/random"
)

func TestGetRandomNumbers_Length(t *testing.T) {
	t.Parallel()

	tests := []int{4, 6, 8}
	for _, n := range tests {
		n := n
		t.Run(fmt.Sprintf("len%d", n), func(t *testing.T) {
			t.Parallel()
			got := random.GetRandomNumbers(n)
			if len(got) != n {
				t.Fatalf("len = %d, want %d", len(got), n)
			}
			for _, ch := range got {
				if !unicode.IsDigit(ch) {
					t.Fatalf("non-digit %q in %q", ch, got)
				}
			}
		})
	}
}

func TestGetRandomNumbers_ZeroLength(t *testing.T) {
	t.Parallel()

	if got := random.GetRandomNumbers(0); got != "" {
		t.Fatalf("GetRandomNumbers(0) = %q, want empty string", got)
	}
}
