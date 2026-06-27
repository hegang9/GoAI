// Package dto 定义 HTTP 接口层的请求与响应数据传输对象。
//
// DTO 仅服务于接口层的序列化契约，不承载业务逻辑；
// 应用层结果由 controller 映射为对应 DTO。
package dto

import "GopherAI/pkg/code"

// Response 统一响应信封：所有 HTTP 接口均以该结构返回。
//
// status_code/status_msg 描述业务结果，data 承载业务数据；
// 失败或无业务数据时 data 为 null（不使用 omitempty，保证字段恒存在，前端可稳定解析）。
type Response struct {
	StatusCode code.Code `json:"status_code"`
	StatusMsg  string    `json:"status_msg,omitempty"`
	Data       any       `json:"data"`
}

// NewResponse 按业务状态码与数据构造统一响应信封，status_msg 自动取状态码对应文案。
func NewResponse(c code.Code, data any) Response {
	return Response{
		StatusCode: c,
		StatusMsg:  c.Msg(),
		Data:       data,
	}
}
