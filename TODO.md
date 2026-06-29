# 消息回放演进
## 已完成

- [x] replayHistory 回放历史消息从全量回放改为按 session 懒加载 / 限制最近活跃会话回放（策略 B：启动预热最近 N 会话 + 运行时 `ensureSessionLoaded` 懒加载）

## messages 单表演进路线

当前采用 **单表 `messages` + `session_id` 区分会话**（不为每个 session 单独建表）。随数据量增长，按阶段演进：

### 阶段 1（当前）

- [x] `messages` 单表，`session_id` 逻辑归属会话
- [x] 策略 B：启动 `ListRecent` 预热 + 运行时 `ListBySession` 懒加载
- [ ] 为懒加载添加联合索引 `(session_id, created_at, id)`（见 `MessagePO` / 手写 migration）

### 阶段 2：索引优化

- [ ] 在 `messages` 上建立 `(session_id, created_at, id)` 联合索引
- [ ] 用 `EXPLAIN` 验证 `ListBySession` 无 `Using filesort`
- [ ] 评估是否保留旧的 `session_id` 单列索引（避免重复索引）

### 阶段 3：分区（行数到百万～千万级）

- [ ] 按 `created_at` 或 `account_no` 对 `messages` 做 MySQL 分区（仍为逻辑单表）
- [ ] 评估启动 `ListRecent` 的 `GROUP BY session_id` 耗时，必要时改为维护 `sessions.last_message_at` 字段
- [ ] 消息落库（MQ 消费者）时同步更新 `sessions.last_message_at`，使「最近活跃」查询可走 `sessions` 索引

### 阶段 4：冷热分离

- [ ] 长期不访问的冷会话历史归档到对象存储 / 历史库
- [ ] 热数据留 MySQL，冷会话首次访问时从归档加载（需扩展 `ensureSessionLoaded`）
- [ ] 制定归档策略（如 N 天未活跃、单用户会话数上限）

### 阶段 5：分库分表（超大规模式，可选）

- [ ] 按 `account_no` 或 `tenant_id` 分库分表（**不是**每 session 一表）
- [ ] 引入分片路由层，仓储端口背后切换为多数据源
- [ ] 读写分离、只读副本承载 `ListBySession` 读流量

## 设计原则（备忘）

- **不采用**「每个 session 单独一张 message 表」：表数量爆炸、`ListRecent` 难以实现、GORM/运维成本高
- 单表瓶颈主要是 **行数过多**，优先索引 → 分区 → 归档 → 分库分表
- 懒加载读路径核心 SQL：`WHERE account_no=? AND session_id=? ORDER BY created_at, id`


--- 

# 模型选择演进
- [ ] 目前普通对话/MCP/RAG三种不同路径由用户手动选择，需要改为在LLM Router中先让模型做一次轻量分类，输出结构化结果，在根据结构化结果调用对应模型。

- [ ] MCP 模型启动或首次使用时调用 MCP Server 的 `ListTools`，动态获取工具名、描述与参数 schema，并拼入工具选择 prompt，避免客户端硬编码工具列表与服务端实际工具定义不一致。

# 启动bug修复和代码优化
- [ ] 后端能够发送验证码，但是前端会返回失败，注册同
- [ ] 前端界面简陋，配置fingma优化前端界面
- [ ] 完善日志，设置滚动日志存储

# 功能完善
- [ ] 支持用户昵称