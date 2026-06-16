package dto

type TTSRequest struct {
	Text string `json:"text,omitempty"`
}

type TTSResponse struct {
	Response
	TaskID string `json:"task_id,omitempty"`
}

type QueryTTSResponse struct {
	Response
	TaskID     string `json:"task_id,omitempty"`
	TaskStatus string `json:"task_status,omitempty"`
	TaskResult string `json:"task_result,omitempty"`
}
