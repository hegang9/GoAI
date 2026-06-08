package bo

type SessionInfoBO struct {
	SessionID string
	Title     string
}

type AIResponseBO struct {
	SessionID string
	Content   string
}

type MessageBO struct {
	IsUser  bool
	Content string
}
