package test

import (
	"net/http"
	"testing"

	"GopherAI/pkg/code"
)

func TestCode_Msg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		c    code.Code
		want string
	}{
		{name: "success", c: code.CodeSuccess, want: "success"},
		{name: "invalid params", c: code.CodeInvalidParams, want: "请求参数错误"},
		{name: "unknown falls back to server busy", c: code.Code(9999), want: "服务繁忙"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.c.Msg(); got != tt.want {
				t.Fatalf("Msg() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCode_HTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		c    code.Code
		want int
	}{
		{name: "success", c: code.CodeSuccess, want: http.StatusOK},
		{name: "unauthorized", c: code.CodeNotLogin, want: http.StatusUnauthorized},
		{name: "forbidden", c: code.CodeForbidden, want: http.StatusForbidden},
		{name: "not found", c: code.CodeRecordNotFound, want: http.StatusNotFound},
		{name: "conflict", c: code.CodeUserExist, want: http.StatusConflict},
		{name: "bad request", c: code.CodeInvalidParams, want: http.StatusBadRequest},
		{name: "internal", c: code.CodeServerBusy, want: http.StatusInternalServerError},
		{name: "default internal", c: code.Code(9999), want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.c.HTTPStatus(); got != tt.want {
				t.Fatalf("HTTPStatus() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCode_Code(t *testing.T) {
	t.Parallel()

	if got := code.CodeSuccess.Code(); got != 1000 {
		t.Fatalf("Code() = %d, want 1000", got)
	}
}
