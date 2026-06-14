package mapper

import (
	"GopherAI/model"

	"github.com/cloudwego/eino/schema"
)

func ConvertToModelMessage(sessionID string, accountNo string, msg *schema.Message) *model.Message {
	return &model.Message{
		SessionID: sessionID,
		AccountNo: accountNo,
		Content:   msg.Content,
	}
}

func ConvertToSchemaMessages(msgs []*model.Message) []*schema.Message {
	schemaMsgs := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		role := schema.Assistant
		if m.IsUser {
			role = schema.User
		}
		schemaMsgs = append(schemaMsgs, &schema.Message{
			Role:    role,
			Content: m.Content,
		})
	}
	return schemaMsgs
}
