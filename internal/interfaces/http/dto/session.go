package dto

// SessionInfo 会话列表项的轻量视图。
type SessionInfo struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"name"`
}

// History 聊天历史记录项。
type History struct {
	IsUser  bool   `json:"is_user"`
	Content string `json:"content"`
}

type GetUserSessionsResponse struct {
	Sessions []SessionInfo `json:"sessions,omitempty"`
}

type CreateSessionRequest struct {
	UserQuestion string `json:"question" binding:"required"`
	ModelType    string `json:"modelType" binding:"required"`
	// StoredName 可选：限定只检索某来源文档；为空时不过滤。
	StoredName string `json:"storedName,omitempty"`
	// Headers 可选：限定章节路径关键字；为空时不过滤。
	Headers string `json:"headers,omitempty"`
}

type CreateSessionResponse struct {
	AiInformation string `json:"Information,omitempty"`
	SessionID     string `json:"sessionId,omitempty"`
}

type ChatSendRequest struct {
	UserQuestion string `json:"question" binding:"required"`
	ModelType    string `json:"modelType" binding:"required"`
	SessionID    string `json:"sessionId" binding:"required"`
	// StoredName 可选：限定只检索某来源文档；为空时不过滤。
	StoredName string `json:"storedName,omitempty"`
	// Headers 可选：限定章节路径关键字；为空时不过滤。
	Headers string `json:"headers,omitempty"`
}

type ChatSendResponse struct {
	AiInformation string `json:"Information,omitempty"`
}

type ChatHistoryRequest struct {
	SessionID string `json:"sessionId" binding:"required"`
}

type ChatHistoryResponse struct {
	History []History `json:"history"`
}
