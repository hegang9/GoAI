// 本文件定义了两端的数据格式和桥接逻辑：
//   - 生产者端：GenerateMessageMQParam 将 Message 结构体序列化为 JSON，作为消息体发送
//   - 消费者端：MQMessage 从 amqp.Delivery 中反序列化 JSON，重建 Message 并写入 MySQL
//
// 两端通过 MessageMQParam 结构体约定消息格式，相当于一个简单的应用层协议。
package rabbitmq

import (
	"GopherAI/dao/message" // DAO 层，负责将消息写入 MySQL
	"GopherAI/model"       // 数据模型
	"encoding/json"

	"github.com/streadway/amqp"
)

// MessageMQParam 是消息队列中传输的 JSON 载荷结构体。
// 它解决了 model.Message 中 GORM tag 与 JSON 耦合的问题——
// model.Message 既用于数据库映射，又需要在 RabbitMQ 中传输，
// 单独定义 DTO 可以避免 GORM 元数据（ID、CreatedAt 等自动字段）
// 污染消息体，清晰隔离"数据库模型"和"消息模型"。
type MessageMQParam struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	UserName  string `json:"user_name"`
	IsUser    bool   `json:"is_user"`
}

// GenerateMessageMQParam 将生产者端的消息参数序列化为 JSON 字节数组。
// 在 aihelper.saveFunc 中调用，生成的字节数组作为 amqp.Publishing.Body
// 发送到 RabbitMQ 队列。
//
// error 被忽略（json.Marshal 对纯基础类型结构体不会失败）。
func GenerateMessageMQParam(sessionID string, content string, userName string, IsUser bool) []byte {
	param := MessageMQParam{
		SessionID: sessionID,
		Content:   content,
		UserName:  userName,
		IsUser:    IsUser,
	}
	data, _ := json.Marshal(param)
	return data
}

// MQMessage 是 RabbitMQ 消费者的回调函数，在 Consume 循环中被调用。
//
// 它接收一个 amqp.Delivery 消息，将 JSON 载荷反序列化为 MessageMQParam，
// 再转换为 model.Message 后通过 DAO 层异步写入 MySQL。
//
// 这是整个"异步消息持久化"链路的关键桥接函数：
//
//	AIHelper → GenerateMessageMQParam → Publish → [RabbitMQ] → Consume → MQMessage → DAO → MySQL
//
// 注意：当前不使用手动 ACK（Consume 中 autoAck=true），如果写入 MySQL 失败，
// 消息仍然丢失。对可靠性有要求时应改为手动 ACK 模式。
func MQMessage(msg *amqp.Delivery) error {
	var param MessageMQParam
	err := json.Unmarshal(msg.Body, &param)
	if err != nil {
		return err
	}
	newMsg := &model.Message{
		SessionID: param.SessionID,
		Content:   param.Content,
		UserName:  param.UserName,
		IsUser:    param.IsUser,
	}
	// 异步插入数据库：此处在消费者 goroutine 中执行，
	// 不阻塞 HTTP 请求处理
	message.CreateMessage(newMsg)
	return nil
}
