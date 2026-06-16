// Package hash 提供通用摘要工具。
//
// 注意：MD5 仅用于非安全场景（如生成缓存键），绝不能用于密码哈希。
// 密码哈希请使用 infrastructure/security 中的 bcrypt 实现。
package hash

import (
	"crypto/md5"
	"encoding/hex"
)

// MD5 计算字符串的 MD5 十六进制摘要。
func MD5(str string) string {
	m := md5.New()
	m.Write([]byte(str))
	return hex.EncodeToString(m.Sum(nil))
}
