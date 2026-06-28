package test

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rag "GopherAI/internal/infrastructure/rag"
)

// TestParseFile_PlainTextAndMarkdown 校验 .txt / .md 直接按文本读取。
func TestParseFile_PlainTextAndMarkdown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.md"} {
		path := filepath.Join(dir, name)
		content := "标题\n正文内容 " + name
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		got, err := rag.ParseFile(path)
		if err != nil {
			t.Fatalf("ParseFile(%s) error = %v", name, err)
		}
		if got != content {
			t.Fatalf("ParseFile(%s) = %q, want %q", name, got, content)
		}
	}
}

// TestParseFile_Docx 校验 .docx 抽取正文 <w:t> 文本并按段落补换行。
func TestParseFile_Docx(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "report.docx")
	writeMinimalDocx(t, path, []string{"第一段内容", "第二段内容"})

	got, err := rag.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile(docx) error = %v", err)
	}
	if !strings.Contains(got, "第一段内容") || !strings.Contains(got, "第二段内容") {
		t.Fatalf("ParseFile(docx) = %q, want both paragraphs", got)
	}
}

// TestLoadDocuments_ChunksWithMetadata 校验解析+切分器分块后每块带连续 ID 与来源元数据。
//
// LoadDocuments 已改用 Eino 递归切分器：按句末标点等自然边界切分，
// 因此构造多句中文文本以触发多块切分，再校验 chunk_N 编号连续、source 元数据正确。
func TestLoadDocuments_ChunksWithMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	// 12 个以“。”分隔的短句，chunkSize=6 → 递归切分器按“。”边界聚合为多块。
	if err := os.WriteFile(path, []byte(strings.Repeat("数据。", 12)), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	docs, err := rag.LoadDocuments(context.Background(), path, 6, 0)
	if err != nil {
		t.Fatalf("LoadDocuments() error = %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("LoadDocuments() chunks = %d, want >= 2", len(docs))
	}
	for i, d := range docs {
		// ID 必须为连续的 chunk_0、chunk_1 ...（向量 key 依赖该约定）。
		if want := fmt.Sprintf("chunk_%d", i); d.ID != want {
			t.Fatalf("doc[%d] ID = %q, want %q", i, d.ID, want)
		}
		if src, ok := d.MetaData["source"].(string); !ok || src != "notes.txt" {
			t.Fatalf("doc[%d] source = %v, want notes.txt", i, d.MetaData["source"])
		}
		if chunk, ok := d.MetaData["chunk"].(int); !ok || chunk != i {
			t.Fatalf("doc[%d] chunk meta = %v, want %d", i, d.MetaData["chunk"], i)
		}
	}
}

// TestLoadDocuments_EmptyFileError 校验空文件不建立索引而返回错误。
func TestLoadDocuments_EmptyFileError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("   \n  "), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := rag.LoadDocuments(context.Background(), path, 10, 0); err == nil {
		t.Fatal("LoadDocuments(empty) error = nil, want error")
	}
}

// writeMinimalDocx 构造一个仅含正文的最小 .docx（zip + word/document.xml）用于测试。
func writeMinimalDocx(t *testing.T, path string, paragraphs []string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}

	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paragraphs {
		body.WriteString(`<w:p><w:r><w:t>` + p + `</w:t></w:r></w:p>`)
	}
	body.WriteString(`</w:body></w:document>`)

	if _, err := w.Write([]byte(body.String())); err != nil {
		t.Fatalf("write document.xml: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}
