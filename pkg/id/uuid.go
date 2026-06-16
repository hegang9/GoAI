// Package id 提供标识生成的通用工具。
package id

import "github.com/google/uuid"

// GenerateUUID 生成一个新的 UUID 字符串。
func GenerateUUID() string {
	return uuid.New().String()
}
