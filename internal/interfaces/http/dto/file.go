package dto

type UploadFileResponse struct {
	Response
	FilePath string `json:"file_path,omitempty"`
}

// DeleteRagFilesRequest 批量删除 RAG 文档的请求体。
// filenames 为待删除文档的存储文件名（上传响应 file_path 的 basename）。
type DeleteRagFilesRequest struct {
	Filenames []string `json:"filenames" binding:"required,min=1"`
}

// DeleteRagFilesResponse 批量删除 RAG 文档的响应，deleted 为实际成功删除的文件名列表。
type DeleteRagFilesResponse struct {
	Response
	Deleted []string `json:"deleted"`
}

// ListRagFilesResponse 列出当前账号已上传 RAG 文档的响应，files 为文档存储文件名列表。
type ListRagFilesResponse struct {
	Response
	Files []string `json:"files"`
}
