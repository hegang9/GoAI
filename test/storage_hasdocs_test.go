package test

import (
	"os"
	"strings"
	"testing"

	"GopherAI/internal/infrastructure/storage"
)

// TestHasUserDocs 校验账号有无文档的判断。
func TestHasUserDocs(t *testing.T) {
	accountNo := "test_hasdocs_" + strings.ReplaceAll(t.Name(), "/", "_")
	dir, err := storage.UserDocDir(accountNo)
	if err != nil {
		t.Fatalf("UserDocDir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	// 尚无目录/文件时应为 false。
	if storage.HasUserDocs(accountNo) {
		t.Fatal("HasUserDocs() = true before any upload, want false")
	}

	s := storage.NewLocalDocStorage()
	if _, err := s.Save(accountNo, "a.md", strings.NewReader("# hi")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !storage.HasUserDocs(accountNo) {
		t.Fatal("HasUserDocs() = false after upload, want true")
	}
}

// TestListUserDocs_MultiDoc 校验多文档场景下可保留并列出多个文件（多文档知识库）。
func TestListUserDocs_MultiDoc(t *testing.T) {
	accountNo := "test_multidoc_" + strings.ReplaceAll(t.Name(), "/", "_")
	dir, err := storage.UserDocDir(accountNo)
	if err != nil {
		t.Fatalf("UserDocDir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	s := storage.NewLocalDocStorage()
	for _, name := range []string{"one.md", "two.txt"} {
		if _, err := s.Save(accountNo, name, strings.NewReader("content")); err != nil {
			t.Fatalf("Save(%s) error = %v", name, err)
		}
	}

	names, err := s.ListUserDocs(accountNo)
	if err != nil {
		t.Fatalf("ListUserDocs() error = %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("ListUserDocs() = %v, want 2 files", names)
	}
}
