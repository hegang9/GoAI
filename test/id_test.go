package test

import (
	"regexp"
	"testing"

	"GopherAI/pkg/id"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestGenerateUUID_Format(t *testing.T) {
	t.Parallel()

	got := id.GenerateUUID()
	if !uuidPattern.MatchString(got) {
		t.Fatalf("GenerateUUID() = %q, want UUID format", got)
	}
}

func TestGenerateUUID_Unique(t *testing.T) {
	t.Parallel()

	a := id.GenerateUUID()
	b := id.GenerateUUID()
	if a == b {
		t.Fatalf("expected unique UUIDs, both were %q", a)
	}
}
