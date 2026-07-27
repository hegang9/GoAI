# RAGBench 使用说明

`ragbench` 使用本地的 watsonxDocsQA 数据集评测 RAG 检索质量。默认评测账号为 `95829666279`。

## 准备

数据集目录应为：

```text
dataset/watsonxDocsQA/
├── corpus/train-00000-of-00001.parquet
└── question_answers/
    ├── train-00000-of-00001.parquet
    └── test-00000-of-00001.parquet
```

请先确认应用配置中的 Redis、Embedding API 可用；启用 reranker 时也需要其服务可用。

先校验数据集，不会连接 Redis 或调用模型：

```bash
go run ./cmd/ragbench -validateOnly
```

## 常用命令

```bash
# 用 test 集完整评测；首次运行会自动建库
go run ./cmd/ragbench -split test

# 用 train 集调参
go run ./cmd/ragbench -split train

# 仅评测前 3 个问题，适合连通性检查
go run ./cmd/ragbench -split test -limit 3

# 指定数据集目录或测试账号
go run ./cmd/ragbench -datasetDir E:\dataset\watsonxDocsQA -accountNo 95829666279

# 忽略现有索引状态并强制重建
go run ./cmd/ragbench -split test -reindex=true
```

`train` 有 45 个问题，适合调参；`test` 有 30 个问题，建议用于最终验收。

## 索引复用规则

默认不需要传 `-reindex`。工具会自动检查索引状态：

- 仅修改检索期配置时复用已有索引，例如 TopK、距离阈值、召回数量、上下文窗口和 reranker 参数。
- 修改 embedding、向量维度、分块、语义切块、标题注入或语料时，自动重建索引。
- 索引或完成标记缺失、索引内容不完整时，自动重建索引。

`-reindex=true` 会清空指定账号的向量索引后重新建立完整语料索引。请仅对测试账号使用。

## 结果阅读

每个问题会输出是否命中金标准文档及其文档级排名。结尾汇总字段：

- `document_recall`：金标准文档被召回的比例，越高越好。
- `empty_rate`：没有检索结果的比例，越低越好。
- `document_mrr`：金标准文档排名质量，越高越好。
- `rerank_gain`：启用重排后排名提升的比例；没有可比较样本时为 `0`。
- `RESULT: PASS`：达到门槛；默认要求召回率不低于 `0.80` 且空召回率不高于 `0.10`。

可通过参数调整门槛：

```bash
go run ./cmd/ragbench -minRecall 0.85 -maxEmptyRate 0.05
```

退出码：`0` 为通过，`1` 为评测完成但未达到门槛，`2` 为数据、服务连接或运行错误。
