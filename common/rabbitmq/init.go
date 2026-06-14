package rabbitmq

var (
	RMQMessage *RabbitMQ
)

func InitRabbitMQ() {
	// 创建MQ并启动消费者
	// 无论调用多少次 NewWorkRabbitMQ，只会创建一次连接
	// 不同队列共用一个连接，可以保持不同队列消费消息的顺序

	RMQMessage = NewWorkRabbitMQ("Message")
	go RMQMessage.Consume(MQMessage)

}

// DestroyRabbitMQ 销毁RabbitMQ，关闭 Channel 与底层 Connection。
// 关闭 Connection 会使消费者 goroutine 的接收 channel 关闭并退出循环。
// 做 nil 保护，避免未初始化或重复关闭时 panic。
func DestroyRabbitMQ() {
	if RMQMessage != nil {
		RMQMessage.Destroy()
	}
}
