package bo

type UserBO struct {
	Token string
}

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

type ImageResultBO struct {
	ClassName string
}

type FileBO struct {
	FilePath string
}

type TTSResultBO struct {
	TaskID     string
	TaskStatus string
	SpeechURL  string
}
