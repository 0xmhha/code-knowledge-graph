import './globals.css';
import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: 'CKG Viewer',
  description: 'Code knowledge graph viewer',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
