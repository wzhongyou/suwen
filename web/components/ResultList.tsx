'use client';

import type { SearchResult } from '@/lib/api';

interface ResultListProps {
  results: SearchResult[];
}

export default function ResultList({ results }: ResultListProps) {
  if (!results || results.length === 0) return null;

  return (
    <div className="flex flex-col gap-1.5 mt-1">
      <div className="text-xs font-semibold text-[var(--text-tertiary)] uppercase tracking-wide py-2">
        全部结果
      </div>
      {results.map((r, i) => (
        <a
          key={r.doc_id || i}
          href={r.url}
          target="_blank"
          rel="noopener noreferrer"
          className="block px-[18px] py-3.5 bg-transparent border border-transparent
            rounded-xl no-underline text-inherit transition-all duration-150
            hover:bg-[var(--surface)] hover:border-[var(--border)] hover:shadow-sm
            group animate-slide-up"
          style={{ animationDelay: `${i * 30}ms` }}
        >
          <div className="flex items-baseline gap-1.5 text-sm font-medium text-[var(--text)] mb-0.5">
            <span className="text-[10px] font-medium text-[var(--text-tertiary)] min-w-4 flex-shrink-0">
              {r.rank || i + 1}
            </span>
            {r.title || 'Untitled'}
          </div>
          <div className="text-xs text-[var(--text-tertiary)] mb-1 truncate flex items-center gap-1">
            {r.url}
          </div>
          <div className="text-[13px] text-[var(--text-secondary)] leading-relaxed line-clamp-3">
            {r.snippet || r.description || ''}
          </div>
        </a>
      ))}
    </div>
  );
}
