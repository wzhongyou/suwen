import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: '素问 Suwen — AI 搜索',
  description: 'AI 驱动的开源搜索引擎 · 问必有据',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
