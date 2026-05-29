package dto

type UploadFileResponse struct {
	Response
	FilePath string `json:"file_path,omitempty"`
}
