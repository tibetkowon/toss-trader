import type { ReactNode } from 'react';

interface Props {
  email?: string | null;
  onLogout?: () => void;
  children: ReactNode;
}

// 모든 화면이 공유하는 바깥 틀입니다. 상단 바(브랜드, 계정, 로그아웃)와 본문 영역을 가집니다.
export function Frame({ email, onLogout, children }: Props) {
  return (
    <div className="frame">
      <header className="appbar">
        <div className="brand">
          <img src={`${import.meta.env.BASE_URL}icon-192.png`} alt="" width={32} height={32} />
          <div className="brand-text">
            <h1>트레이더 설정</h1>
            {email && <p className="sub">{email}</p>}
          </div>
        </div>
        {onLogout && (
          <button type="button" className="btn ghost small" onClick={onLogout}>
            로그아웃
          </button>
        )}
      </header>
      <main className="page">{children}</main>
    </div>
  );
}
