// Package fileutil 提供文件校验与目录清理的通用工具。
package fileutil

import (
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

// RemoveAllFilesInDir 删除指定目录下的所有普通文件，保留子目录不变。
func RemoveAllFilesInDir(dir string) error {
	// 先读取目录下的全部条目；目录不存在时视为无需清理。
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// 再逐个删除普通文件，保留目录结构供后续继续使用。
	for _, entry := range entries {
		if !entry.IsDir() {
			filePath := filepath.Join(dir, entry.Name())
			// TODO: 用户可控参数 accountNo 经 UserDocDir(accountNo) 生成目录 dir ， RemoveAllFilesInDir 函数中 filePath 由 dir 和 entry.Name() 拼接，直接传入 os.Remove 未做路径校验，恶意 accountNo 可构造 ../ 等路径逃逸，导致删除任意文件。
			if err := os.Remove(filePath); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateFile 校验上传文件扩展名是否为允许的文本类型。
func ValidateFile(file *multipart.FileHeader) error {
	return ValidateDocExt(file.Filename)
}

// ValidateDocExt 按文件名校验扩展名是否为允许的文本类型（.md / .txt）。
// 以文件名（而非 multipart）为入参，便于应用层在不感知 HTTP 细节的情况下复用。
func ValidateDocExt(filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".md" && ext != ".txt" {
		return fmt.Errorf("文件类型不正确，只允许 .md 或 .txt 文件，当前扩展名: %s", ext)
	}
	return nil
}
