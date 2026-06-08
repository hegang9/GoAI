package converter

import (
	"GopherAI/bo"
	"GopherAI/dto"
)

//func UserModelToBO(user *model.User, token string) bo.UserBO {
//	return bo.UserBO{Token: token}
//}
//
//func UserBOToLoginResponse(userBO bo.UserBO) dto.LoginResponse {
//	return dto.LoginResponse{Token: userBO.Token}
//}
//
//func UserBOToRegisterResponse(userBO bo.UserBO) dto.RegisterResponse {
//	return dto.RegisterResponse{Token: userBO.Token}
//}

func SessionInfoBOsToDTO(sessionBOs []bo.SessionInfoBO) []dto.SessionInfo {
	sessions := make([]dto.SessionInfo, 0, len(sessionBOs))
	for _, sessionBO := range sessionBOs {
		sessions = append(sessions, dto.SessionInfo{
			SessionID: sessionBO.SessionID,
			Title:     sessionBO.Title,
		})
	}
	return sessions
}

func AIResponseBOToCreateSessionResponse(aiBO bo.AIResponseBO) dto.CreateSessionResponse {
	return dto.CreateSessionResponse{
		AiInformation: aiBO.Content,
		SessionID:     aiBO.SessionID,
	}
}

func AIResponseBOToChatSendResponse(aiBO bo.AIResponseBO) dto.ChatSendResponse {
	return dto.ChatSendResponse{AiInformation: aiBO.Content}
}

func MessageBOsToHistoryDTO(messageBOs []bo.MessageBO) []dto.History {
	history := make([]dto.History, 0, len(messageBOs))
	for _, messageBO := range messageBOs {
		history = append(history, dto.History{
			IsUser:  messageBO.IsUser,
			Content: messageBO.Content,
		})
	}
	return history
}

func ImageResultBOToResponse(imageBO bo.ImageResultBO) dto.RecognizeImageResponse {
	return dto.RecognizeImageResponse{ClassName: imageBO.ClassName}
}

func FileBOToUploadResponse(fileBO bo.FileBO) dto.UploadFileResponse {
	return dto.UploadFileResponse{FilePath: fileBO.FilePath}
}

func TTSResultBOToResponse(ttsBO bo.TTSResultBO) dto.TTSResponse {
	return dto.TTSResponse{TaskID: ttsBO.TaskID}
}

func TTSResultBOToQueryResponse(ttsBO bo.TTSResultBO) dto.QueryTTSResponse {
	return dto.QueryTTSResponse{
		TaskID:     ttsBO.TaskID,
		TaskStatus: ttsBO.TaskStatus,
		TaskResult: ttsBO.SpeechURL,
	}
}
