import { useEffect, useMemo, useState } from 'react';
import { GoogleAuthProvider, onAuthStateChanged, signInWithPopup, signOut, type Auth, type User } from 'firebase/auth';
import type { Firestore } from 'firebase/firestore';
import { auth, configured, db } from './firebase';
import { SettingsPage } from './SettingsPage';
import { Frame } from './Frame';
import { Login } from './Login';
import { fetchAppliedConfig, firestoreStore, type AppliedSource } from './data';

const snapshotUrl = import.meta.env.VITE_SNAPSHOT_URL || undefined;
const loadApplied: AppliedSource | undefined = snapshotUrl ? () => fetchAppliedConfig(snapshotUrl) : undefined;

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
  return (
    <Frame email={user.email} onLogout={() => signOut(auth)}>
      <SettingsPage store={store} user={user} loadApplied={loadApplied} />
    </Frame>
  );
}
