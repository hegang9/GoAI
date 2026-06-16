// Package random 提供随机码生成的通用工具。
package random

import (
	"math/rand"
	"strconv"
	"time"
)

// GetRandomNumbers 生成指定长度的随机数字字符串。
func GetRandomNumbers(num int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	code := ""
	for i := 0; i < num; i++ {
		digit := r.Intn(10)
		code += strconv.Itoa(digit)
	}
	return code
}
