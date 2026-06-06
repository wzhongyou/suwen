# 素问（Suwen）实现方案

## 1. 定位明确

```
suwen = Ranker + 展现后端
```

suwen 不做数据抓取（kuafu）、不做索引存储（vortex/proximia）、不做 LLM 推理（llmgate）。
**suwen 只做一件事：把查询变成答案。** 也就是连接“已有基础设施”和“用户看到的结果”之间的全部逻辑。

对比成熟产品，suwen 对标的是：

| 产品 | suwen 对应模块 |
|------|---------------|
| Perplexity 的 RAG Pipeline | 查询理解 + 混合召回编排 + 排序 + 生成 |
| Google 的 Ranking Stack | 多阶段排序（RRF → 精排） |
| Perplexity 的 Answer API | gateway/（Search API + SSE 流式） |

---

## 2. 模块架构

```
用户查询 "怎么提高数据库性能"
    │
    ▼
┌─────────────────────────────────────────────────────┐
│                 suwen Go 主进程                       │
│                                                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────┐ │
│  │  query   │  │retrieval │  │ ranking  │  │ gene- │ │
│  │ 查询理解  │→│ 混合召回  │→│ 多阶段排序│→│ ration│ │
│  │          │  │ 编排     │  │          │  │ 生成  │ │
│  └──────────┘  └────┬─────┘  └──────────┘  └──┬───┘ │
│                     │                         │     │
│          ┌──────────┼──────────┐    ┌─────────┤     │
│          ▼          ▼          │    ▼         ▼     │
│     ┌────────┐ ┌────────┐      │  ┌────────────────┐ │
│     │ vortex │ │proximia│      │  │   llmgate      │ │
│     │ gRPC   │ │  import│      │  │   import       │ │
│     └────────┘ └────────┘      │  └────────────────┘ │
│                                │                     │
│  ┌─────────────────────────────┴───────────────────┐ │
│  │              gateway/ (API层)                    │ │
│  │    POST /api/search      搜索接口                │ │
│  │    GET  /api/search/stream  SSE流式              │ │
│  │    gRPC SearchService      (对内)               │ │
│  └─────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────┐
│           suwen-console (TypeScript, Next.js)        │
│                                                      │
│   搜索首页 / 结果页 / 搜索历史 / 设置                 │
└─────────────────────────────────────────────────────┘
```

---

## 3. 模块详细设计

### 3.1 query/ — 查询理解

**职责**：接收原始查询，输出结构化查询对象

```
"怎么提高数据库性能"
    → 意图: technical_howto | 领域: database
    → 改写: ["数据库性能优化方法", "SQL调优", "数据库索引优化"]
    → 类型: 探索型（提高向量权重）
```

**接口定义**：

```go
// query/query.go

type ParsedQuery struct {
    Raw           string     // 原始查询
    Rewrites      []string   // 改写扩展后的子查询（含原始）
    Intent        string     // 意图分类: factual / howto / navigational / exploratory
    Domain        string     // 领域标签
    SearchType    string     // navigational / exploratory / mixed
    VectorWeight  float64    // 向量检索权重 (0.2~0.8)
    KeywordWeight float64    // 关键词检索权重
}

type QueryParser interface {
    Parse(ctx context.Context, raw string) (*ParsedQuery, error)
}
```

**实现策略**：
- **第一版**：规则 + 小模型。用 llmgate 调便宜的模型（如 gpt-4.1-mini）做意图分类和扩展，prompt 工程为主
- **后续优化**：训练专用分类模型，本地推理 < 50ms
- **超时**：200ms，超时降级为直接使用原始查询

### 3.2 retrieval/ — 混合召回编排

**职责**：并发调用 vortex 和 proximia，融合返回候选集

```go
// retrieval/retrieval.go

type SearchResult struct {
    DocID    string
    Title    string
    Snippet  string
    URL      string
    BM25Score    float64
    VectorScore  float64
    FinalScore   float64
}

type Searcher interface {
    // HybridSearch 并发执行多路检索并融合
    HybridSearch(ctx context.Context, pq *query.ParsedQuery) ([]*SearchResult, error)
}
```

**实现细节**：

```go
func (s *searcher) HybridSearch(ctx context.Context, pq *query.ParsedQuery) ([]*SearchResult, error) {
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()

    var (
        bm25Results   []*SearchResult
        vectorResults []*SearchResult
        wg            sync.WaitGroup
    )
    
    wg.Add(2)
    
    // 并发1: vortex 倒排（通过 gRPC）
    go func() {
        defer wg.Done()
        bm25Results = callVortex(ctx, pq.Rewrites)
    }()
    
    // 并发2: proximia 向量（同进程 import）
    go func() {
        defer wg.Done()
        embedding := getEmbedding(ctx, pq.Raw)
        vectorResults = proximia.Search(ctx, embedding, topK=100)
    }()
    
    wg.Wait()
    
    // RRF 融合
    return rrf.Fuse(bm25Results, vectorResults, rrf.Params{
        K:             60,
        KeywordWeight: pq.KeywordWeight,
        VectorWeight:  pq.VectorWeight,
        Limit:         100,
    }), nil
}
```

**关键设计**：
- vortex 走 gRPC（独立C++进程），proximia 同进程 import
- 两路并发，2 秒超时，超时不阻塞另一路
- 每路召回 50-100 条，RRF 融合后保留 100 条给排序层

### 3.3 ranking/ — 多阶段排序

**职责**：对候选集做精细排序

**两阶段设计**：

```go
// ranking/ranking.go

type Ranker interface {
    // Rerank 对候选集做精排
    Rerank(ctx context.Context, query string, candidates []*retrieval.SearchResult) ([]*RankedResult, error)
}

type RankedResult struct {
    retrieval.SearchResult
    RerankScore float64
    Rank        int
}
```

**第一阶段：RRF 融合（retrieval 层已完成）**
- 100 条候选 → 按 RRF 分数排序

**第二阶段：Cross-Encoder 精排（ranking 层）**
- 取 RRF Top-30 → Cross-Encoder 逐对打分 → 返回 Top-10
- 模型选择：bge-reranker-v2-m3（开源SOTA，Go 通过 ONNX runtime 或 HTTP 调 Python sidecar）
- 第一版可先跳过精排，RRF 直接出 Top-10

**后续可加入的信号**：
- 新鲜度衰减（按文档时间指数衰减）
- 来源权威性加权（可配置的域名权重）
- 用户点击反馈（长期优化）

### 3.4 generation/ — 答案生成

**职责**：取 Top-K 文档片段 → 构造 prompt → LLM 生成 → 流式输出

```go
// generation/generation.go

type Generator interface {
    // Generate 流式生成答案
    Generate(ctx context.Context, query string, docs []*ranking.RankedResult) (<-chan Token, error)
}

type Token struct {
    Text   string // 文本片段
    Finish bool   // 是否结束
    Citations []Citation // 引用映射
}

type Citation struct {
    Index int
    URL   string
    Title string
}
```

**Prompt 构造**：

```
System:
你是素问，一个AI搜索引擎。你只能基于提供的【参考资料】回答问题。
对于每个事实性主张，标注来源编号 [1] [2]。
如果参考资料不包含答案，直说"未找到相关信息"，不要编造。

User:
问题：{user_query}

参考资料：
[1] {doc1_title} ({doc1_url})
{doc1_snippet}

[2] {doc2_title} ({doc2_url})
{doc2_snippet}
...
```

**关键设计**：
- llmgate 统一调用，模型可配置
- 流式 SSE 透传给前端
- 引用索引映射在 generation 层维护

### 3.5 gateway/ — API 层

**职责**：对外暴露搜索接口

```protobuf
// api/suwen.proto

service SearchService {
  // 标准搜索（等全部结果后返回）
  rpc Search(SearchRequest) returns (SearchResponse);
  
  // 流式搜索（边生成边返回，SSE）
  rpc StreamSearch(SearchRequest) returns (stream SearchEvent);
}

message SearchRequest {
  string query = 1;
  int32 max_results = 2;
  bool stream = 3;
}

message SearchResponse {
  string answer = 1;
  repeated Source sources = 2;
  repeated RelatedQuery related = 3;
}

message SearchEvent {
  oneof event {
    StatusEvent status = 1;    // "正在理解问题..." / "正在检索..."
    TokenEvent token = 2;      // 答案文本片段
    SourceEvent sources = 3;   // 引用来源
    DoneEvent done = 4;        // 完成
  }
}
```

**REST 端点**：
```
POST /api/v1/search           → 200 { answer, sources }
GET  /api/v1/search/stream    → SSE text/event-stream
```

**SSE 事件流**（向 Perplexity 看齐）：
```
event: status
data: {"stage":"understanding","message":"正在理解问题..."}

event: status  
data: {"stage":"retrieving","message":"找到 23 篇相关资料"}

event: token
data: {"text":"数据库性能优化"}

event: token  
data: {"text":"主要从以下几个方面入手"}

event: citation
data: {"index":1,"url":"...","title":"..."}

event: done
data: {"total_ms":1200}
```

---

## 4. 数据流全景

```
用户输入 "怎么提高数据库性能"
    │
    ▼
┌─ query/ ──────────────────────────────────────
│  Parse("怎么提高数据库性能")
│  → 意图: howto, 领域: database
│  → 改写: ["数据库性能优化方法","SQL调优实践","数据库索引优化"]
│  耗时: ~150ms (LLM)
└──────────┬────────────────────────────────────
           ▼
┌─ retrieval/ ─────────────────────────────────
│  ┌─ goroutine 1: vortex.Search(3个改写查询) ─┐
│  │  每个查询返回 Top-30                       │
│  │  → BM25F 打分                             │
│  └─────────────────  ~300ms ─────────────────┘
│  ┌─ goroutine 2: proximia.Search(embedding) ─┐
│  │  HNSW 语义检索 Top-100                     │
│  │  → 余弦相似度打分                          │
│  └─────────────────  ~100ms ─────────────────┘
│  
│  RRF(k=60) 融合 → 100条候选
│  耗时: ~350ms (并发取max)
└──────────┬────────────────────────────────────
           ▼
┌─ ranking/ ───────────────────────────────────
│  Cross-Encoder 精排 Top-30 → Top-10
│  耗时: ~150ms (ONNX/HTTP)
└──────────┬────────────────────────────────────
           ▼
┌─ generation/ ────────────────────────────────
│  取 Top-5 文档片段 → 构造 Prompt
│  llmgate.Chat(stream=true) → SSE 逐 token 输出
│  首 token: ~500ms, 后续: 流式
└──────────┬────────────────────────────────────
           ▼
      SSE → 前端逐字渲染
```

**总延迟目标**：首 token < 2s，完整答案 < 8s

---

## 5. 项目结构

```
suwen/
├── cmd/
│   └── suwen/
│       └── main.go              # 入口，组装各模块
├── query/                       # 查询理解
│   ├── parser.go                # 接口定义 + 实现
│   └── parser_test.go
├── retrieval/                   # 混合召回编排
│   ├── searcher.go              # 接口 + 并发编排
│   ├── rrf.go                   # RRF 融合算法
│   ├── vortex_client.go         # vortex gRPC 客户端
│   └── searcher_test.go
├── ranking/                     # 多阶段排序
│   ├── ranker.go                # 接口 + 精排实现
│   └── ranker_test.go
├── generation/                  # 答案生成
│   ├── generator.go             # 接口 + LLM 调用
│   ├── prompt.go                # Prompt 模板
│   └── generator_test.go
├── gateway/                     # API 层
│   ├── handler.go               # HTTP handler
│   ├── sse.go                   # SSE 流式辅助
│   └── middleware.go            # 认证/限流/日志
├── api/
│   └── suwen.proto              # Proto 定义
├── config/
│   └── config.go                # 配置结构
├── go.mod
├── go.sum
└── README.md
```

**suwen-console (独立仓库)**：
```
suwen-console/
├── app/                         # Next.js App Router
│   ├── page.tsx                 # 搜索首页
│   ├── search/page.tsx          # 搜索结果页
│   └── layout.tsx
├── components/
│   ├── SearchBox.tsx            # 搜索框
│   ├── AnswerCard.tsx           # 答案卡片（流式渲染）
│   ├── SourceList.tsx           # 引用来源列表
│   └── RelatedQueries.tsx       # 相关搜索
├── lib/
│   └── api.ts                   # suwen API 客户端
└── package.json
```

---

## 6. 实施路线

### Phase 1：最小可用（2周）

目标：输入 query → 出答案（哪怕很简陋）

- [ ] `cmd/suwen/main.go` 骨架 + 配置加载
- [ ] `query/` — 直接透传原始查询（不做改写），后续加模型
- [ ] `retrieval/` — 并发调 vortex + proximia，RRF 融合
- [ ] `generation/` — 构造 prompt → llmgate 调 LLM → 打印结果
- [ ] `gateway/` — 一个 `POST /api/v1/search` 同步接口
- [ ] `suwen-console` — 一个输入框 + 结果显示页

### Phase 2：搜索体验（2周）

- [ ] `query/` — 加 LLM 意图分类 + 查询改写
- [ ] `ranking/` — 加 Cross-Encoder 精排
- [ ] `gateway/` — SSE 流式接口 + 状态事件
- [ ] 前端流式渲染 + 引用展示

### Phase 3：生产就绪（2周）

- [ ] 查询缓存（相同/相似查询直接返回）
- [ ] 限流 + 认证
- [ ] 监控（延迟分布、召回率、LLM 成本）
- [ ] 部署方案（Docker Compose / 单机 binary）
- [ ] 评测基准（回答质量人工评估）

---

## 7. 配置结构

```toml
# suwen.toml

[server]
addr = ":8080"

[vortex]
addr = "localhost:9090"

[llm]
provider = "openai"     # 通过 llmgate 路由
model = "deepseek-v4-flash"
timeout = "30s"

[retrieval]
timeout = "2s"
rrf_k = 60

[ranking]
cross_encoder_enabled = false  # Phase 2 启用
cross_encoder_model = "bge-reranker-v2-m3"
```

---

## 8. 关键设计决策

1. **Ranker 同进程，不回网络**：查询理解→召回编排→排序→生成全在 Go 主进程，模块间纯内存传递。只有 vortex 走 gRPC（C++ 语言边界）。

2. **先同步后流式**：Phase 1 只做同步接口快速验证链路，Phase 2 再加 SSE。

3. **先规则后模型**：查询理解先用规则 + prompt 搞定，效果跑通了再看要不要训小模型。

4. **proximia 要补的能力**：目前 proximia 的混合搜索在同一引擎内（BM25+向量），但我们的场景是跨引擎混合（vortex 倒排 + proximia 向量）。需要在 retrieval 层做 RRF 融合，不需要 proximia 改代码。

5. **vortex 要确认的能力**：gRPC 服务化是否完成？还是目前只有 embed 模式？如果是后者，Phase 1 需要先给 vortex 包一个 gRPC 服务层。
