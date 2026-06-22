package test

import (
	"mime/multipart"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"GopherAI/pkg/fileutil"
)

func TestValidatePath_AllowsChildPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target := filepath.Join(base, "child.txt")
	if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := fileutil.ValidatePath(base, target); err != nil {
		t.Fatalf("ValidatePath() error = %v, want nil", err)
	}
}

func TestValidatePath_AllowsBaseDirectory(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	if err := fileutil.ValidatePath(base, base); err != nil {
		t.Fatalf("ValidatePath() error = %v, want nil", err)
	}
}

func TestValidatePath_BlocksEscape(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := fileutil.ValidatePath(base, target)
	if err == nil {
		t.Fatal("ValidatePath() error = nil, want path escape error")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Fatalf("error = %v, want escape message", err)
	}
}

func TestValidateDocExt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
		wantErr  bool
	}{
		{name: "markdown", filename: "readme.md", wantErr: false},
		{name: "text", filename: "notes.txt", wantErr: false},
		{name: "uppercase ext", filename: "DOC.MD", wantErr: false},
		{name: "pdf allowed", filename: "doc.pdf", wantErr: false},
		{name: "docx allowed", filename: "report.docx", wantErr: false},
		{name: "uppercase docx", filename: "REPORT.DOCX", wantErr: false},
		{name: "exe rejected", filename: "evil.exe", wantErr: true},
		{name: "no extension", filename: "README", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := fileutil.ValidateDocExt(tt.filename)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDocExt() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateFile(t *testing.T) {
	t.Parallel()

	header := &multipart.FileHeader{Filename: "guide.txt"}
	if err := fileutil.ValidateFile(header); err != nil {
		t.Fatalf("ValidateFile() error = %v, want nil", err)
	}
}

func TestRemoveAllFilesInDir_RemovesRegularFiles(t *testing.T) {
	dir := t.TempDir()
	keepDir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(keepDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"a.txt", "b.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := fileutil.RemoveAllFilesInDir(dir); err != nil {
		t.Fatalf("RemoveAllFilesInDir() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "subdir" || !entries[0].IsDir() {
		t.Fatalf("entries = %+v, want only subdir left", entries)
	}
}

func TestRemoveAllFilesInDir_NotExistIsNoOp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if err := fileutil.RemoveAllFilesInDir(dir); err != nil {
		t.Fatalf("RemoveAllFilesInDir() error = %v, want nil", err)
	}
}

func TestRemoveAllFilesInDir_SkipsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}

	dir := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	linkPath := filepath.Join(dir, "link.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("skip symlink test: %v", err)
	}

	if err := fileutil.RemoveAllFilesInDir(dir); err != nil {
		t.Fatalf("RemoveAllFilesInDir() error = %v", err)
	}
	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatalf("outside file should remain, stat error = %v", err)
	}
}
