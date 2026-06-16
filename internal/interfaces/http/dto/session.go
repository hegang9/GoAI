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
	Response
	Sessions []SessionInfo `json:"sessions,omitempty"`
}

type CreateSessionRequest struct {
	UserQuestion string `json:"question" binding:"required"`
	ModelType    string `json:"modelType" binding:"required"`
}

type CreateSessionResponse struct {
	Response
	AiInformation string `json:"Information,omitempty"`
	SessionID     string `json:"sessionId,omitempty"`
}

type ChatSendRequest struct {
	UserQuestion string `json:"question" binding:"required"`
	ModelType    string `json:"modelType" binding:"required"`
	SessionID    string `json:"sessionId" binding:"required"`
}

type ChatSendResponse struct {
	Response
	AiInformation string `json:"Information,omitempty"`
}

type ChatHistoryRequest struct {
	SessionID string `json:"sessionId" binding:"required"`
}

type ChatHistoryResponse struct {
	Response
	History []History `json:"history"`
}
