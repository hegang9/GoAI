package test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenDomainImports 列出领域层（internal/domain）禁止依赖的导入前缀。
//
// 领域层必须保持纯净：不依赖任何外层（应用层/基础设施/接口层）与具体技术框架，
// 只能依赖标准库，通过端口接口与外部协作。该测试在 CI 中固化这一架构约束。
var forbiddenDomainImports = []string{
	"GopherAI/internal/application",
	"GopherAI/internal/infrastructure",
	"GopherAI/internal/interfaces",
	"GopherAI/config",
	"github.com/gin-gonic",
	"gorm.io",
	"github.com/cloudwego/eino",
	"github.com/redis/go-redis",
	"github.com/rabbitmq/amqp091-go",
	"github.com/golang-jwt",
	"github.com/yalue/onnxruntime_go",
}

// TestDomainLayerHasNoOutwardDependencies 校验领域层不依赖外层与具体框架。
func TestDomainLayerHasNoOutwardDependencies(t *testing.T) {
	domainRoot := filepath.Join("..", "internal", "domain")

	err := filepath.Walk(domainRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, forbidden := range forbiddenDomainImports {
				if strings.HasPrefix(importPath, forbidden) {
					t.Errorf("领域层文件 %s 不应依赖 %q（违反分层约束）", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk domain dir failed: %v", err)
	}
}

// TestInfrastructureDoesNotImportInterfaces 校验基础设施层不反向依赖接口层。
func TestInfrastructureDoesNotImportInterfaces(t *testing.T) {
	infraRoot := filepath.Join("..", "internal", "infrastructure")

	err := filepath.Walk(infraRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(importPath, "GopherAI/internal/interfaces") ||
				strings.HasPrefix(importPath, "GopherAI/internal/application") {
				t.Errorf("基础设施层文件 %s 不应依赖 %q（违反分层约束）", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk infrastructure dir failed: %v", err)
	}
}
