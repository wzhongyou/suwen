# 素问 Suwen

AI 驱动的开源搜索引擎 —— 混合召回，多步推理，有据可答。

> 素问出自《黄帝内经·素问》——黄帝问，岐伯答。两千年前定义了问答的范式：人带着真问题来，智者调动全部知识储备去回答。这正是 AI 搜索的本质。

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.25-blue)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## 架构

```
用户查询 → 查询理解 → 混合召回(vortex + proximia) → 排序 → LLM生成 → 答案+引用
```

Suwen 只做 **Ranker + 展现后端**。数据抓取（[kuafu](https://github.com/wzhongyou/kuafu)）、索引存储（[vortex](https://github.com/wzhongyou/vortex) / [proximia](https://github.com/wzhongyou/proximia)）、LLM 推理（[llmgate](https://github.com/wzhongyou/llmgate)）均由周边模块提供。

详细架构见 [docs/design/architecture.md](docs/design/architecture.md)。

## 快速开始

### 前置依赖

需要先启动以下服务：

| 服务 | 端口 | 说明 |
|------|------|------|
| [Vortex](https://github.com/wzhongyou/vortex) | 9527 | 倒排索引（关键词检索） |
| [Proximia](https://github.com/wzhongyou/proximia) | 9876 | 向量检索（语义检索） |

### 安装运行

```bash
# 克隆
git clone https://github.com/wzhongyou/suwen.git
cd suwen

# 配置 LLM（编辑 conf/llmgate.toml 填入 API key）
# 或设置环境变量:
export DEEPSEEK_KEY=sk-your-key

# 构建
make build

# 启动
./suwen --config=conf/suwen.toml \
  --vortex-addr=http://localhost:9527 \
  --proximia-addr=http://localhost:9876
```

打开 `http://localhost:9090` 开始搜索。

### 配置

```toml
# conf/suwen.toml

[server]
addr = ":9090"

[vortex]
addr = "http://localhost:9527"

[proximia]
addr = "http://localhost:9876"

[llm]
provider = "deepseek"
model = "deepseek-v4-flash"
timeout = "30s"
config_path = "conf/llmgate.toml"

[retrieval]
timeout = "2s"
rrf_k = 60
```

## API

### 搜索

```bash
POST /api/v1/search
Content-Type: application/json

{"query": "Go语言并发怎么实现"}
```

<details>
<summary>响应示例</summary>

```json
{
  "answer": "Go 语言的并发模型基于 goroutine 和 channel...",
  "sources": [
    {"index": 1, "url": "https://go.dev/tour/", "title": "Go 语言并发编程"}
  ],
  "results": [
    {
      "doc_id": "10000",
      "title": "Go 语言并发编程",
      "url": "https://go.dev/tour/",
      "snippet": "Go 并发编程模型详解",
      "bm25_score": 5.46,
      "final_score": 0.0082,
      "Rank": 1
    }
  ],
  "time_ms": 2206
}
```
</details>

### 调试检索（不含 LLM）

```bash
POST /api/v1/search/debug

{"query": "并发"}
```

### SSE 流式（Phase 2）

```bash
GET /api/v1/search/stream?q=并发
```

## 项目结构

```
suwen/
├── cmd/suwen/main.go           # 入口
├── internal/                    # 应用内部包
│   ├── config/                 # 配置定义与加载
│   ├── query/                  # 查询理解（意图分类、改写）
│   ├── retrieval/              # 混合召回编排、RRF 融合
│   ├── ranking/                # 多阶段排序（Cross-Encoder 精排）
│   ├── generation/             # 答案生成（Prompt 工程、引用追踪）
│   └── gateway/                # HTTP API、SSE 流式、搜索页面
├── conf/                       # 配置文件
├── docs/design/                # 设计文档
└── Makefile
```

## 开发

```bash
make build      # 构建
make test       # 测试
make run        # 启动（读取 conf/suwen.toml）
make clean      # 清理
```

## 路线图

- [x] Phase 1 — 最小可用：检索 + LLM 生成 + 搜索页面
- [ ] Phase 2 — 搜索体验：查询改写、Cross-Encoder 精排、SSE 流式
- [ ] Phase 3 — 生产就绪：查询缓存、监控、评测基准

详见 [implementation-plan.md](docs/design/implementation-plan.md)。

## 相关项目

| 项目 | 说明 |
|------|------|
| [vortex](https://github.com/wzhongyou/vortex) | C++ 倒排索引引擎 |
| [proximia](https://github.com/wzhongyou/proximia) | Go 向量数据库 |
| [kuafu](https://github.com/wzhongyou/kuafu) | Python 网络爬虫 |
| [llmgate](https://github.com/wzhongyou/llmgate) | Go LLM 网关 |

## License

MIT
