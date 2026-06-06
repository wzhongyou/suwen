'use client';

import { useState, useCallback, useRef } from 'react';
import SearchBox from '@/components/SearchBox';
import StatusBar from '@/components/StatusBar';
import AnswerCard from '@/components/AnswerCard';
import SourceList from '@/components/SourceList';
import ResultList from '@/components/ResultList';
import type { Citation, SearchResult } from '@/lib/api';
import { search, searchStream } from '@/lib/api';

type Stage = 'idle' | 'retrieving' | 'generating' | 'done';

export default function HomePage() {
  const [compact, setCompact] = useState(false);
  const [stage, setStage] = useState<Stage>('idle');
  const [statusMsg, setStatusMsg] = useState('');
  const [answer, setAnswer] = useState('');
  const [sources, setSources] = useState<Citation[]>([]);
  const [results, setResults] = useState<SearchResult[]>([]);
  const [timeMs, setTimeMs] = useState(0);
  const [loading, setLoading] = useState(false);
  const [useStream, setUseStream] = useState(true);
  const abortRef = useRef<AbortController | null>(null);

  const handleSearch = useCallback(async (query: string) => {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;

    setCompact(true);
    setLoading(true);
    setAnswer('');
    setSources([]);
    setResults([]);
    setTimeMs(0);

    if (useStream) {
      setStage('retrieving');
      setStatusMsg('正在检索...');

      const start = Date.now();
      let streamedAnswer = '';

      try {
        const gen = searchStream(query);
        for await (const ev of gen) {
          if (controller.signal.aborted) break;
          switch (ev.event) {
            case 'status':
              setStatusMsg(ev.data.message || '');
              break;
            case 'citations':
              setSources(ev.data || []);
              break;
            case 'token':
              streamedAnswer += ev.data.text || '';
              setAnswer(streamedAnswer);
              setStage('generating');
              setStatusMsg('正在生成答案...');
              break;
            case 'done':
              setStage('done');
              setTimeMs(Date.now() - start);
              setStatusMsg(`完成 · ${Date.now() - start}ms`);
              setLoading(false);
              return;
            case 'error':
              setStage('done');
              setStatusMsg(ev.data.message || '流式错误');
              setLoading(false);
              return;
          }
        }
      } catch (err: any) {
        if (err.name === 'AbortError') return;
        console.error('Stream error:', err);
        setStatusMsg('流式失败，尝试普通搜索...');
        setUseStream(false);
      }
    }

    setStage('retrieving');
    setStatusMsg('正在搜索...');
    const start = Date.now();

    try {
      const resp = await search({ query });
      if (controller.signal.aborted) return;
      setAnswer(resp.answer || '未找到相关信息。');
      setSources(resp.sources || []);
      setResults(resp.results || []);
      setTimeMs(resp.time_ms || Date.now() - start);
      setStage('done');
      setStatusMsg(`找到 ${resp.results?.length || 0} 条结果 · ${resp.time_ms || Date.now() - start}ms`);
    } catch (err: any) {
      if (err.name === 'AbortError') return;
      setAnswer(`搜索失败: ${err.message}`);
      setStage('done');
      setStatusMsg('搜索失败');
    } finally {
      setLoading(false);
    }
  }, [useStream]);

  return (
    <div
      style={{
        width: '100%',
        maxWidth: 740,
        margin: '0 auto',
        padding: '0 24px',
        display: 'flex',
        flexDirection: 'column',
        minHeight: '100vh',
        position: 'relative',
        zIndex: 1,
      }}
      className="max-sm:px-3"
    >
      {/* ====== Hero / Header ====== */}
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          width: '100%',
          paddingTop: compact ? 28 : '30vh',
          paddingBottom: compact ? 20 : 0,
          transition: 'padding-top 0.6s cubic-bezier(0.16, 1, 0.3, 1)',
        }}
      >
        {/* Logo */}
        <div
          style={{
            textAlign: 'center',
            marginBottom: compact ? 16 : 48,
            transition: 'all 0.6s cubic-bezier(0.16, 1, 0.3, 1)',
            opacity: 1,
          }}
        >
          <h1
            style={{
              fontWeight: 700,
              color: 'var(--text)',
              fontSize: compact ? 24 : 56,
              letterSpacing: compact ? '-0.5px' : '-2.5px',
              lineHeight: 1.1,
              margin: 0,
              transition: 'all 0.6s cubic-bezier(0.16, 1, 0.3, 1)',
            }}
          >
            素问
          </h1>
          <p
            style={{
              fontSize: compact ? 0 : 15,
              color: 'var(--text-tertiary)',
              fontWeight: 400,
              margin: 0,
              marginTop: compact ? 0 : 10,
              transition: 'all 0.5s ease',
              opacity: compact ? 0 : 1,
              height: compact ? 0 : 'auto',
              overflow: 'hidden',
            }}
          >
            AI 驱动的开源搜索 · 问必有据
          </p>
        </div>

        {/* Search */}
        <SearchBox onSearch={handleSearch} loading={loading} compact={compact} />
      </div>

      {/* ====== Results ====== */}
      {compact && (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 16,
            paddingBottom: 64,
            width: '100%',
          }}
          className="animate-fade-in"
        >
          <StatusBar stage={stage} message={statusMsg} />
          <AnswerCard content={answer} loading={loading && stage === 'generating'} />
          <SourceList sources={sources} />
          <ResultList results={results} />
        </div>
      )}
    </div>
  );
}
