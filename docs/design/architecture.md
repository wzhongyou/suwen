# 素问（Suwen）架构设计

## 1. 定位

素问 是一个开源 AI 搜索引擎，对标 Google / 百度 / Perplexity。

定位：**Ranker + 展现后端**。数据抓取（kuafu）、索引存储（vortex/proximia）、
LLM 推理（llmgate）均由周边模块提供。suwen 只做一件事：**把查询变成有据可依的答案**。

> 素问（Suwen）出自《黄帝内经·素问》——黄帝问，岐伯答。
> 两千年前定义了一种范式：人带着真问题来，智者调动全部知识储备去回答。
> 这正是 AI 搜索的本质。

---

## 2. 全景架构

```
┌──────────────────────────────────────────────────┐
│                   素问 Suwen                      │
│                                                  │
│  ┌─────────┐  ┌──────────┐  ┌────────────────┐   │
│  │  API    │  │  Ranker  │  │  LLM 网关       │   │
│  │ 网关层   │  │  编排层   │  │  (llmgate)      │   │
│  └────┬────┘  └────┬─────┘  └───────┬────────┘   │
│       │            │               │             │
│  ─ ─ ─┼────────────┼───────────────┼─────────    │
│       │     Go 主进程               │             │
│       │                            │             │
│  ┌────┴────────────────────────────┴────┐         │
│  │          混合召回引擎                 │         │
│  │  ┌──────────────┐  ┌──────────────┐  │         │
│  │  │  向量检索     │  │  倒排检索     │  │        │
│  │  │  (proximia)   │  │ (Vortex C++) │  │        │
│  │  └──────────────┘  └──────┬───────┘  │         │
│  └───────────────────────────┼──────────┘         │
└──────────────────────────────┼────────────────────┘
                               │ gRPC
                      ┌────────┴────────┐
                      │  Vortex Server  │
                      │  (C++ 独立进程)  │
                      └─────────────────┘
```

---

## 3. 技术选型

### 3.1 语言与运行时的选择

| 层 | 语言 | 形态 | 与素问进程关系 |
|---|---|---|---|
| API 网关 | Go | suwen 主进程 | — |
| Ranker 编排（query/retrieval/ranking/generation） | Go | suwen 主进程 | — |
| LLM 网关 | Go | import llmgate | 同进程 |
| 向量检索 | Go | import proximia | 同进程 |
| 倒排检索 | C++17 | gRPC 服务 | 独立进程 |
| 管理台 | TypeScript (Next.js) | 独立仓库 suwen-console | 独立进程 |

### 3.2 为什么选 Go

素问的核心工作流是：

```
接收请求 → 并发调用多个下游（Vortex、向量库、LLM）
→ 流式聚合返回
```

Go 的 goroutine + channel 就是这个模式的天选语言。一个 `go func()` 就是一个并发子查询，
代码写出来跟架构图一一对应。

除此之外：

- **单一二进制分发。** `go install` 或下载一个 binary 就能跑。开源产品的分发成本就是核心
  竞争力——不需要装虚拟环境、不需要配语言版本。更多的人会试用，更少的人在安装阶段放弃。
- **Agent 编排适合同语言。** Agent 不是独立的微服务，它只是 API 服务里的一种复杂调用模式：
  多轮循环、条件分支、工具调用。在 Go 里写一个 `AgentLoop` 的 for 循环 + tool dispatch，
  比拆到 Python 再走网络调用回来简洁得多。
- **向量库同进程同语言。** 向量库是 Go 写的，直接 import 进来，零开销。混合召回时两路并发，
  结果在内存里融合，没有序列化成本，没有网络往返。

### 3.3 为什么不选 Python

Python 唯一的优势是 LLM SDK 生态（openai、anthropic、langchain）。但这些 SDK 的本质就是
HTTP 调用 + SSE 流式解析，Go 里自己封装几百行代码的事情，不值得为此引入第二语言加上
部署复杂度的代价。

### 3.4 为什么不一开始做微服务

素问先是一个**单体 Go binary**，内部按接口做模块隔离：

```
cmd/suwen/main.go
  → query/       (查询理解：意图分类、改写、扩展)
  → retrieval/   (混合召回编排：并发调用 vortex + proximia)
  → ranking/     (多阶段排序：RRF 融合 + Cross-Encoder 精排)
  → generation/  (答案生成：片段抽取 + LLM 生成 + 引用标注)
  → gateway/     (API 路由、SSE 流式、中间件)
```

模块间通过 Go interface 解耦。未来某个模块需要独立扩容时，再拆为微服务——但不在第一天写分布式。

---

## 4. 模块设计

### 4.1 API 网关

职责：

- REST API 暴露搜索能力
- SSE 流式响应透传
- 认证、限流、日志、追踪（OpenTelemetry）

Phase 1 只做 REST + SSE。后续可加 gRPC 协议给内部服务调用。

### 4.2 查询理解 (query/)

职责：

- 接收原始查询，输出结构化查询对象
- 意图分类（factual / howto / navigational / exploratory）
- 查询改写与扩展（用 LLM 生成子查询变体）
- 确定检索策略（调整向量 vs 关键词的权重）

```go
type ParsedQuery struct {
    Raw           string
    Rewrites      []string   // 改写扩展后的子查询
    Intent        string
    Domain        string
    VectorWeight  float64
    KeywordWeight float64
}

type QueryParser interface {
    Parse(ctx context.Context, raw string) (*ParsedQuery, error)
}
```

第一版用 llmgate 调小模型做意图识别和改写（<200ms），后续可训练本地模型。

### 4.3 混合召回

职责：

- 接收查询，并发调用 Vortex（倒排）和向量库
- 融合排序：BM25F 分数 + 向量相似度 + 可能的语义重排
- 返回统一排序的候选文档列表

同进程的向量检索和 gRPC 的倒排检索并发进行：

```
ctx, cancel := context.WithTimeout(ctx, 2*time.Second)

go func() { results.bm25 = vortex.Search(ctx, query) }()
go func() { results.vector = vector.Search(ctx, embedding) }()

// 融合排序在两边都返回后（或超时后）进行
merge.Rerank(results.bm25, results.vector, strategy)
```

### 4.4 多阶段排序 (ranking/)

职责：

- 对检索候选集做精细排序
- 第一阶段（retrieval 层完成）：RRF 融合 BM25 + 向量分数，输出 100 条候选
- 第二阶段（ranking 层）：Cross-Encoder 对 Top-30 逐对精排，输出 Top-10
- 后续可加入：新鲜度衰减、域名权威性加权、用户反馈信号

```go
type Ranker interface {
    Rerank(ctx context.Context, query string, candidates []*retrieval.SearchResult) ([]*RankedResult, error)
}
```

Phase 1 可跳过精排，RRF 直接出 Top-10。

### 4.5 答案生成 (generation/)

职责：

- 从 Top-K 文档中抽取最相关片段
- 构造 prompt：强约束 "只能基于提供的资料回答，每个主张必须标注来源"
- 通过 llmgate 调 LLM，流式生成
- 维护引用索引映射，逐 token 输出给 gateway 透传前端

这是 Perplexity "没检索到的就不要说" 原则的落地模块。

### 4.6 LLM 网关

职责：

- 统一 OpenAI-compatible API 代理
- 多模型路由（按任务类型分派不同模型：便宜的做分类，强的做推理）
- 流式透传 + token 计数
- 限流、重试、fallback

直接 import `llmgate` 模块，suwen 只做配置和调用。

### 4.7 Vortex 倒排集成

Vortex 通过 REST API 独立部署。素问启动时连接它：

```
./suwen --vortex-addr=http://localhost:8080
```

协议（详见 [Vortex 索引协议](https://github.com/wzhongyou/vortex/blob/main/docs/index_protocol.md)）：

```
GET  /api/search?q=<query>&page=<N>   → 搜索（BM25F 评分，每页10条）
GET  /api/document/<id>               → 文档详情
POST /api/document                    → 添加文档
```

连不上时报错并给出启动提示。

---

## 5. 部署模式

### 5.1 开发 / 单机模式

```
./vortex_server --port=8080 --data=/var/lib/vortex
./suwen --vortex-addr=http://localhost:8080
```

两个进程，零外部依赖。向量库内嵌在 suwen 进程中，倒排由 Vortex 进程提供。

### 5.2 生产模式

```
┌──────────┐  ┌──────────┐
│  suwen   │  │  suwen   │   ← 水平扩展
└────┬─────┘  └────┬─────┘
     │             │
     ├─────────────┤
     ▼             ▼
┌──────────┐  ┌──────────┐
│ Postgres │  │  Redis   │   ← 存储 + 缓存
└──────────┘  └──────────┘
     ▲             
     │             
┌────┴─────┐
│  Vortex  │                   ← 独立扩缩
└──────────┘
```

---

## 6. 仓库组织

| 仓库 | 内容 | 语言 |
|---|---|---|
| `suwen` | Ranker（查询理解+混合召回+排序+生成）、API 网关 | Go |
| `suwen-console` | Web 搜索前端 | TypeScript (Next.js) |
| `vortex` | 倒排索引引擎 + gRPC 服务 | C++17 |
| `proximia` | 向量检索库 | Go |
| `kuafu` | 网络爬虫引擎 | Python |
| `llmgate` | LLM 网关 | Go |

---

## 7. 关键设计决策

### 7.1 向量库同进程，倒排走网络

有意识的选择。向量库是 Go 写的，同进程零开销。倒排是 C++ 写的，不同语言只能走网络。
这个不对称不是缺陷——它是务实的结果。

### 7.2 先单体后微服务

模块边界用 Go interface 定义清楚，但运行在同一进程。
拆分的触发条件是明确的扩容需求或团队分工需要，而非提前设计。

### 7.3 少依赖原则

- 不引入 Python / Node.js 运行时作为服务端依赖
- 不引入 Kubernetes 作为首要部署目标（支持单机 binary 启动）
- 不引入消息队列作为启动前提（异步任务用 goroutine + channel）

---

## 8. 下一步

### Phase 1：最小可用
1. 创建 proto 定义（suwen Search API）
2. 搭建 Go 项目骨架（cmd、5个模块划分）
3. 实现 retrieval/ — 并发调用 vortex + proximia，RRF 融合
4. 实现 generation/ — 构造 prompt → llmgate 调 LLM → 输出答案
5. 实现 gateway/ — POST /api/v1/search 同步接口
6. suwen-console 搜索首页 + 结果页

### Phase 2：搜索体验
7. query/ 加 LLM 意图分类 + 查询改写
8. ranking/ 加 Cross-Encoder 精排
9. gateway/ SSE 流式接口 + 前端流式渲染

### Phase 3：生产就绪
10. 查询缓存、限流、监控
11. 部署方案（Docker Compose / 单机 binary）
12. 评测基准

> 详细方案见 [implementation-plan.md](implementation-plan.md)
