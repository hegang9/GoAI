// Package controller 是 HTTP 接口层的处理器集合。
//
// 处理器以 Handlers 结构持有各应用服务（通过 bootstrap 依赖注入），
// 只负责：解析请求、调用应用服务、把应用层结果映射为 DTO 响应。
package controller

import (
	chatapp "GopherAI/internal/application/chat"
	fileapp "GopherAI/internal/application/file"
	imageapp "GopherAI/internal/application/image"
	ttsapp "GopherAI/internal/application/tts"
	userapp "GopherAI/internal/application/user"
)

// Handlers 聚合所有应用服务，供路由注册各业务处理器方法。
type Handlers struct {
	User  *userapp.Service
	Chat  *chatapp.Service
	File  *fileapp.Service
	Image *imageapp.Service
	TTS   *ttsapp.Service
}

// NewHandlers 创建处理器集合。
func NewHandlers(
	user *userapp.Service,
	chat *chatapp.Service,
	file *fileapp.Service,
	image *imageapp.Service,
	tts *ttsapp.Service,
) *Handlers {
	return &Handlers{User: user, Chat: chat, File: file, Image: image, TTS: tts}
}
