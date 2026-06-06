// Suwen API client — search, stream, debug, health.
// Uses relative paths; rewrites are configured in next.config.js.

const BASE = '';

export interface SearchRequest {
  query: string;
  max_results?: number;
  stream?: boolean;
}

export interface Citation {
  index: number;
  url: string;
  title: string;
}

export interface SearchResult {
  doc_id: string;
  title: string;
  snippet: string;
  url: string;
  description?: string;
  bm25_score: number;
  vector_score?: number;
  final_score: number;
  rank: number;
  rerank_score?: number;
}

export interface SearchResponse {
  answer: string;
  sources: Citation[];
  results: SearchResult[];
  time_ms: number;
  cached?: boolean;
  error?: string;
}

export interface MetricsSnapshot {
  total_requests: number;
  total_errors: number;
  avg_latency_ms: number;
  llm_calls: number;
  llm_cost_usd: number;
  path_counts: Record<string, number>;
}

export interface HealthResponse {
  status: string;
  metrics: MetricsSnapshot;
}

export async function search(req: SearchRequest): Promise<SearchResponse> {
  const resp = await fetch(`${BASE}/api/v1/search`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(err.error || `HTTP ${resp.status}`);
  }
  return resp.json();
}

export async function searchDebug(query: string): Promise<{ results: SearchResult[]; total: number; time_ms: number }> {
  const resp = await fetch(`${BASE}/api/v1/search/debug`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query }),
  });
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(err.error || `HTTP ${resp.status}`);
  }
  return resp.json();
}

// SSE stream reader — yields events as they arrive.
export async function* searchStream(query: string): AsyncGenerator<SSEEvent> {
  const url = `${BASE}/api/v1/search/stream?q=${encodeURIComponent(query)}`;
  const resp = await fetch(url, {
    headers: { Accept: 'text/event-stream' },
  });

  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}`);
  }

  const reader = resp.body!.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() || '';

    let eventType = '';
    for (const line of lines) {
      if (line.startsWith('event: ')) {
        eventType = line.slice(7).trim();
      } else if (line.startsWith('data: ')) {
        const data = JSON.parse(line.slice(6));
        yield { event: eventType, data };
      }
    }
  }
}

export interface SSEEvent {
  event: string;
  data: any;
}

export async function health(): Promise<HealthResponse> {
  const resp = await fetch(`${BASE}/health`);
  return resp.json();
}
