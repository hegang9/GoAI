package rag

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"GopherAI/pkg/logger"

	"github.com/ledongthuc/pdf"
)

// ParseFile 按文件扩展名将文档解析为纯文本。
//
// 支持的格式：
//   - .txt / .md / 空扩展名：直接按 UTF-8 文本读取；
//   - .pdf：抽取全部页面的纯文本；
//   - .docx：解压 OOXML 包并抽取正文文本。
//
// 未知扩展名回退为纯文本读取，避免因扩展名校验与解析层不一致导致整体失败。
func ParseFile(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".pdf":
		return parsePDF(filePath)
	case ".docx":
		return parseDOCX(filePath)
	case ".md", ".txt", "":
		return parsePlainText(filePath)
	default:
		logger.Warn("parseFile unknown ext, fallback to plain text", "ext", ext, "path", filePath)
		return parsePlainText(filePath)
	}
}

// parsePlainText 直接读取纯文本文件内容。
func parsePlainText(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		logger.Error("parsePlainText read file failed", "path", filePath, "err", err)
		return "", fmt.Errorf("read text file: %w", err)
	}
	return string(content), nil
}

// parsePDF 抽取 PDF 文档的纯文本内容。
//
// 使用 ledongthuc/pdf 的 GetPlainText 逐页提取文本；该实现对扫描件（图片型 PDF）无效，
// 这类文件会得到空文本，由上层 loadDocuments 统一判空并报错。
func parsePDF(filePath string) (string, error) {
	f, r, err := pdf.Open(filePath)
	if err != nil {
		logger.Error("parsePDF open failed", "path", filePath, "err", err)
		return "", fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	reader, err := r.GetPlainText()
	if err != nil {
		logger.Error("parsePDF get plain text failed", "path", filePath, "err", err)
		return "", fmt.Errorf("extract pdf text: %w", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		logger.Error("parsePDF read text failed", "path", filePath, "err", err)
		return "", fmt.Errorf("read pdf text: %w", err)
	}
	logger.Info("parsePDF success", "path", filePath, "pages", r.NumPage())
	return buf.String(), nil
}

// parseDOCX 抽取 .docx（OOXML）文档的正文纯文本。
//
// .docx 本质是一个 zip 包，正文位于 word/document.xml；这里仅依赖标准库解压并解析 XML，
// 抽取 <w:t> 文本节点，并在段落（<w:p>）结束处补换行，尽量还原段落结构。
func parseDOCX(filePath string) (string, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		logger.Error("parseDOCX open zip failed", "path", filePath, "err", err)
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer zr.Close()

	// 在 zip 条目中定位正文 XML。
	var docXML io.ReadCloser
	for _, zf := range zr.File {
		if zf.Name == "word/document.xml" {
			rc, oerr := zf.Open()
			if oerr != nil {
				logger.Error("parseDOCX open document.xml failed", "path", filePath, "err", oerr)
				return "", fmt.Errorf("open docx document.xml: %w", oerr)
			}
			docXML = rc
			break
		}
	}
	if docXML == nil {
		return "", fmt.Errorf("docx missing word/document.xml: %s", filePath)
	}
	defer docXML.Close()

	text, err := extractDocxText(docXML)
	if err != nil {
		logger.Error("parseDOCX extract text failed", "path", filePath, "err", err)
		return "", err
	}
	logger.Info("parseDOCX success", "path", filePath)
	return text, nil
}

// extractDocxText 流式解析 document.xml，抽取 <w:t> 文本并按段落补换行。
func extractDocxText(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	var buf strings.Builder
	// inText 标记当前是否处于 <w:t> 文本节点内。
	inText := false
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("decode docx xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				// 段落结束补换行，保留原文的段落边界。
				buf.WriteString("\n")
			}
		case xml.CharData:
			if inText {
				buf.Write(t)
			}
		}
	}
	return buf.String(), nil
}
