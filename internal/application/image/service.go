// Package image 是图像识别应用服务：编排图片识别用例。
//
// 它依赖图像识别端口（image.Recognizer），由 bootstrap 注入具体实现（如 ONNX）。
package image

import (
	domainimage "GopherAI/internal/domain/image"
	"GopherAI/pkg/code"
	"GopherAI/pkg/logger"
)

// Service 图像识别应用服务。
type Service struct {
	recognizer domainimage.Recognizer
}

// NewService 创建图像识别应用服务。
func NewService(recognizer domainimage.Recognizer) *Service {
	return &Service{recognizer: recognizer}
}

// Recognize 对上传图片字节执行识别，返回分类名称。
func (s *Service) Recognize(content []byte) (string, code.Code) {
	className, err := s.recognizer.Recognize(content)
	if err != nil {
		logger.Error("Recognize failed", "err", err)
		return "", code.CodeServerBusy
	}
	return className, code.CodeSuccess
}
