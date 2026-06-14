package router

import (
	"GopherAI/dto"
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandlerBindsJSONAndDelegatesToControllerHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	called := false
	r.POST("/login", Handler(func(c *gin.Context, req dto.LoginRequest) {
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
