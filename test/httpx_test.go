package test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"GopherAI/internal/interfaces/http/dto"
	"GopherAI/internal/interfaces/http/httpx"

	"github.com/gin-gonic/gin"
)

// TestHandlerBindsJSONAndDelegates 验证通用 Handler 会完成 JSON 绑定并调用业务处理函数。
func TestHandlerBindsJSONAndDelegates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	called := false
	r.POST("/login", httpx.Handler(func(c *gin.Context, req dto.LoginRequest) {
		called = true
		if req.Email != "alice@example.com" {
			t.Fatalf("expected email alice@example.com, got %s", req.Email)
		}
		c.Status(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"alice@example.com","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if !called {
		t.Fatal("expected wrapped handler to be called")
	}
	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, resp.Code)
	}
}

// TestHandlerRejectsInvalidJSON 验证非法 JSON 会被拦截并返回参数错误。
func TestHandlerRejectsInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	called := false
	r.POST("/login", httpx.Handler(func(c *gin.Context, req dto.LoginRequest) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":""}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if called {
		t.Fatal("handler should not be called on invalid input")
	}
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}
