import { useEffect, useMemo, useState } from 'react';
import { GoogleAuthProvider, onAuthStateChanged, signInWithPopup, signOut, type Auth, type User } from 'firebase/auth';
import { doc, getDoc, type Firestore } from 'firebase/firestore';
import { auth, configured, db } from './firebase';
import { Dashboard } from './Dashboard';
import { networkSource } from './dashboardData';
import { Frame } from './Frame';
import { Login } from './Login';
import { fetchAppliedConfig, firestoreStore, type AppliedSource } from './data';

const snapshotUrl = import.meta.env.VITE_SNAPSHOT_URL || undefined;
const loadApplied: AppliedSource | undefined = snapshotUrl ? () => fetchAppliedConfig(snapshotUrl) : undefined;

const source = snapshotUrl ? networkSource(snapshotUrl) : undefined;

export function App() {
  if (!configured || !auth || !db) {
    return (
      <Frame>
        <section className="card empty">
          <h2>설정이 필요합니다</h2>
          <p className="muted">
            빌드할 때 Firebase 환경변수(VITE_FIREBASE_*)를 넣어야 합니다. 자세한 내용은 web/README.md를 보세요.
          </p>
        </section>
      </Frame>
    );
  }
  return <Signed auth={auth} db={db} />;
}

function Signed({ auth, db }: { auth: Auth; db: Firestore }) {
  const [user, setUser] = useState<User | null | undefined>(undefined);
  const store = useMemo(() => firestoreStore(db), [db]);

  useEffect(() => onAuthStateChanged(auth, setUser), [auth]);

  // 관리자 목록에 있는 계정만 화면을 봅니다(규칙상 본인 문서만 읽을 수 있음). 상태 파일 자체는 공개 URL이라
  // 이 확인은 화면 접근을 맞추는 용도이고 데이터 보호 수단이 아닙니다.
  const uid = user?.uid;
  const [admin, setAdmin] = useState<boolean | undefined>(undefined);
  useEffect(() => {
    setAdmin(undefined);
    if (!uid) return;
    let cancelled = false;
    getDoc(doc(db, 'admins', uid)).then((snap) => !cancelled && setAdmin(snap.exists()), () => !cancelled && setAdmin(false));
    return () => {
      cancelled = true;
    };
  }, [db, uid]);

  if (user === undefined) {
    return (
      <Frame>
        <p className="muted center">확인 중…</p>
      </Frame>
    );
  }
  if (user === null) {
    return (
      <Frame>
        <Login
          onSignIn={async () => {
            await signInWithPopup(auth, new GoogleAuthProvider());
          }}
        />
      </Frame>
    );
  }
  if (admin !== true) {
    return (
      <Frame email={user.email} onLogout={() => signOut(auth)}>
        {admin === undefined ? (
          <p className="muted center">확인 중…</p>
        ) : (
          <section className="card empty">
            <h2>접근 권한이 없습니다</h2>
            <p className="muted">관리자 목록(<code>admins</code> 컬렉션)에 아래 UID를 문서 ID로 추가해야 합니다.</p>
            <code className="uid">{user.uid}</code>
          </section>
        )}
      </Frame>
    );
  }
  return (
    <Frame email={user.email} onLogout={() => signOut(auth)}>
      <Dashboard store={store} user={user} loadApplied={loadApplied} source={source} />
    </Frame>
  );
}
