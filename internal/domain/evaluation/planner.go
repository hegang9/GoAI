package evaluation

// PlannerDataset 是一次 Planner 离线评测所使用的已解析用例集。
type PlannerDataset struct {
	Cases []PlannerCase
}

// PlannerCase 描述一条检索决策评测用例。
type PlannerCase struct {
	ID          string
	Split       string
	Category    string
	History     []PlannerMessage
	LastMessage string
	Expect      PlannerExpectation
}

// PlannerMessage 是评测用的单条对话历史消息。
type PlannerMessage struct {
	Role    string
	Content string
}

// PlannerExpectation 是 Planner 用例的金标准。
type PlannerExpectation struct {
	NeedRetrieval bool
	DocFilter     PlannerDocFilter
	QueryKeywords []string
}

// PlannerDocFilter 是预期提取的显式文档范围。
type PlannerDocFilter struct {
	StoredName string
	Headers    string
}

// PlannerDecision 是 Planner 对评测用例做出的决策视图。
type PlannerDecision struct {
	NeedRetrieval  bool
	RetrievalQuery string
	DocFilter      PlannerDocFilter
	Source         string
	Confidence     string
}
