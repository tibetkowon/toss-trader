import { useEffect, useState } from 'react';
import { GoogleAuthProvider, onAuthStateChanged, signInWithPopup, signOut, type Auth, type User } from 'firebase/auth';
import type { Firestore } from 'firebase/firestore';
import { auth, configured, db } from './firebase';
import { SettingsPage } from './SettingsPage';

const snapshotUrl = import.meta.env.VITE_SNAPSHOT_URL || undefined;

export function App() {
  if (!configured || !auth || !db) {
    return (
      <main className="shell">
        <section className="card">
          <h2>설정이 필요합니다</h2>
          <p>빌드할 때 Firebase 환경변수(VITE_FIREBASE_*)를 넣어야 합니다. 자세한 내용은 web/README.md를 보세요.</p>
        </section>
      </main>
    );
  }
  return <Signed auth={auth} db={db} />;
}

function Signed({ auth, db }: { auth: Auth; db: Firestore }) {
  const [user, setUser] = useState<User | null | undefined>(undefined);

  useEffect(() => onAuthStateChanged(auth, setUser), [auth]);

  if (user === undefined) {
    return <p className="muted center">확인 중…</p>;
  }
  if (user === null) {
    return <Login auth={auth} />;
  }
  return (
    <main className="shell">
      <header className="top">
        <h1>트레이더 설정</h1>
        <button type="button" className="ghost" onClick={() => signOut(auth)}>로그아웃</button>
      </header>
      <SettingsPage db={db} user={user} snapshotUrl={snapshotUrl} />
    </main>
  );
}

function Login({ auth }: { auth: Auth }) {
  const [error, setError] = useState<string | null>(null);
  const signIn = async () => {
    setError(null);
    try {
      await signInWithPopup(auth, new GoogleAuthProvider());
    } catch {
      setError('로그인에 실패했습니다. 팝업 차단을 해제하고 다시 시도하세요.');
    }
  };
  return (
    <main className="shell">
      <section className="card center">
        <h1>트레이더 설정</h1>
        <p className="muted">관리자 계정으로 로그인하세요.</p>
        <button type="button" onClick={signIn}>Google로 로그인</button>
        {error && <p className="error">{error}</p>}
      </section>
    </main>
  );
}
