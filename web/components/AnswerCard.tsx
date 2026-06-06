'use client';

import { useEffect, useRef } from 'react';

interface AnswerCardProps {
  content: string;
  loading: boolean;
}

export default function AnswerCard({ content, loading }: AnswerCardProps) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (ref.current) {
      ref.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
  }, [content]);

  if (!content && !loading) return null;

  const html = renderAnswer(content);

  return (
    <div
      ref={ref}
      className="answer-content bg-[var(--surface)] border border-[var(--border)]
        rounded-2xl px-7 py-6 shadow-sm text-[15px] leading-relaxed
        text-[var(--text)] break-words animate-fade-in max-sm:px-4 max-sm:py-4 max-sm:text-sm"
    >
      {loading && !content ? (
        <div className="flex items-center gap-3 text-[var(--text-tertiary)] italic">
          <span className="w-2 h-2 rounded-full bg-[var(--accent)] animate-pulse-dot" />
          正在生成答案...
        </div>
      ) : (
        <div dangerouslySetInnerHTML={{ __html: html }} />
      )}
    </div>
  );
}

function renderAnswer(text: string): string {
  let html = escapeHtml(text);

  // Convert [N] references to clickable citation badges
  html = html.replace(/\[(\d+)\]/g, (_, num) =>
    `<span class="cite-badge" data-cite="${num}">${num}</span>`
  );

  // Convert **bold**
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');

  // Paragraphs
  html = html.replace(/\n\n/g, '</p><p>');
  html = '<p>' + html + '</p>';
  html = html.replace(/<p><\/p>/g, '');

  return html;
}

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}
