// Package ai 是 AI 模型适配层：基于 eino 实现 domain/chat.Model 与 domain/chat.ModelFactory 端口。
//
// 该层负责领域消息（chat.Message）与底层 eino schema.Message 之间的转换，
// 使领域层与具体模型 SDK 完全解耦。
package ai

import (
	"GopherAI/internal/domain/chat"

	"github.com/cloudwego/eino/schema"
)

// toSchemaMessages 将领域消息列表转换为 eino schema 消息列表。
// 用户发的话会变成 Eino 的 user message，AI 回复会变成 assistant message，content直接复制
func toSchemaMessages(history []chat.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(history))
	for _, m := range history {
		role := schema.Assistant
		if m.IsUser {
			role = schema.User
		}
		out = append(out, &schema.Message{Role: role, Content: m.Content})
	}
	return out
}
