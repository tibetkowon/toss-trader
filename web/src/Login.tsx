import { useState } from 'react';

interface Props {
  onSignIn: () => Promise<void>;
}

// 로그인 전 화면입니다. 실패 사유는 사용자에게 보이는 말로만 알려 줍니다.
export function Login({ onSignIn }: Props) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const signIn = async () => {
    setBusy(true);
    setError(null);
    try {
      await onSignIn();
    } catch {
      setError('로그인이 완료되지 않았습니다. 팝업 차단을 해제하고 다시 시도하세요.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="card login">
      <img src={`${import.meta.env.BASE_URL}icon-192.png`} alt="" width={64} height={64} className="login-icon" />
      <h2>관리자 로그인</h2>
      <p className="muted">등록된 관리자 계정으로 로그인하면 자동매매 설정을 볼 수 있습니다.</p>
      <button type="button" className="btn primary wide" onClick={signIn} disabled={busy}>
        {busy ? '로그인 중…' : 'Google 계정으로 로그인'}
      </button>
      {error && (
        <p className="notice-inline error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
