package test

import (
	"context"
	"testing"

	"GopherAI/internal/domain/chat"
)

// fakeModel 是 chat.Model 的测试替身，返回固定内容并记录收到的历史长度。
type fakeModel struct {
	reply       string
	lastHistory int
}

func (m *fakeModel) Generate(ctx context.Context, history []chat.Message) (string, error) {
	m.lastHistory = len(history)
	return m.reply, nil
}

func (m *fakeModel) Stream(ctx context.Context, history []chat.Message, cb chat.StreamCallback) (string, error) {
	m.lastHistory = len(history)
	cb(m.reply)
	return m.reply, nil
}

func (m *fakeModel) Type() string { return "fake" }

// fakeSink 是 chat.MessageSink 的测试替身，记录被持久化的消息。
type fakeSink struct {
	saved []chat.Message
}

func (s *fakeSink) Save(msg chat.Message) error {
	s.saved = append(s.saved, msg)
	return nil
}

// TestConversationGeneratePersistsUserAndAIMessages 校验会话聚合在生成回复时
// 会按顺序追加用户消息与 AI 回复，并通过 Sink 持久化两条消息。
func TestConversationGeneratePersistsUserAndAIMessages(t *testing.T) {
	model := &fakeModel{reply: "hello from ai"}
	sink := &fakeSink{}
	conv := chat.NewConversation(model, "session-1", sink)

	content, err := conv.Generate(context.Background(), "acc-1", "你好", chat.RAGFilter{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if content != "hello from ai" {
		t.Fatalf("unexpected content: %q", content)
	}

	messages := conv.Messages()
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if !messages[0].IsUser || messages[0].Content != "你好" {
		t.Errorf("first message should be user question, got %+v", messages[0])
	}
	if messages[1].IsUser || messages[1].Content != "hello from ai" {
		t.Errorf("second message should be AI reply, got %+v", messages[1])
	}

	// 模型生成时应能看到已追加的用户消息（历史长度为 1）。
	if model.lastHistory != 1 {
		t.Errorf("expected model to receive 1 history message, got %d", model.lastHistory)
	}

	// 用户消息与 AI 回复都应被持久化。
	if len(sink.saved) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(sink.saved))
	}
}

// TestConversationAddMessageWithoutPersist 校验回放场景（persist=false）只重建内存、不触发持久化。
func TestConversationAddMessageWithoutPersist(t *testing.T) {
	sink := &fakeSink{}
	conv := chat.NewConversation(&fakeModel{}, "session-2", sink)

	if err := conv.AddMessage("历史消息", "acc-1", true, false); err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}
	if len(conv.Messages()) != 1 {
		t.Fatalf("expected 1 in-memory message")
	}
	if len(sink.saved) != 0 {
		t.Fatalf("replay should not persist, but got %d saved", len(sink.saved))
	}
}

// TestConversationRestoreMessagePreservesID 校验冷会话回放不会重新生成消息 ID，
// 从而保证会话摘要保存的覆盖水位在服务重启后仍可定位。
func TestConversationRestoreMessagePreservesID(t *testing.T) {
	conv := chat.NewConversation(&fakeModel{}, "session-3", &fakeSink{})
	conv.RestoreMessage(chat.Message{
		ID:        "persisted-message-id",
		SessionID: "session-3",
		AccountNo: "acc-1",
		Content:   "历史回答",
		IsUser:    false,
	})

	messages := conv.Messages()
	if len(messages) != 1 || messages[0].ID != "persisted-message-id" {
		t.Fatalf("restored messages = %+v", messages)
	}
}
