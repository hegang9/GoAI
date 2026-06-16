// Package tts 是文本转语音适配层：基于百度智能云 TTS 实现 domain/tts.Synthesizer 端口。
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	domaintts "GopherAI/internal/domain/tts"
	"GopherAI/pkg/logger"
)

// BaiduSynthesizer 基于百度 TTS 实现 domain/tts.Synthesizer 端口。
type BaiduSynthesizer struct {
	apiKey    string
	secretKey string
}

// NewBaiduSynthesizer 创建百度 TTS 合成器。
func NewBaiduSynthesizer(apiKey, secretKey string) *BaiduSynthesizer {
	return &BaiduSynthesizer{apiKey: apiKey, secretKey: secretKey}
}

// 编译期断言：BaiduSynthesizer 必须满足领域端口。
var _ domaintts.Synthesizer = (*BaiduSynthesizer)(nil)

// createRequest 百度创建任务请求体。
type createRequest struct {
	Text           string `json:"text"`
	Format         string `json:"format"`
	Voice          int    `json:"voice"`
	Lang           string `json:"lang"`
	Speed          int    `json:"speed"`
	Pitch          int    `json:"pitch"`
	Volume         int    `json:"volume"`
	EnableSubtitle int    `json:"enable_subtitle"`
}

// Create 提交文本转语音任务并返回任务 ID。
func (s *BaiduSynthesizer) Create(ctx context.Context, text string) (string, error) {
	accessToken := s.accessToken()
	if accessToken == "" {
		return "", fmt.Errorf("failed to get access token")
	}

	payload := createRequest{
		Text:           text,
		Format:         "mp3-16k",
		Voice:          4194,
		Lang:           "zh",
		Speed:          5,
		Pitch:          5,
		Volume:         5,
		EnableSubtitle: 0,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := "https://aip.baidubce.com/rpc/2.0/tts/v1/create?access_token=" + accessToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	logger.Debug("TTS Create raw response", "body", string(respBody))

	var result struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if result.TaskID == "" {
		return "", fmt.Errorf("create tts failed: empty task_id")
	}
	return result.TaskID, nil
}

// Query 查询任务状态与结果。
func (s *BaiduSynthesizer) Query(ctx context.Context, taskID string) (domaintts.TaskResult, error) {
	accessToken := s.accessToken()
	if accessToken == "" {
		return domaintts.TaskResult{}, fmt.Errorf("failed to get access token")
	}

	reqBody := map[string][]string{"task_ids": {taskID}}
	bodyBytes, _ := json.Marshal(reqBody)

	url := "https://aip.baidubce.com/rpc/2.0/tts/v1/query?access_token=" + accessToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return domaintts.TaskResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return domaintts.TaskResult{}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	logger.Debug("TTS Query raw response", "body", string(respBody))

	var rawResp struct {
		TasksInfo []struct {
			TaskID     string          `json:"task_id"`
			TaskStatus string          `json:"task_status"`
			TaskResult json.RawMessage `json:"task_result,omitempty"`
		} `json:"tasks_info"`
	}
	if err := json.Unmarshal(respBody, &rawResp); err != nil {
		return domaintts.TaskResult{}, err
	}
	if len(rawResp.TasksInfo) == 0 {
		return domaintts.TaskResult{}, fmt.Errorf("empty tasks_info")
	}

	t := rawResp.TasksInfo[0]
	result := domaintts.TaskResult{TaskID: t.TaskID, Status: t.TaskStatus}
	if t.TaskStatus == "Success" && len(t.TaskResult) > 0 {
		var r struct {
			SpeechURL string `json:"speech_url,omitempty"`
		}
		if err := json.Unmarshal(t.TaskResult, &r); err != nil {
			logger.Error("TTS parse task_result", "err", err)
			return domaintts.TaskResult{}, fmt.Errorf("failed to parse task result: %v", err)
		}
		result.SpeechURL = r.SpeechURL
	}
	return result, nil
}

// accessToken 获取百度 TTS 访问令牌。
func (s *BaiduSynthesizer) accessToken() string {
	url := "https://aip.baidubce.com/oauth/2.0/token"
	postData := fmt.Sprintf(
		"grant_type=client_credentials&client_id=%s&client_secret=%s",
		s.apiKey, s.secretKey,
	)

	resp, err := http.Post(url, "application/x-www-form-urlencoded", bytes.NewReader([]byte(postData)))
	if err != nil {
		logger.Error("TTS get token", "err", err)
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("TTS read token", "err", err)
		return ""
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		logger.Error("TTS unmarshal token", "err", err)
		return ""
	}
	return tokenResp.AccessToken
}
