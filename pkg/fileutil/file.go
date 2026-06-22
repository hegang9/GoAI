// Package fileutil 提供文件校验与目录清理的通用工具。
package fileutil

import (
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"GopherAI/pkg/logger"
)

// RemoveAllFilesInDir 删除指定目录下的所有普通文件，保留子目录不变。
func RemoveAllFilesInDir(dir string) error {
	// 将目录转为绝对路径，作为后续路径约束的基准。
	absDir, err := filepath.Abs(dir)
	if err != nil {
		logger.Error("RemoveAllFilesInDir resolve dir failed", "dir", dir, "err", err)
		return fmt.Errorf("resolve dir: %w", err)
	}

	// 先读取目录下的全部条目；目录不存在时视为无需清理。
	entries, err := os.ReadDir(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		logger.Error("RemoveAllFilesInDir read dir failed", "dir", absDir, "err", err)
		return err
	}

	// 再逐个删除普通文件，保留目录结构供后续继续使用。
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// 拒绝含路径分隔符或 .. 的条目名，防止拼接后逃逸。
		if !filepath.IsLocal(name) {
			logger.Warn("RemoveAllFilesInDir invalid entry name", "dir", absDir, "name", name)
			return fmt.Errorf("invalid entry name: %q", name)
		}
		filePath := filepath.Join(absDir, name)
		if err := ValidatePath(absDir, filePath); err != nil {
			logger.Warn("RemoveAllFilesInDir path escape blocked", "dir", absDir, "path", filePath, "err", err)
			return err
		}
		info, err := entry.Info()
		if err != nil {
			logger.Error("RemoveAllFilesInDir stat entry failed", "path", filePath, "err", err)
			return err
		}
		// 跳过符号链接，避免通过链接删除目录外的文件。
		if info.Mode()&os.ModeSymlink != 0 {
			logger.Warn("RemoveAllFilesInDir skip symlink", "path", filePath)
			continue
		}
		if err := os.Remove(filePath); err != nil {
			logger.Error("RemoveAllFilesInDir remove file failed", "path", filePath, "err", err)
			return err
		}
	}
	return nil
}

// ValidateFile 校验上传文件扩展名是否为允许的文本类型。
func ValidateFile(file *multipart.FileHeader) error {
	return ValidateDocExt(file.Filename)
}

// allowedDocExts 允许上传并可被 RAG 解析的文档扩展名集合。
// 与 infrastructure/rag 解析层支持的格式保持一致：纯文本、Markdown、PDF、Word。
var allowedDocExts = map[string]struct{}{
	".md":   {},
	".txt":  {},
	".pdf":  {},
	".docx": {},
}

// ValidateDocExt 按文件名校验扩展名是否为允许的文档类型（.md / .txt / .pdf / .docx）。
// 以文件名（而非 multipart）为入参，便于应用层在不感知 HTTP 细节的情况下复用。
func ValidateDocExt(filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))
	if _, ok := allowedDocExts[ext]; !ok {
		return fmt.Errorf("文件类型不正确，只允许 .md / .txt / .pdf / .docx 文件，当前扩展名: %s", ext)
	}
	return nil
}

// ValidatePath 校验 targetpath 是否位于 basepath 目录内，防止路径逃逸。
func ValidatePath(basepath, targetpath string) error {
	basepathAbs, err := filepath.Abs(basepath)
	if err != nil {
		return fmt.Errorf("resolve base path: %w", err)
	}
	targetpathAbs, err := filepath.Abs(targetpath)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}

	rel, err := filepath.Rel(basepathAbs, targetpathAbs)
	if err != nil {
		return fmt.Errorf("compute relative path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes base directory %q", targetpath, basepath)
	}
	return nil
}
