1. ~~replayHistory 回放历史消息从全量回放改为按session懒加载 或 限制最近活跃会话回放~~（已完成：策略 B，启动预热最近 N 会话 + 运行时 ensureSessionLoaded 懒加载）
