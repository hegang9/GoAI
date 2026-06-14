# JWT 鉴权设计

## 背景

GopherAI 是一个基于 Gin 的 AI 对话服务。除登录、注册、发送验证码等公开接口外，AI 对话、图片、文件等业务接口都要求用户先登录。项目采用 JWT（JSON Web Token）实现无状态的身份认证：用户登录后获取 token，后续请求在请求头中携带 token，服务端通过中间件统一校验并解析出用户身份。

## 目标

- 用户登录/注册成功后下发 token，凭 token 访问受保护接口。
- 服务端无需存储会话状态（无状态认证），token 自身携带用户信息与有效期。
- 鉴权逻辑集中在中间件，业务 handler 只需从上下文读取用户标识。

## 整体流程

```text
注册/登录 (公开接口)
   └─ service/user 使用 email + password 校验通过 → auth.GenerateToken(id, account_no) → 返回 token 给客户端

访问受保护接口 (/api/v1/ai、/image、/file)
   └─ jwt.Auth() 中间件
        ├─ 从 Authorization: Bearer <token> 或 ?token= 提取 token
        ├─ auth.ParseToken(token) 校验签名与有效期
        ├─ 成功 → c.Set("accountNo", accountNo) → c.Next() 进入业务 handler
        └─ 失败 → 返回 CodeInvalidToken(2006) + 401 → c.Abort()

业务 handler
   └─ c.GetString("accountNo") 获取当前用户身份，执行业务逻辑
```

## 关键设计

### 1. Token 生成（`auth/jwt.go`）

`GenerateToken` 使用 HMAC-SHA256（`SigningMethodHS256`）对称签名生成 token。自定义 `Claims` 在标准声明之外携带用户 `ID` 和 `AccountNo`：

```go
type Claims struct {
    ID       int64  `json:"id"`
    AccountNo string `json:"account_no"`
    jwt.RegisteredClaims
}
```

标准声明（`RegisteredClaims`）取自配置：

- `ExpiresAt`：当前时间 + `expire_duration` 小时（过期时间）
- `Issuer`：签发者
- `Subject`：主题
- `IssuedAt`：签发时间

签名密钥来自配置项 `jwtConfig.key`，对称密钥意味着同一把 key 既用于签名也用于验签。

### 2. Token 解析（`auth/jwt.go`）

`ParseToken` 使用同一把密钥验签，校验 `t.Valid`（包含签名正确性与过期校验）。校验通过返回 `AccountNo` 和 `true`，任何错误（格式错误、签名不匹配、已过期）都返回 `"", false`，由调用方统一按未授权处理。

### 3. 鉴权中间件（`middleware/jwt/jwt.go`）

`Auth()` 返回一个 `gin.HandlerFunc`，挂载在需要登录的路由组上。核心逻辑：

1. **提取 token**：优先从标准请求头 `Authorization: Bearer <token>` 读取；兼容从 URL 参数 `?token=` 读取（便于 SSE 流式接口等无法自定义请求头的场景）。
2. **缺失校验**：token 为空时直接返回 `CodeInvalidToken` 并 `c.Abort()`，阻止进入业务逻辑。
3. **解析校验**：调用 `auth.ParseToken`，失败同样返回 `CodeInvalidToken` 并中止。
4. **注入身份**：成功后通过 `c.Set("accountNo", accountNo)` 把账号编号写入 Gin 上下文，再 `c.Next()` 放行。

中间件还使用 `common/logger` 打印 token 前缀用于调试（避免完整泄露）。

### 4. 路由挂载（`router/router.go`）

中间件只在统一的鉴权分组上挂载一次，所有子组自动继承：

```go
v1 := r.Group("/api/v1")

// 公开路由，不需要鉴权
RegisterUserRouter(v1.Group("/user"))

// JWT 鉴权路由组 —— 中间件只挂载一次，子组自动继承
auth := v1.Group("")
auth.Use(jwt.Auth())
{
    RegisterAIRouter(auth.Group("/ai"))
    RegisterImageRouter(auth.Group("/image"))
    RegisterFileRouter(auth.Group("/file"))
}
```

这种方式避免了在每个受保护分组上重复调用 `.Use(jwt.Auth())`，新增受保护模块只需挂到 `auth` 组下。

### 5. 业务层读取身份

进入业务 handler 后，统一通过 `c.GetString("accountNo")` 获取当前用户。例如 `controller/session.go`、`controller/file.go` 都依赖中间件注入的 `accountNo` 进行后续业务（会话、文件等）。`controller/file.go` 还额外判断 `accountNo` 为空时返回 `CodeInvalidToken`，作为防御性兜底。

## 配置说明

JWT 相关配置位于 `config/config.toml` 的 `[jwtConfig]` 段，由 `config.JwtConfig` 映射：

| 配置项 | 字段 | 说明 |
| --- | --- | --- |
| `expire_duration` | `ExpireDuration` | Token 过期时长（小时） |
| `issuer` | `Issuer` | 签发者标识 |
| `subject` | `Subject` | 主题 |
| `key` | `Key` | HMAC 对称签名密钥 |

通过 `config.GetConfig()` 单例懒加载读取。

## 错误码

鉴权失败统一使用 `common/code` 中的错误码：

- `CodeInvalidToken (2006)`：Token 无效（缺失、格式错误、签名失败、过期等），HTTP 映射为 `401 Unauthorized`。
- `CodeNotLogin (2007)`：用户未登录，同样映射为 `401 Unauthorized`。

错误响应体为 `dto.Response`，包含 `status_code` 与 `status_msg`。

## 风险与限制

- **对称密钥**：使用 HS256 对称密钥，签名与验签共用一把 key，密钥泄露即可伪造任意 token；密钥必须妥善保管，避免提交到仓库（当前 `config.toml` 中为示例值）。
- **无吊销机制**：JWT 无状态，token 一旦签发，在过期前无法主动失效（无黑名单/吊销列表），无法实现"登出即失效"或"强制下线"。
- **过期时长偏长**：默认 `expire_duration = 8760`（约一年），实际生产建议缩短并配合刷新机制。
- **URL 传 token**：兼容 `?token=` 便于流式接口，但 token 可能被记录在访问日志、浏览器历史中，存在泄露风险，应限制使用场景。
- **无刷新令牌**：当前仅有 access token，没有 refresh token 机制，过期后需重新登录。

## 涉及文件

- `auth/jwt.go`：Token 生成与解析。
- `middleware/jwt/jwt.go`：Gin 鉴权中间件。
- `router/router.go`：中间件挂载与路由分组。
- `service/user/user.go`：登录/注册成功后下发 token。
- `config/config.go` 与 `config/config.toml`：JWT 配置。
- `common/code/code.go`：鉴权相关错误码与 HTTP 状态映射。
