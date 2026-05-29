package rabbitmq

import (
	"GopherAI/common/logger"
	"GopherAI/config"
	"fmt"

	"github.com/streadway/amqp"
)

// 全局connection对象
// 所有RabbitMQ都会复用该对象
var conn *amqp.Connection

// 初始化connection
func initConn() {
	// 拉取配置
	c := config.GetConfig()
	mqUrl := fmt.Sprintf(
		"amqp://%s:%s@%s:%d/%s",
		c.RabbitmqUsername, c.RabbitmqPassword, c.RabbitmqHost, c.RabbitmqPort, c.RabbitmqVhost,
	)
	logger.Debug("RabbitMQ connecting", "url", mqUrl)
	var err error
	// Dial函数是 amqp 库的入口函数，做两件事：
	// 1. 与RMQ服务器建立TCP连接
	// 2. 完成 amqp 0-9-1 协议握手————发送协议头、交换能力协商、认证凭据验证
	// 返回值conn类型是*amqp.Connection，用于创建 Channel
	conn, err = amqp.Dial(mqUrl)
	if err != nil {
		logger.Fatal("RabbitMQ connection failed", "err", err)
	}
}

// RabbitMQ 封装一个 AMQP Channel 及其所属的 Connection，代表一次与 RabbitMQ
// 服务端的“逻辑会话”。一个 Connection 上可以打开多个 RabbitMQ 实例（各自持有独立
// 的 Channel），它们共享底层 TCP 连接，避免反复握手。
//
// 字段说明：
//
//	conn     — 底层 TCP 连接（通常与全局 conn 指向同一对象），负责多路复用所有 Channel 的帧
//	channel  — 当前实例独占的 AMQP Channel，所有 Publish/Consume 操作通过它执行。非线程安全
//	Exchange — 交换机名称，空字符串表示使用 RabbitMQ 默认交换机
//	Key      — 队列名（Work 模式下 Key 即队列名）或路由键
type RabbitMQ struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	Exchange string
	Key      string
}

// NewRabbitMQ 创建 RabbitMQ 结构体实例。此时仅设置了 Exchange 和 Key，
// conn 和 channel 尚未赋值，需要后续调用 NewWorkRabbitMQ 等方法填充。
// 将构造和连接初始化分为两步的原因是：不同工作模式（Work/PubSub/Topic）
// 对 channel 的配置方式不同，但都能复用同一个基础结构。
func NewRabbitMQ(exchange string, key string) *RabbitMQ {
	return &RabbitMQ{Exchange: exchange, Key: key}
}

// Destroy 关闭 Channel 和 Connection。Channel 会向服务端发送 Close 帧，
// 通知对方释放该 Channel 上绑定的队列和消费者。Connection 的 Close 会
// 同时关闭该连接上所有 Channel（但对端可能有短暂延迟感知）。
//
// 注意：本项目 Destroy 是测试/清理用途，生产运行期间不调用。
func (r *RabbitMQ) Destroy() {
	_ = r.channel.Close()
	_ = r.conn.Close()
}

// NewWorkRabbitMQ 创建 Work 队列模式的 RabbitMQ 实例。
//
// Work 模式特点：
//   - 使用默认交换机（Exchange 为空字符串），消息直接发送到同名队列
//   - 多个消费者竞争消费同一队列，每条消息只被一个消费者处理
//   - 适合任务分发、异步写入等场景
//
// 初始化流程：
//  1. 确保全局 Connection 存在（懒加载，首次调用时触发 Dial）
//  2. 在 Connection 上打开一个全新的 Channel
//  3. 将 Connection 和 Channel 绑定到 RabbitMQ 实例
//
// 本例中会通过该实例调用 Publish 和 Consume，共用同一个 Channel。
func NewWorkRabbitMQ(queue string) *RabbitMQ {
	rabbitmq := NewRabbitMQ("", queue)

	// 全局连接只建一次，后续所有实例复用
	if conn == nil {
		initConn()
	}
	rabbitmq.conn = conn

	// 在共享的 Connection 上打开独立的 Channel
	// Channel() 是轻量操作（仅发送一个 AMQP Open 帧），可以频繁调用
	var err error
	rabbitmq.channel, err = rabbitmq.conn.Channel()
	if err != nil {
		panic(err.Error())
	}

	return rabbitmq
}

// Publish 向队列发送一条消息。
//
// 执行步骤：
//  1. QueueDeclare：声明队列（不存在时自动创建）。这是幂等操作——
//     队列已存在时直接返回队列信息，不会报错
//  2. channel.Publish：向默认交换机发送消息，路由键 = 队列名，
//     消息直接投递到对应队列
//
// 并发安全性说明：
//
//	虽然 Channel 自身非线程安全，但 go-redis 同款的 streadway/amqp
//	库对不同 Channel 上的并发 Publish 是安全的（底层通过帧序列化保证）。
//	但如果多个 goroutine 共用同一个 Channel 并发 Publish，理论上存在
//	帧交错风险。当前项目 RMQMessage 只有一个 Channel，生产者和消费者
//	各自使用它的不同操作（Publish vs Consume），在低并发下稳定运行。
func (r *RabbitMQ) Publish(message []byte) error {
	// 声明队列：保证目标队列存在（幂等，队列已存在则直接返回）
	// 参数含义：队列名、非持久化、非排他、非自动删除、无额外参数
	_, err := r.channel.QueueDeclare(r.Key, false, false, false, false, nil)
	if err != nil {
		return err
	}

	// 发送消息到默认交换机，路由键即队列名
	// Publish 参数：(exchange, routingKey, mandatory, immediate, msg)
	// mandatory=false：队列不存在时不回退消息
	// immediate=false：已废弃，忽略
	return r.channel.Publish(r.Exchange, r.Key, false, false,
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        message,
		},
	)
}

// Consume 启动消费者，从队列持续接收消息并通过回调函数处理。
//
// 执行步骤：
//  1. QueueDeclare：确保目标队列存在（与 Publish 侧相同，幂等）
//  2. channel.Consume：向服务端注册为消费者，返回 Go channel 接收消息
//  3. 循环读取 Go channel，每条消息调用 handle 回调处理
//
// 关键参数说明：
//
//	autoAck=true  → 消息一旦发给消费者即标记为已投递，不等 Ack。简单但不可靠
//	autoAck=false → 消费成功后需显式调用 msg.Ack(false)，消息不丢失
//	当前使用 true 是因为：消息已持久化到 MySQL 才被消费，即使丢失也可从 DB 恢复
//
// Consume 返回的 Go channel 永远不会主动关闭，除非 Connection 断开或
// 服务端发送 Cancel 帧（如队列被删除）。因此通常放在独立 goroutine 中运行。
func (r *RabbitMQ) Consume(handle func(msg *amqp.Delivery) error) {
	q, err := r.channel.QueueDeclare(r.Key, false, false, false, false, nil)
	if err != nil {
		panic(err)
	}

	// 注册消费者，返回的 msgs 是 Go channel，服务端推送消息时数据进入此 channel
	// 消费者名称（consumer tag）为空，让服务端自动生成
	msgs, err := r.channel.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		panic(err)
	}

	// 阻塞循环：服务端通过网络帧推送消息 → amqp 库写入 msgs channel → 此处读出
	for msg := range msgs {
		if err := handle(&msg); err != nil {
			fmt.Println(err.Error())
		}
	}
}
