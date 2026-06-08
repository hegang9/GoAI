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

// 从数据库加载消息并初始化 AIHelperManager
func readDataFromDB() error {
	manager := aihelper.GetGlobalManager()
	// 从数据库读取所有消息
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

		// 创建对应的 AIHelper
		helper, err := manager.GetOrCreateAIHelper(m.UserName, m.SessionID, modelType, c)
		if err != nil {
			logger.Error("readDataFromDB failed to create helper", "user", m.UserName, "session", m.SessionID, "err", err)
			continue
		}
		logger.Debug("readDataFromDB init", "session", helper.SessionID)
		// 添加消息到内存中(不开启存储功能)
		helper.AddMessage(m.Content, m.UserName, m.IsUser, false)
	}

	logger.Info("AIHelperManager init success")
	return nil
}

func main() {
	logger.Init()
	conf := config.GetConfig()
	host := conf.MainConfig.Host
	port := conf.MainConfig.Port
	//初始化mysql
	if err := mysql.InitMysql(); err != nil {
		logger.Error("InitMysql error", "err", err)
		return
	}
	//初始化AIHelperManager
	if err := readDataFromDB(); err != nil {
		logger.Fatal("readDataFromDB failed", "err", err)
		return
	}

	//初始化redis
	redis.Init()
	logger.Info("redis init success")
	rabbitmq.InitRabbitMQ()
	logger.Info("rabbitmq init success")

	err := StartServer(host, port) // 启动 HTTP 服务
	if err != nil {
		logger.Fatal("StartServer failed", "err", err)
	}
}
