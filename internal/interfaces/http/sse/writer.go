// Package sse 提供 SSE（Server-Sent Events）传输适配器，
// 将 HTTP 流式传输细节（响应头、data 帧编码、flush）封装在接口层，
// 使应用层只关心产出内容分片，不感知 net/http。
package sse

import (
	"fmt"
	"net/http"

	"GopherAI/pkg/logger"

	"github.com/gin-gonic/gin"
)

// Writer 封装一次 SSE 响应的传输细节。
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter 基于 gin.Context 构造 Writer，并写入标准 SSE 响应头。
// 若底层 ResponseWriter 不支持 Flush，返回 ok=false。
func NewWriter(c *gin.Context) (*Writer, bool) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		logger.Error("NewWriter: streaming unsupported, ResponseWriter is not http.Flusher")
		return nil, false
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")

	return &Writer{w: c.Writer, flusher: flusher}, true
}

// SendData 以 `data: <payload>\n\n` 帧发送一段文本并立即 flush。
func (s *Writer) SendData(payload string) error {
	if _, err := s.w.Write([]byte("data: " + payload + "\n\n")); err != nil {
		logger.Error("SSE SendData write failed", "err", err)
		return err
	}
	s.flusher.Flush()
	return nil
}

// SendSessionID 发送会话创建事件，告知客户端本次流对应的 sessionID。
func (s *Writer) SendSessionID(sessionID string) error {
	return s.SendData(fmt.Sprintf("{\"sessionId\": \"%s\"}", sessionID))
}

// SendDone 发送结束标记 `[DONE]`。
func (s *Writer) SendDone() error {
	return s.SendData("[DONE]")
}

// Chunk 返回供应用层使用的内容分片回调，应用层无需感知 SSE 编码与 flush。
func (s *Writer) Chunk() func(chunk string) {
	return func(chunk string) {
		logger.Debug("SSE sending chunk", "len", len(chunk))
		if err := s.SendData(chunk); err != nil {
			logger.Error("SSE Chunk send failed", "err", err)
		}
	}
}
