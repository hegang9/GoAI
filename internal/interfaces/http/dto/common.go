// Package dto 定义 HTTP 接口层的请求与响应数据传输对象。
//
// DTO 仅服务于接口层的序列化契约，不承载业务逻辑；
// 应用层结果由 controller 映射为对应 DTO。
package dto

import "GopherAI/pkg/code"

// Response 通用响应结构，内嵌于各业务响应中。
type Response struct {
	StatusCode code.Code `json:"status_code"`
	StatusMsg  string    `json:"status_msg,omitempty"`
}

// CodeOf 按指定业务状态码填充通用响应结构。
func (r *Response) CodeOf(c code.Code) Response {
	if nil == r {
		r = new(Response)
	}
	r.StatusCode = c
	r.StatusMsg = c.Msg()
	return *r
}

// Success 将通用响应结构设置为成功状态。
func (r *Response) Success() {
	r.CodeOf(code.CodeSuccess)
}
