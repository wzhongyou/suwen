'use client';

import { useState, useEffect, useRef, KeyboardEvent } from 'react';

interface SearchBoxProps {
  onSearch: (query: string) => void;
  loading: boolean;
  compact: boolean;
}

export default function SearchBox({ onSearch, loading, compact }: SearchBoxProps) {
  const [value, setValue] = useState('');
  const [focused, setFocused] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handleKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        inputRef.current?.focus();
      }
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, []);

  const submit = () => {
    const q = value.trim();
    if (!q || loading) return;
    onSearch(q);
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter') submit();
  };

  const isMac = typeof navigator !== 'undefined' && navigator.platform.toLowerCase().includes('mac');

  return (
    <div
      className="search-root"
      style={{
        width: '100%',
        maxWidth: compact ? 680 : 640,
        margin: '0 auto',
      }}
    >
      <div
        style={{
          position: 'relative',
          width: '100%',
        }}
      >
        {/* Focus glow ring */}
        <div
          style={{
            position: 'absolute',
            inset: -4,
            borderRadius: compact ? 20 : 28,
            background: focused ? 'var(--accent-glow)' : 'transparent',
            filter: 'blur(8px)',
            transition: 'all 0.4s ease',
            pointerEvents: 'none',
            opacity: focused ? 1 : 0,
          }}
        />

        <input
          ref={inputRef}
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          placeholder="输入你的问题..."
          autoFocus
          autoComplete="off"
          style={{
            width: '100%',
            height: compact ? 48 : 56,
            padding: `0 ${compact ? 48 : 56}px 0 ${compact ? 18 : 24}px`,
            fontSize: compact ? 15 : 17,
            fontFamily: 'inherit',
            border: `1.5px solid ${focused ? 'var(--accent)' : 'var(--border)'}`,
            borderRadius: compact ? 16 : 28,
            background: 'var(--surface)',
            color: 'var(--text)',
            outline: 'none',
            transition: 'all 0.3s cubic-bezier(0.16, 1, 0.3, 1)',
            boxShadow: focused
              ? 'var(--shadow-lg), 0 0 0 1px var(--accent-glow)'
              : 'var(--shadow-md)',
            position: 'relative',
            zIndex: 1,
          }}
          className="placeholder:text-[var(--text-tertiary)]"
        />

        {/* Cmd+K hint */}
        {!value && !focused && !loading && (
          <span
            style={{
              position: 'absolute',
              right: compact ? 48 : 56,
              top: '50%',
              transform: 'translateY(-50%)',
              fontSize: 11,
              color: 'var(--text-tertiary)',
              background: 'var(--border-light)',
              padding: '2px 7px',
              borderRadius: 5,
              border: '1px solid var(--border)',
              fontFamily: 'inherit',
              pointerEvents: 'none',
              zIndex: 2,
              transition: 'opacity 0.2s',
            }}
            className="max-sm:hidden"
          >
            {isMac ? '⌘K' : 'Ctrl+K'}
          </span>
        )}

        {/* Submit button */}
        <button
          onClick={submit}
          disabled={loading}
          aria-label="搜索"
          style={{
            position: 'absolute',
            right: compact ? 5 : 7,
            top: '50%',
            transform: 'translateY(-50%)',
            width: compact ? 36 : 42,
            height: compact ? 36 : 42,
            borderRadius: '50%',
            border: 'none',
            background: value ? 'var(--accent)' : 'var(--border)',
            color: value ? '#fff' : 'var(--text-tertiary)',
            cursor: value && !loading ? 'pointer' : 'default',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            transition: 'all 0.3s cubic-bezier(0.16, 1, 0.3, 1)',
            opacity: loading ? 0.6 : 1,
            zIndex: 2,
            transformOrigin: 'center',
          }}
          onMouseEnter={(e) => {
            if (value && !loading) {
              e.currentTarget.style.transform = 'translateY(-50%) scale(1.06)';
            }
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.transform = 'translateY(-50%) scale(1)';
          }}
        >
          {loading ? (
            <svg width="16" height="16" viewBox="0 0 24 24" style={{ animation: 'spin 0.8s linear infinite' }}>
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" fill="none" opacity="0.3" />
              <path d="M4 12a8 8 0 018-8" stroke="currentColor" strokeWidth="3" strokeLinecap="round" fill="none" />
              <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
            </svg>
          ) : (
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <line x1="5" y1="12" x2="19" y2="12" />
              <polyline points="12 5 19 12 12 19" />
            </svg>
          )}
        </button>
      </div>
    </div>
  );
}
