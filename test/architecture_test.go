package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// domainForbiddenImports 领域层禁止引入的框架与基础设施依赖。
var domainForbiddenImports = []string{
	"github.com/gin-gonic/gin",
	"gorm.io/gorm",
	"gorm.io/driver/mysql",
	"github.com/redis/go-redis/v9",
	"github.com/streadway/amqp",
	"github.com/cloudwego/eino",
	"github.com/golang-jwt/jwt/v4",
}

// TestDomainLayer_NoFrameworkImports 校验 internal/domain 不依赖 Web/ORM/消息队列等框架。
func TestDomainLayer_NoFrameworkImports(t *testing.T) {
	t.Helper()

	domainRoot := filepath.Join("..", "internal", "domain")
	fset := token.NewFileSet()

	err := filepath.WalkDir(domainRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, forbidden := range domainForbiddenImports {
				if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
					t.Errorf("domain file %s imports forbidden package %q", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk domain layer: %v", err)
	}
}

// TestDomainLayer_NoTestOnlyConstructs 确保领域层源文件均为常规 Go 源码（非测试文件）。
func TestDomainLayer_NoTestOnlyConstructs(t *testing.T) {
	t.Helper()

	domainRoot := filepath.Join("..", "internal", "domain")
	fset := token.NewFileSet()

	err := filepath.WalkDir(domainRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}
			if fn.Name.Name == "TestMain" {
				t.Errorf("domain file %s should not define TestMain", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk domain layer: %v", err)
	}
}
