# GORM 表名与字段名映射规则

这份文档说明本项目中 GORM 如何把 Go 结构体映射到具体的数据库表和字段。

## 1. 默认表名映射规则

GORM 默认会把结构体名：

- 转成 `snake_case`
- 再转成复数表名

例如：

```go
type User struct {}
type Session struct {}
type Message struct {}
```

默认会映射成：

- `User` -> `users`
- `Session` -> `sessions`
- `Message` -> `messages`

所以当代码里使用：

```go
var msgs []model.Message
mysql.DB.Find(&msgs)
```

GORM 会根据 `model.Message` 推断当前操作的表通常是 `messages`。

## 2. 默认字段名映射规则

GORM 默认会把结构体字段名转成 `snake_case` 列名。

例如：

```go
type Message struct {
    ID        uint
    SessionID string
    UserName  string
    Content   string
    IsUser    bool
    CreatedAt time.Time
}
```

默认会映射成这些列：

- `ID` -> `id`
- `SessionID` -> `session_id`
- `UserName` -> `user_name`
- `Content` -> `content`
- `IsUser` -> `is_user`
- `CreatedAt` -> `created_at`

因此下面这段代码里的排序字段：

```go
mysql.DB.Order("created_at asc").Find(&msgs)
```

对应的就是数据库中的 `created_at` 列。

## 3. GORM tag 如何影响映射

结构体字段上的 `gorm:"..."` tag 可以补充字段约束和数据库元信息。

例如本项目中的 `model.Message`：

```go
type Message struct {
    ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
    SessionID string `gorm:"index;not null;type:varchar(36)" json:"session_id"`
    UserName  string `gorm:"type:varchar(20)" json:"username"`
    Content   string `gorm:"type:text" json:"content"`
    IsUser    bool   `gorm:"not null;" json:"is_user"`
    CreatedAt time.Time `json:"created_at"`
}
```

这些 tag 的作用主要是：

- `primaryKey`：主键
- `autoIncrement`：自增
- `index`：创建索引
- `not null`：非空约束
- `type:varchar(36)`：指定列类型
- `type:text`：指定长文本列类型

注意：

这些 tag 并不会自动改变默认列名，除非显式写了 `column:xxx`。

例如：

```go
SessionID string `gorm:"column:sid"`
```

这时字段就不再映射到 `session_id`，而是映射到 `sid`。

## 4. 如何手动指定表名

如果不想使用 GORM 默认的复数表名规则，可以为结构体实现 `TableName()` 方法。

例如：

```go
func (Message) TableName() string {
    return "chat_messages"
}
```

加上这个方法后，`Message` 对应的表就不再是 `messages`，而是 `chat_messages`。

## 5. 本项目里是如何建表的

本项目在 `common/mysql/mysql.go` 中调用：

```go
DB.AutoMigrate(
    new(model.User),
    new(model.Session),
    new(model.Message),
)
```

这表示 GORM 会根据这些模型结构自动同步表结构。

因此：

- `model.User` 对应用户表
- `model.Session` 对应会话表
- `model.Message` 对应消息表

在当前代码里，由于 `model.Message` 没有自定义 `TableName()`，所以它默认对应的就是 `messages` 表。

## 6. 如何理解 `Find(&msgs)` 在做什么

例如：

```go
func GetAllMessages() ([]model.Message, error) {
    var msgs []model.Message
    err := mysql.DB.Order("created_at asc").Find(&msgs).Error
    return msgs, err
}
```

可以把它理解成：

1. 根据 `msgs` 的元素类型 `model.Message`，确定要查的是 `messages` 表
2. 按 `created_at asc` 排序
3. 查询多条记录
4. 把查询结果填充到 `msgs` 切片里
5. 返回查询错误

大致对应 SQL：

```sql
SELECT * FROM messages ORDER BY created_at ASC;
```

## 7. 一句话总结

GORM 的默认映射规则可以概括为：

- 结构体名 -> `snake_case` 复数表名
- 字段名 -> `snake_case` 列名
- `gorm` tag 可以补充约束和列定义
- `column:xxx` 可以改列名
- `TableName()` 可以改表名
