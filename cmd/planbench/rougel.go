// Package main 提供 planner 离线评测工具（cmd/planbench）。
//
// 本文件实现轻量 ROUGE-L，用于评估 planner 产出的 retrieval_query 与期望 query_keywords
// 的 n-gram 重叠质量。不引入外部 NLP 依赖，仅基于 LCS（最长公共子序列）计算 F1。
package main

import (
	"strings"
	"unicode"
)

// tokenize 按非字母数字（含中文逐字）切分为 token 序列。
// 中文按 rune 逐字切，英文按单词切，统一为小写。
func tokenize(s string) []string {
	var tokens []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			tokens = append(tokens, strings.ToLower(buf.String()))
			buf.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			// 中文 rune 直接作为单 token；英文/数字累积成词
			if unicode.Is(unicode.Han, r) {
				flush()
				tokens = append(tokens, string(r))
				continue
			}
			buf.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// lcsLen 计算两个序列的最长公共子序列长度。
func lcsLen(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp[len(a)][len(b)]
}

// rougeL 计算 candidate 与 reference 的 ROUGE-L F1。
// 空串或无重叠返回 0。
func rougeL(candidate, reference string) float64 {
	c := tokenize(candidate)
	r := tokenize(reference)
	if len(c) == 0 || len(r) == 0 {
		return 0
	}
	lcs := lcsLen(c, r)
	if lcs == 0 {
		return 0
	}
	precision := float64(lcs) / float64(len(c))
	recall := float64(lcs) / float64(len(r))
	if precision+recall == 0 {
		return 0
	}
	return 2 * precision * recall / (precision + recall)
}
