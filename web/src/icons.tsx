// 화면에서 쓰는 작은 아이콘입니다. 선 두께와 색은 currentColor를 따릅니다.
export function LockIcon({ size = 20 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="5" y="11" width="14" height="9" rx="2" />
      <path d="M8 11V8a4 4 0 0 1 8 0v3" />
    </svg>
  );
}

export function TabIcon({ index }: { index: number }) {
  const paths = [
    'M3 10 12 3l9 7v11h-6v-7H9v7H3Z',
    'M3 7h17m-4-4 4 4-4 4M21 17H4m4-4-4 4 4 4',
    'M4 4h6v6H4ZM14 4h6v6h-6ZM4 14h6v6H4ZM14 14h6v6h-6Z',
    'M3 3v18h18M6 16l5-6 4 3 6-8',
    'M4 7h16M4 17h16M8 3v8M16 13v8',
  ];
  return <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={paths[index]} /></svg>;
}
