package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"GopherAI/internal/infrastructure/storage"
)

func TestUserDocDir_ValidAccount(t *testing.T) {
	t.Parallel()

	dir, err := storage.UserDocDir("user123")
	if err != nil {
		t.Fatalf("UserDocDir() error = %v, want nil", err)
	}
	want := filepath.Join("uploads", "user123")
	if dir != want {
		t.Fatalf("UserDocDir() = %q, want %q", dir, want)
	}
}

func TestUserDocDir_RejectsInvalidAccount(t *testing.T) {
	t.Parallel()

	tests := []string{"", "../escape", ".."}
	for _, accountNo := range tests {
		accountNo := accountNo
		t.Run(accountNo, func(t *testing.T) {
			t.Parallel()
			_, err := storage.UserDocDir(accountNo)
			if err == nil {
				t.Fatalf("UserDocDir(%q) error = nil, want error", accountNo)
			}
		})
	}
}

func TestLocalDocStorage_Save_RejectsInvalidStoredName(t *testing.T) {
	t.Parallel()

	s := storage.NewLocalDocStorage()
	_, err := s.Save("user123", "../evil.md", strings.NewReader("content"))
	if err == nil {
		t.Fatal("Save() error = nil, want invalid stored name error")
	}
}

func TestLocalDocStorage_SaveAndClearUserDocs(t *testing.T) {
	accountNo := "test_account_" + strings.ReplaceAll(t.Name(), "/", "_")
	dir, err := storage.UserDocDir(accountNo)
	if err != nil {
		t.Fatalf("UserDocDir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	s := storage.NewLocalDocStorage()
	localPath, err := s.Save(accountNo, "doc.md", strings.NewReader("# hello"))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}

	names, err := s.ListUserDocs(accountNo)
	if err != nil {
		t.Fatalf("ListUserDocs() error = %v", err)
	}
	if len(names) != 1 || names[0] != "doc.md" {
		t.Fatalf("ListUserDocs() = %v, want [doc.md]", names)
	}

	if err := s.ClearUserDocs(accountNo); err != nil {
		t.Fatalf("ClearUserDocs() error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Fatalf("expected no files after clear, found %q", entry.Name())
		}
	}
}

func TestResolveUserDocFilename(t *testing.T) {
	accountNo := "test_resolve_" + strings.ReplaceAll(t.Name(), "/", "_")
	dir, err := storage.UserDocDir(accountNo)
	if err != nil {
		t.Fatalf("UserDocDir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	s := storage.NewLocalDocStorage()
	if _, err := s.Save(accountNo, "notes.txt", strings.NewReader("notes")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	filename, err := storage.ResolveUserDocFilename(accountNo)
	if err != nil {
		t.Fatalf("ResolveUserDocFilename() error = %v", err)
	}
	if filename != "notes.txt" {
		t.Fatalf("ResolveUserDocFilename() = %q, want notes.txt", filename)
	}
}
