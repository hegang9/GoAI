// code.go 业务状态码和HTTP状态码映射定义

package code

import "net/http"

//code响应状态码

type Code int64

const (
	CodeSuccess Code = 1000 // 请求成功，业务处理完成

	// 2xxx: 客户端请求或用户输入相关错误
	CodeInvalidParams    Code = 2001 // 请求参数不合法（缺字段、格式错误等）
	CodeUserExist        Code = 2002 // 用户名已存在，无法重复注册
	CodeUserNotExist     Code = 2003 // 用户不存在（登录或查询用户时未找到）
	CodeInvalidPassword  Code = 2004 // 用户名或密码错误（密码校验失败）
	CodeNotMatchPassword Code = 2005 // 两次输入的密码不一致
	CodeInvalidToken     Code = 2006 // Token 无效（格式错误、签名失败、过期等）
	CodeNotLogin         Code = 2007 // 用户未登录或未携带认证信息
	CodeInvalidCaptcha   Code = 2008 // 邮箱验证码错误或校验失败
	CodeRecordNotFound   Code = 2009 // 目标记录不存在（通用“未找到”）
	CodeIllegalPassword  Code = 2010 // 密码不符合系统安全规则

	// 3xxx: 权限相关错误
	CodeForbidden Code = 3001 // 权限不足，当前用户无权执行该操作

	// 4xxx: 服务端通用错误
	CodeServerBusy Code = 4001 // 服务繁忙或内部异常，请稍后重试

	// 5xxx: AI 模型相关错误
	AIModelNotFind    Code = 5001 // 指定模型不存在或未配置
	AIModelCannotOpen Code = 5002 // 模型初始化/加载失败，无法打开模型
	AIModelFail       Code = 5003 // 模型推理过程中发生错误

	// 6xxx: 语音（TTS）服务相关错误
	TTSFail Code = 6001 // 文字转语音服务调用失败
)

var msg = map[Code]string{
	CodeSuccess: "success",

	CodeInvalidParams:    "请求参数错误",
	CodeUserExist:        "用户名已存在",
	CodeUserNotExist:     "用户不存在",
	CodeInvalidPassword:  "用户名或密码错误",
	CodeNotMatchPassword: "两次密码不一致",
	CodeInvalidToken:     "无效的Token",
	CodeNotLogin:         "用户未登录",
	CodeInvalidCaptcha:   "验证码错误",
	CodeRecordNotFound:   "记录不存在",
	CodeIllegalPassword:  "密码不合法",

	CodeForbidden: "权限不足",

	CodeServerBusy: "服务繁忙",

	AIModelNotFind:    "模型不存在",
	AIModelCannotOpen: "无法打开模型",
	AIModelFail:       "模型运行失败",
	TTSFail:           "语音服务失败",
}

func (code Code) Code() int64 {
	return int64(code)
}

// Msg 获取响应消息
func (code Code) Msg() string {
	if m, ok := msg[code]; ok {
		return m
	}
	return msg[CodeServerBusy]
}

// HTTPStatus 将业务错误码映射为标准 HTTP 状态码。
func (code Code) HTTPStatus() int {
	switch code {
	case CodeSuccess:
		return http.StatusOK
	case CodeInvalidToken, CodeNotLogin:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeUserNotExist, CodeRecordNotFound, AIModelNotFind:
		return http.StatusNotFound
	case CodeUserExist:
		return http.StatusConflict
	case CodeInvalidParams, CodeInvalidPassword, CodeNotMatchPassword, CodeInvalidCaptcha, CodeIllegalPassword:
		return http.StatusBadRequest
	case CodeServerBusy, AIModelCannotOpen, AIModelFail, TTSFail:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
