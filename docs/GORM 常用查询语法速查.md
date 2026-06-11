# GORM 常用查询语法速查

这份文档整理本项目里最常见的 GORM 查询写法，重点是看懂 `dao` 层代码时经常会遇到的链式调用。

## 1. 基本查询入口

本项目的查询入口通常都是全局数据库实例：

```go
mysql.DB
```

它的类型是 `*gorm.DB`，后面可以继续链式拼接条件、排序、查询方式等。

常见结构：

```go
mysql.DB.Where(...).Order(...).Find(&list).Error
```

可以理解成：

1. 先构造查询条件
2. 再补充排序
3. 执行查询
4. 把结果写入目标变量
5. 取出执行错误

---

## 2. 查询多条：`Find`

### 典型写法

```go
var msgs []model.Message
err := mysql.DB.Find(&msgs).Error
```

含义：

- 查询 `model.Message` 对应表中的多条记录
- 结果写入 `msgs`
- 错误写入 `err`

### 本项目示例

`dao/message.go`

```go
func GetAllMessages() ([]model.Message, error) {
    var msgs []model.Message
    err := mysql.DB.Order("created_at asc").Find(&msgs).Error
    return msgs, err
}
```

这段代码表示：

- 查询所有消息
- 按 `created_at` 升序排序
- 把结果放进 `msgs`

大致对应 SQL：

```sql
SELECT * FROM messages ORDER BY created_at ASC;
```

---

## 3. 查询单条：`First`

### 典型写法

```go
var user model.User
err := mysql.DB.First(&user).Error
```

含义：

- 查询一条记录
- 通常带有 `LIMIT 1`
- 结果写入 `user`

### 本项目示例

`dao/session.go`

```go
func GetSessionByID(sessionID string) (*model.Session, error) {
    var session model.Session
    err := mysql.DB.Where("id = ?", sessionID).First(&session).Error
    return &session, err
}
```

这段代码表示：

- 按 `id = ?` 查找会话
- 只取第一条
- 结果写入 `session`

大致对应 SQL：

```sql
SELECT * FROM sessions WHERE id = ? ORDER BY id LIMIT 1;
```

---

## 4. 条件查询：`Where`

### 单条件查询

```go
err := mysql.DB.Where("session_id = ?", sessionID).Find(&msgs).Error
```

含义：

- `?` 是占位符
- 后面的 `sessionID` 会安全替换进去
- 用于筛选满足条件的记录

### 本项目示例

`dao/message.go`

```go
func GetMessagesBySessionID(sessionID string) ([]model.Message, error) {
    var msgs []model.Message
    err := mysql.DB.Where("session_id = ?", sessionID).Order("created_at asc").Find(&msgs).Error
    return msgs, err
}
```

表示：

- 只查某个会话下的消息
- 按时间升序排列

大致对应 SQL：

```sql
SELECT * FROM messages WHERE session_id = ? ORDER BY created_at ASC;
```

---

## 5. `IN` 查询

### 典型写法

```go
err := mysql.DB.Where("session_id IN ?", sessionIDs).Find(&msgs).Error
```

含义：

- 查询 `session_id` 在某个集合中的记录
- `sessionIDs` 一般是切片

### 本项目示例

`dao/message.go`

```go
func GetMessagesBySessionIDs(sessionIDs []string) ([]model.Message, error) {
    var msgs []model.Message
    if len(sessionIDs) == 0 {
        return msgs, nil
    }
    err := mysql.DB.Where("session_id IN ?", sessionIDs).Order("created_at asc").Find(&msgs).Error
    return msgs, err
}
```

表示：

- 查多个 session 下的所有消息
- 空切片时直接返回，避免无意义查询

大致对应 SQL：

```sql
SELECT * FROM messages WHERE session_id IN (?, ?, ...) ORDER BY created_at ASC;
```

---

## 6. 排序：`Order`

### 典型写法

```go
mysql.DB.Order("created_at asc").Find(&msgs)
mysql.DB.Order("created_at desc").Find(&msgs)
```

含义：

- `asc`：升序
- `desc`：降序

本项目里消息历史通常使用：

```go
Order("created_at asc")
```

因为聊天消息必须按时间顺序恢复和展示。

---

## 7. 创建记录：`Create`

### 典型写法

```go
err := mysql.DB.Create(&msg).Error
```

含义：

- 向数据库插入一条新记录
- 插入成功后，主键等自动生成字段会回填到结构体里

### 本项目示例

`dao/message.go`

```go
func CreateMessage(message *model.Message) (*model.Message, error) {
    err := mysql.DB.Create(message).Error
    return message, err
}
```

`dao/session.go`

```go
func CreateSession(session *model.Session) (*model.Session, error) {
    err := mysql.DB.Create(session).Error
    return session, err
}
```

---

## 8. 错误处理：`.Error`

GORM 的链式调用最后通常会接：

```go
.Error
```

例如：

```go
err := mysql.DB.Where("username = ?", username).First(user).Error
```

意思是取出这次数据库操作的错误。

如果成功：

```go
err == nil
```

如果失败：

```go
err != nil
```

### 本项目示例

`dao/user.go`

```go
func getUserByUsername(username string) (*model.User, error) {
    user := new(model.User)
    err := mysql.DB.Where("username = ?", username).First(user).Error
    return user, err
}
```

---

## 9. `ErrRecordNotFound` 是什么

在查单条记录时，如果数据库里没有找到对应数据，GORM 常见返回：

```go
gorm.ErrRecordNotFound
```

### 本项目示例

`dao/user.go`

```go
func IsExistUser(username string) (bool, *model.User) {
    user, err := getUserByUsername(username)
    if err == gorm.ErrRecordNotFound || user == nil {
        return false, nil
    }
    return true, user
}
```

表示：

- 如果用户不存在，就返回 `false`
- 如果找到了，就返回 `true`

---

## 10. 最常见的链式组合

### 按条件查多条

```go
err := mysql.DB.Where("session_id = ?", sessionID).Find(&msgs).Error
```

### 按条件查多条并排序

```go
err := mysql.DB.Where("session_id = ?", sessionID).Order("created_at asc").Find(&msgs).Error
```

### 按条件查单条

```go
err := mysql.DB.Where("id = ?", sessionID).First(&session).Error
```

### 直接插入

```go
err := mysql.DB.Create(&user).Error
```

---

## 11. 阅读 dao 层代码时的理解顺序

看到这类代码时：

```go
err := mysql.DB.Where("session_id = ?", sessionID).Order("created_at asc").Find(&msgs).Error
```

建议按这个顺序理解：

1. 操作的是哪个模型
2. `Where` 筛了什么条件
3. `Order` 按什么字段排序
4. `Find` 还是 `First`
5. 结果写进了哪个变量
6. 错误是不是通过 `.Error` 取出来了

---

## 12. 一句话速记

可以把常见 GORM 查询速记成：

- `Where`：筛选
- `Order`：排序
- `Find`：查多条
- `First`：查一条
- `Create`：插入
- `Error`：取错误
