package main

import (
	"GopherAI/common/aihelper"
	"GopherAI/common/logger"
	"GopherAI/common/mysql"
	"GopherAI/common/rabbitmq"
	"GopherAI/common/redis"
	"GopherAI/config"
	"GopherAI/dao"
	"GopherAI/router"
	"fmt"
)

func StartServer(addr string, port int) error {
	r := router.InitRouter()
	//服务器静态资源路径映射关系，这里目前不需要
	// r.Static(config.GetConfig().HttpFilePath, config.GetConfig().MusicFilePath)
	return r.Run(fmt.Sprintf("%s:%d", addr, port))
}

// readDataFromDBAndInitHelper 从数据库加载消息并初始化 AIHelperManager
func readDataFromDBAndInitHelper() error {
	// 初始化全局管理器
	manager := aihelper.GetGlobalManager()
	// 从数据库读取所有历史消息
	msgs, err := dao.GetAllMessages()
	if err != nil {
		return err
	}
	// 遍历数据库消息
	for i := range msgs {
		m := &msgs[i]
		//默认openai模型
		modelType := "1"
		// config
		c := make(map[string]interface{})

		// 为每个session恢复对应的 AIHelper
		helper, err := manager.GetOrCreateAIHelper(m.UserName, m.SessionID, modelType, c)
		if err != nil {
			logger.Error("readDataFromDB failed to create helper", "user", m.UserName, "session", m.SessionID, "err", err)
			continue
		}
		logger.Debug("readDataFromDB init", "session", helper.SessionID)
		// 根据message的SessionID字段，给对应的helper添加消息上下文
		// 此处实现暂时不开启持久化功能，如果开启，需要使用setSaveFunc设置自定义保存函数
		helper.AddMessage(m.Content, m.UserName, m.IsUser, false)
	}

	logger.Info("AIHelperManager init success")
	return nil
}

func main() {
	logger.InitLogger()
	conf := config.GetConfig()
	host := conf.MainConfig.Host
	port := conf.MainConfig.Port
	//初始化mysql客户端
	if err := mysql.InitMysql(); err != nil {
		logger.Error("InitMysql error", "err", err)
		return
	}
	//用数据库里的历史消息回放，重建内存中的 AI 会话状态，并初始化AIHelperManager
	if err := readDataFromDBAndInitHelper(); err != nil {
		logger.Fatal("readDataFromDBAndInitHelper failed", "err", err)
		return
	}

	//初始化redis客户端
	redis.InitRedis()
	logger.Info("redis init success")
	//初始化rabbitmq客户端
	rabbitmq.InitRabbitMQ()
	logger.Info("rabbitmq init success")

	// 启动 HTTP 服务，监听指定地址和端口
	err := StartServer(host, port)
	if err != nil {
		logger.Fatal("StartServer failed", "err", err)
	}
}
