// session_loader.go 提供会话历史的运行时懒加载能力
//
// 启动阶段仅预热最近 N 个会话；用户访问冷会话时，由 ensureSessionLoaded
// 从数据库加载消息并回放到 Manager，与前端 switchSession 按需拉 /history 的语义对齐。
package chat

import (
	"context"
	"time"

	"GopherAI/pkg/logger"
)

// ensureSessionLoaded 确保指定会话的历史消息已加载到内存 Manager 中。
//
// 加载流程：
//  1. 若 Manager 中已有该会话，直接返回（热会话 / 已预热会话）；
//  2. 否则从 messageRepo.ListBySession 按 accountNo + sessionID 查询 DB；
//  3. 若有历史消息，调用 Manager.ReplayMessages 以 persist=false 回放到内存；
//  4. 若无消息（新会话或空会话），不创建 Conversation，留给后续 GetOrCreate 处理。
//
// 参数说明：
//   - accountNo：当前登录用户的内部账号编号，用于归属校验；
//   - sessionID：目标会话 ID。
//
// 回放使用 s.defaultModelType 创建会话模型；Phase 2 引入统一 auto 模型后，
// 该默认类型将切换为 "auto"，Phase 4 退役 defaultModelType 配置时再简化。
//
// 调用方：ChatSend、StreamToSession、GetChatHistory。
func (s *Service) ensureSessionLoaded(ctx context.Context, accountNo, sessionID string) error {
	// 内存命中则无需访问数据库。
	if _, ok := s.manager.Get(accountNo, sessionID); ok {
		return nil
	}

	start := time.Now()
	msgs, err := s.messageRepo.ListBySession(ctx, accountNo, sessionID)
	if err != nil {
		logger.Error("ensureSessionLoaded ListBySession failed",
			"accountNo", accountNo, "sessionID", sessionID, "err", err)
		return err
	}
	// 空会话：DB 中尚无消息，无需预先创建 Conversation。
	if len(msgs) == 0 {
		return nil
	}

	if err := s.manager.ReplayMessages(ctx, accountNo, sessionID, s.defaultModelType, modelParams(accountNo), msgs); err != nil {
		logger.Error("ensureSessionLoaded ReplayMessages failed",
			"accountNo", accountNo, "sessionID", sessionID, "err", err)
		return err
	}

	// 日志记录回放消息耗时，作为后续优化的依据
	// TODO：优化查表和回放耗时
	logger.Info("ensureSessionLoaded replay done",
		"accountNo", accountNo,
		"sessionID", sessionID,
		"messageCount", len(msgs),
		"duration", time.Since(start))
	return nil
}
