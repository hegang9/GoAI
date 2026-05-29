package dto

type RecognizeImageResponse struct {
	Response
	ClassName string `json:"class_name,omitempty"`
}
