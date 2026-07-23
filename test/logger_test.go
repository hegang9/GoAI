package test

import (
	"testing"

	"github.com/gin-gonic/gin"

	"GopherAI/pkg/logger"
)

func TestInitLogger_DoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger.InitLogger()
	logger.Info("logger test info")
	logger.Warn("logger test warn")
	logger.Debug("logger test debug")
	logger.Error("logger test error")
}
