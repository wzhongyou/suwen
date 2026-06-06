'use client';

import type { Citation } from '@/lib/api';

interface SourceListProps {
  sources: Citation[];
}

export default function SourceList({ sources }: SourceListProps) {
  if (!sources || sources.length === 0) return null;

  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wide mr-1">
        参考来源
      </span>
      {sources.map((s) => (
        <a
          key={s.index}
          href={s.url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1.5 px-3.5 py-1.5
            bg-[var(--surface)] border border-[var(--border)]
            text-[var(--text-secondary)] rounded-full text-[13px]
            no-underline transition-all duration-200
            hover:border-[var(--accent)] hover:text-[var(--text)] hover:shadow-sm
            max-w-[220px] whitespace-nowrap overflow-hidden text-ellipsis"
          title={s.title}
        >
          <span className="font-bold text-[var(--accent)] text-[11px]">{s.index}</span>
          <span className="truncate">{s.title}</span>
        </a>
      ))}
    </div>
  );
}
