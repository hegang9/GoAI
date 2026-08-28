package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"GopherAI/internal/domain/chat"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// memoryTestModel 同时充当摘要模型和主回答模型，用确定性输出验证官方中间件接入。
type memoryTestModel struct {
	mu         sync.Mutex
	calls      [][]*schema.Message
	summaryErr error
}

func (m *memoryTestModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	m.calls = append(m.calls, input)
	m.mu.Unlock()
	if len(input) > 0 && strings.Contains(input[len(input)-1].Content, "<core_memory>") {
		if m.summaryErr != nil {
			return nil, m.summaryErr
		}
		return schema.AssistantMessage(
			"<core_memory>用户偏好简洁回答</core_memory>"+
				"<session_summary>已经完成第一轮讨论，当前继续第二轮。</session_summary>", nil), nil
	}
	return schema.AssistantMessage("最终回答", nil), nil
}

func TestAutoRouterSummaryFailureFallsBackToOriginalContext(t *testing.T) {
	llm := &memoryTestModel{summaryErr: errors.New("summary unavailable")}
	repo := &memoryTestRepo{}
	router := &AutoRouterModel{
		retrieval:   NewRetrievalModifier(nil, nil, "account-1"),
		llm:         llm,
		contextRepo: repo,
		contextCfg: ContextConfig{
			Enabled:                true,
			SummaryTriggerTokens:   1,
			RecentTurns:            1,
			ToolClearTriggerTokens: 1000,
		}.normalized(),
	}
	history := []chat.Message{
		{ID: "m1", SessionID: "s1", AccountNo: "account-1", Content: "第一问", IsUser: true},
		{ID: "m2", SessionID: "s1", AccountNo: "account-1", Content: "第一答", IsUser: false},
		{ID: "m3", SessionID: "s1", AccountNo: "account-1", Content: "第二问", IsUser: true},
		{ID: "m4", SessionID: "s1", AccountNo: "account-1", Content: "第二答", IsUser: false},
		{ID: "m5", SessionID: "s1", AccountNo: "account-1", Content: "第三问", IsUser: true},
	}

	got, err := router.Generate(context.Background(), history)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got != "最终回答" {
		t.Fatalf("Generate() = %q, want 最终回答", got)
	}
	repo.mu.Lock()
	saves := repo.saves
	repo.mu.Unlock()
	if saves != 0 {
		t.Fatalf("context saves = %d, want 0", saves)
	}
}

func (m *memoryTestModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *memoryTestModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

type memoryTestRepo struct {
	mu       sync.Mutex
	snapshot chat.ContextSnapshot
	found    bool
	saves    int
}

func (r *memoryTestRepo) Get(_ context.Context, _, _ string) (chat.ContextSnapshot, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshot, r.found, nil
}

func (r *memoryTestRepo) Save(_ context.Context, snapshot chat.ContextSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshot = snapshot
	r.found = true
	r.saves++
	return nil
}

func TestAutoRouterUsesOfficialSummarizationAndPersistsLayeredMemory(t *testing.T) {
	llm := &memoryTestModel{}
	repo := &memoryTestRepo{}
	router := &AutoRouterModel{
		retrieval:   NewRetrievalModifier(nil, nil, "account-1"),
		llm:         llm,
		contextRepo: repo,
		contextCfg: ContextConfig{
			Enabled:                true,
			SummaryTriggerTokens:   1,
			RecentTurns:            1,
			ToolClearTriggerTokens: 1000,
		}.normalized(),
	}

	history := []chat.Message{
		{ID: "m1", SessionID: "s1", AccountNo: "account-1", Content: "第一问", IsUser: true},
		{ID: "m2", SessionID: "s1", AccountNo: "account-1", Content: "第一答", IsUser: false},
		{ID: "m3", SessionID: "s1", AccountNo: "account-1", Content: "第二问", IsUser: true},
		{ID: "m4", SessionID: "s1", AccountNo: "account-1", Content: "第二答", IsUser: false},
		{ID: "m5", SessionID: "s1", AccountNo: "account-1", Content: "第三问", IsUser: true},
	}
	got, err := router.Generate(context.Background(), history)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got != "最终回答" {
		t.Fatalf("Generate() = %q, want 最终回答", got)
	}

	repo.mu.Lock()
	snapshot, saves := repo.snapshot, repo.saves
	repo.mu.Unlock()
	if saves != 1 {
		t.Fatalf("context saves = %d, want 1", saves)
	}
	if snapshot.CoveredMessageID != "m2" {
		t.Fatalf("covered message = %q, want m2", snapshot.CoveredMessageID)
	}
	if snapshot.CoreMemory != "用户偏好简洁回答" {
		t.Fatalf("core memory = %q", snapshot.CoreMemory)
	}
	if snapshot.Summary != "已经完成第一轮讨论，当前继续第二轮。" {
		t.Fatalf("summary = %q", snapshot.Summary)
	}
}

func TestPrepareMemoryContextRestoresSnapshotAndMessagesAfterWatermark(t *testing.T) {
	repo := &memoryTestRepo{
		found: true,
		snapshot: chat.ContextSnapshot{
			AccountNo:        "account-1",
			SessionID:        "s1",
			CoreMemory:       "偏好中文",
			Summary:          "已讨论部署方案",
			CoveredMessageID: "m2",
			Version:          2,
		},
	}
	router := &AutoRouterModel{contextRepo: repo, contextCfg: ContextConfig{Enabled: true}.normalized()}
	history := []chat.Message{
		{ID: "m1", SessionID: "s1", AccountNo: "account-1", Content: "旧问题", IsUser: true},
		{ID: "m2", SessionID: "s1", AccountNo: "account-1", Content: "旧回答", IsUser: false},
		{ID: "m3", SessionID: "s1", AccountNo: "account-1", Content: "新问题", IsUser: true},
	}

	_, prepared := router.prepareMemoryContext(context.Background(), history)
	if len(prepared) != 3 {
		t.Fatalf("prepared messages = %d, want 3", len(prepared))
	}
	if prepared[0].Role != schema.System || !strings.Contains(prepared[0].Content, "偏好中文") {
		t.Fatalf("first prepared message should be core memory: %+v", prepared[0])
	}
	if prepared[1].Role != schema.System || !strings.Contains(prepared[1].Content, "已讨论部署方案") {
		t.Fatalf("second prepared message should be summary: %+v", prepared[1])
	}
	if prepared[2].Role != schema.User || prepared[2].Content != "新问题" {
		t.Fatalf("last prepared message should be post-watermark user message: %+v", prepared[2])
	}
}

func TestSplitSummaryPrefixKeepsRecentCompleteTurnAndCurrentUser(t *testing.T) {
	messages := []*schema.Message{
		messageWithID(schema.User, "u1", "m1"),
		messageWithID(schema.Assistant, "a1", "m2"),
		messageWithID(schema.User, "u2", "m3"),
		messageWithID(schema.Assistant, "a2", "m4"),
		messageWithID(schema.User, "u3", "m5"),
	}
	prefix, recent := splitSummaryPrefix(messages, 1)
	if len(prefix) != 2 || lastPersistentMessageID(prefix) != "m2" {
		t.Fatalf("unexpected prefix: %+v", prefix)
	}
	if len(recent) != 3 || recent[0].Content != "u2" || recent[2].Content != "u3" {
		t.Fatalf("unexpected recent messages: %+v", recent)
	}
}

func messageWithID(role schema.RoleType, content, id string) *schema.Message {
	return &schema.Message{Role: role, Content: content, Extra: map[string]any{messageIDExtraKey: id}}
}
