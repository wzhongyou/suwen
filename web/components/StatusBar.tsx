'use client';

interface StatusBarProps {
  stage: 'idle' | 'retrieving' | 'generating' | 'done';
  message: string;
}

export default function StatusBar({ stage, message }: StatusBarProps) {
  if (stage === 'idle') return null;

  return (
    <div className="flex items-center gap-2.5 text-[13px] text-[var(--text-secondary)] py-1 min-h-6">
      <span
        className={`w-2 h-2 rounded-full flex-shrink-0 transition-all duration-300 ${
          stage === 'done'
            ? 'bg-green-500'
            : 'bg-[var(--accent)] animate-pulse-dot'
        }`}
      />
      <span>{message}</span>
    </div>
  );
}
