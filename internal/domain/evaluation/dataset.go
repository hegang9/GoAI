// Package evaluation 定义离线评测使用的语料与金标准问题模型。
package evaluation

// Dataset 是一次评测所需的公共语料与问题集。
type Dataset struct {
	Documents         []Document
	Questions         []Question
	CorpusFingerprint string
}

// Document 是可建立索引的文本文档。
type Document struct {
	ID         string
	StoredName string
	Title      string
	Content    string
}

// Question 保存查询、金标准文档和参考答案。
type Question struct {
	ID                 string
	Query              string
	CorrectAnswer      string
	CorrectDocumentID  string
	CorrectStoredName  string
	GroundTruthContext string
}

// Candidate 是评测所需的最小检索结果视图。
type Candidate struct {
	StoredName string
	Distance   float64
}

// RetrievalTrace 保存粗筛、精排和最终返回三个阶段的候选。
type RetrievalTrace struct {
	Relevant []Candidate
	Reranked []Candidate
	Final    []Candidate
}

// IndexState 描述评测账号当前向量索引与已完成建库的指纹。
type IndexState struct {
	Exists                 bool
	CorpusFingerprint      string
	IndexConfigFingerprint string
	IndexedChunks          int
}
