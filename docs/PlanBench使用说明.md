# PlanBench 使用说明

`planbench` 用于观察 Planner 的检索决策、检索 query 改写和显式文档/章节 filter 提取质量。
它不会连接 Redis，也不会建立向量索引；运行时会真实调用 `[plannerConfig]` 配置的 Planner 模型。

## 准备

- 本地存在 watsonxDocsQA 数据集：`dataset/watsonxDocsQA`。
- `config/config.toml` 中 `[plannerConfig].enabled = true`，并配置可用的模型、Base URL 与 API Key。
- 从项目根目录运行命令。
- 使用专用评测账号。工具会在 `uploads/{accountNo}` 临时创建占位文件；该目录已存在且非空时会拒绝运行。

先校验双源测试集，不调用模型：

```bash
go run ./cmd/planbench -validateOnly -split test
```

## 运行

```bash
# 默认运行 test split，账号为 planner_bench
go run ./cmd/planbench

# 运行 train split
go run ./cmd/planbench -split train

# 只运行前 3 条，适合连通性检查
go run ./cmd/planbench -split test -limit 3

# 指定 watsonx 数据集、金标准文件和专用账号
go run ./cmd/planbench -datasetDir E:\dataset\watsonxDocsQA -evalset testdata/planbench/evalset.jsonl -accountNo planner_bench
```

## 测试集

金标准文件为 `testdata/planbench/evalset.jsonl`，随仓库提交。它通过 `source` 区分两种来源：

- `watsonxDocsQA`：引用原始 `question_id`，作为英文单轮应检索正例。
- `manual`：通用企业中文样本，覆盖闲聊/常识、模糊问题、多轮指代和显式文件或章节范围。

可用分类为：`watsonx_single_turn`、`no_retrieval`、`ambiguous`、`multiturn_reference`、`explicit_filter`。

## 结果

每条用例会输出实际与期望的检索决策、query、filter 和 ROUGE-L。汇总包括：

- `misjudge_rate`：是否检索的误判比例。
- `filter_accuracy`：`storedName` 与 `headers` 同时精确匹配的比例。
- `rougeL_avg`：应检索样本的 query 改写相似度。
- `category=...`：按用例分类的上述观察指标。

当前不设质量门禁：评测成功完成即退出码 `0`；数据、配置或模型调用准备失败时退出码为 `2`。
