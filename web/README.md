# 트레이더 설정 웹 (PWA)

자동매매 설정을 휴대폰에서 바꾸는 화면입니다(SPEC.md 9.1·9.2). Vite + React + Firebase(Auth, Firestore)로 만들고, 빌드 결과물은 기존 GCS 버킷에 올립니다.

- 로그인한 관리자만 설정을 보고 바꿀 수 있습니다.
- 손절·일일 한도는 SPEC 4.3 범위(1~2%, 1~5%)를 넘을 수 없습니다. 범위는 `src/limits.ts`, `internal/settings`, `firebase/firestore.rules`에 같은 값으로 있습니다.
- 저장한 설정은 **다음 세션부터** 적용됩니다.

## 로컬 실행

```bash
cd web
npm install
cp .env.example .env.local   # 값을 채웁니다(저장소에 올리지 마세요)
npm run dev
npm test                     # 범위·변환·변경 내역 단위 테스트
npm run build                # VITE_BASE=/<bucket>/ 로 GCS 경로에 맞춥니다
```

## 환경변수 (빌드 시점)

| 변수 | 설명 |
|---|---|
| `VITE_FIREBASE_API_KEY` | Firebase 웹 앱 설정 값 |
| `VITE_FIREBASE_AUTH_DOMAIN` | `<프로젝트>.firebaseapp.com` |
| `VITE_FIREBASE_PROJECT_ID` | Firebase 프로젝트 ID |
| `VITE_FIREBASE_APP_ID` | Firebase 웹 앱 ID |
| `VITE_SNAPSHOT_URL` | 상태 파일(`status.json`)의 전체 주소. 실행 중인 설정 버전을 비교합니다 |
| `VITE_BASE` | 배포 경로. GCS 슬러그 경로면 `/<버킷>/`, 루트면 `/` |

## 배포 전 준비 (한 번만)

1. **Firebase 프로젝트**: 기존 GCP 프로젝트에 Firebase를 연결합니다. Firestore 데이터베이스를 만들고, Authentication에서 Google 로그인을 켭니다.
2. **로그인 허용 도메인**: Authentication > 설정 > 승인된 도메인에 GCS 호스트(`storage.googleapis.com`)를 추가할 수 있는지 확인하세요. 추가가 안 되면 커스텀 도메인이 필요합니다.
3. **관리자 등록**: 로그인 후 화면에 표시되는 UID를 Firestore `admins` 컬렉션에 문서 ID로 추가합니다(필드는 비워도 됩니다).
4. **보안 규칙 배포**: `firebase/` 폴더에서 `firebase deploy --only firestore:rules --project <프로젝트 ID>`를 실행합니다.
5. **트레이더 설정**: VM 서비스 계정에 `roles/datastore.viewer`를 부여하고, 환경변수 `SETTINGS_PROJECT_ID`를 `/etc/toss-trader/env`에 넣습니다.

## 배포

```bash
cd web
VITE_BASE=/<버킷>/ npm run build
# dist/ 내용을 버킷에 올립니다. index.html과 sw.js는 캐시되지 않게 올리는 것을 권장합니다.
```

`status.json`은 트레이더가 올리는 파일이며, 이 앱은 항상 네트워크에서 직접 읽습니다(`cache: 'no-store'`).

## 알려진 점

- 아이콘(`public/icon-*.png`)은 임시 이미지입니다. `scripts/gen-icons.mjs`를 바꾸거나 PNG를 교체하세요.
- 번들이 약 784KB입니다(Firebase SDK 포함). 필요하면 화면을 나눠 로드합니다.
- 전략 핵심값(k, 이동평균 기간 등)도 웹에서 바꿀 수 있습니다. 저장 확인 창에서 경고합니다.

## 화면 미리보기 (Firebase 없이)

`npm run dev`를 실행한 뒤 아래 주소를 엽니다. 목업 데이터로 화면 상태를 바꿔 볼 수 있습니다.

- `/mock/index.html?state=pending`: 저장 v5, 실행 중 v4 (다음 세션 적용 대기)
- `/mock/index.html`: 저장과 실행 버전이 같음
- `/mock/index.html?state=empty`: 저장된 설정 없음
- `/mock/index.html?state=denied`: 권한 없음 안내
- `/mock/index.html?state=offline`: 실행 상태를 가져오지 못함
- `/mock/index.html?view=login`: 로그인 화면

미리보기 코드(`mock/`)는 실제 빌드(`npm run build`)에 들어가지 않습니다.
