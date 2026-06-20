package test

import (
	"testing"

	"GopherAI/pkg/hash"
)

func TestMD5_KnownValue(t *testing.T) {
	t.Parallel()

	// hello 的 MD5 摘要为固定值，用于校验实现正确性。
	const input = "hello"
	const want = "5d41402abc4b2a76b9719d911017c592"

	if got := hash.MD5(input); got != want {
		t.Fatalf("MD5(%q) = %q, want %q", input, got, want)
	}
}

func TestMD5_EmptyString(t *testing.T) {
	t.Parallel()

	const want = "d41d8cd98f00b204e9800998ecf8427e"
	if got := hash.MD5(""); got != want {
		t.Fatalf("MD5(\"\") = %q, want %q", got, want)
	}
}
