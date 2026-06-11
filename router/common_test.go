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
		if req.Username != "alice" {
			t.Fatalf("expected username alice, got %s", req.Username)
		}
		c.Status(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"username":"alice","password":"secret"}`))
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
