// sse.go 提供 SSE（Server-Sent Events）传输适配器，
// 将「HTTP 流式传输细节（响应头、data 帧编码、flush）」从 service 层剥离到 controller 层，
// 使 service 只关心「产出内容分片」，不再依赖 net/http。

package controller

import (
	"fmt"
	"net/http"

	"GopherAI/common/logger"

	"github.com/gin-gonic/gin"
)

// SSEWriter 封装一次 SSE 响应的传输细节。
// 持有底层 ResponseWriter 与 Flusher，对外只暴露「发送数据帧 / 结束帧」等语义化方法。
type SSEWriter struct {
	w       http.ResponseWriter // 底层 HTTP 响应写入器
	flusher http.Flusher        // 用于在每个分片后立即推送到客户端
}

// NewSSEWriter 基于 gin.Context 构造 SSEWriter，并写入标准 SSE 响应头。
// 若底层 ResponseWriter 不支持 Flush（无法做流式推送），返回 ok=false，调用方应按错误处理。
func NewSSEWriter(c *gin.Context) (*SSEWriter, bool) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		logger.Error("NewSSEWriter: streaming unsupported, ResponseWriter is not http.Flusher")
		return nil, false
	}

	// SSE 标准响应头：声明事件流、禁用缓存、保持连接，并关闭 Nginx 缓冲以保证实时性。
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")

	return &SSEWriter{w: c.Writer, flusher: flusher}, true
}

// SendData 以 `data: <payload>\n\n` 帧发送一段文本，并立即 flush 推送给客户端。
func (s *SSEWriter) SendData(payload string) error {
	if _, err := s.w.Write([]byte("data: " + payload + "\n\n")); err != nil {
		logger.Error("SSEWriter SendData write failed", "err", err)
		return err
	}
	s.flusher.Flush()
	return nil
}

// SendSessionID 发送会话创建事件，告知客户端本次流对应的 sessionID。
func (s *SSEWriter) SendSessionID(sessionID string) error {
	return s.SendData(fmt.Sprintf("{\"sessionId\": \"%s\"}", sessionID))
}

// SendDone 发送结束标记 `[DONE]`，通知客户端流式响应结束。
func (s *SSEWriter) SendDone() error {
	return s.SendData("[DONE]")
}

// Chunk 返回一个供 service 层使用的内容分片回调。
// service 只需调用该回调输出内容，无需感知 SSE 编码与 flush 细节。
func (s *SSEWriter) Chunk() func(chunk string) {
	return func(chunk string) {
		logger.Debug("SSE sending chunk", "len", len(chunk))
		if err := s.SendData(chunk); err != nil {
			logger.Error("SSEWriter Chunk send failed", "err", err)
		}
	}
}
