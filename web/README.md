# 트레이더 웹 (PWA)

홈·거래·종목·성과·시스템의 하단 탭에서 자동매매 상태를 확인하고 설정을 바꾸는 화면입니다(SPEC.md 9.1·9.2). Vite + React + Firebase(Auth, Firestore)로 만들고, 빌드 결과물은 Firebase Hosting에 올립니다.

- 로그인한 관리자만 설정을 보고 바꿀 수 있습니다.
- 손절·일일 한도는 SPEC 4.3 범위(1~2%, 1~5%)를 넘을 수 없습니다. 범위는 `src/limits.ts`, `internal/settings`, `firebase/firestore.rules`에 같은 값으로 있습니다.
- 저장한 설정은 **다음 세션부터** 적용됩니다.
  - 단, 트레이더의 Firestore 설정 로더(`internal/settings`)가 main에 들어오기 전까지는 저장해도 트레이더에 적용되지 않습니다(SPEC.md 10절 8항 진행 상태).

## 로컬 실행

```bash
cd web
npm install
cp .env.example .env.local   # 값을 채웁니다(저장소에 올리지 마세요)
npm run dev
npm test                     # 범위·변환·변경 내역 단위 테스트
npm run build
```

## 환경변수 (빌드 시점)

| 변수 | 설명 |
|---|---|
| `VITE_FIREBASE_API_KEY` | Firebase 웹 앱 설정 값 |
| `VITE_FIREBASE_AUTH_DOMAIN` | `<프로젝트>.firebaseapp.com` |
| `VITE_FIREBASE_PROJECT_ID` | Firebase 프로젝트 ID |
| `VITE_FIREBASE_APP_ID` | Firebase 웹 앱 ID |
| `VITE_SNAPSHOT_URL` | 상태 파일(`status.json`)의 전체 주소. 상태 조회와 적용 설정 비교에 사용합니다. 같은 디렉터리의 history/에서 성과를 읽습니다 |
| `VITE_BASE` | 배포 경로. Hosting은 기본값 `/` 그대로 둡니다 |

## 배포 전 준비 (한 번만)

1. **Firebase 프로젝트**: 기존 GCP 프로젝트에 Firebase를 연결합니다. Firestore 데이터베이스를 만들고, Authentication에서 Google 로그인을 켭니다.
2. **로그인 허용 도메인**: Authentication > 설정 > 승인된 도메인에 GCS 호스트(`storage.googleapis.com`)를 추가할 수 있는지 확인하세요. 추가가 안 되면 커스텀 도메인이 필요합니다.
3. **관리자 등록**: 로그인 후 화면에 표시되는 UID를 Firestore `admins` 컬렉션에 문서 ID로 추가합니다(필드는 비워도 됩니다).
4. **보안 규칙 배포**: `firebase/` 폴더에서 `firebase deploy --only firestore:rules --project <프로젝트 ID>`를 실행합니다.
5. **트레이더 설정**: VM 서비스 계정에 `roles/datastore.viewer`를 부여하고, 환경변수 `SETTINGS_PROJECT_ID`를 `/etc/toss-trader/env`에 넣습니다.

## 배포

```bash
cd web
npm run build
firebase deploy --only hosting --project <프로젝트 ID>   # 저장소 루트에서 실행. index.html·sw.js 무캐시 헤더는 firebase.json에 있음
```

`status.json`은 트레이더가 올리는 파일이며, 이 앱은 30초마다 네트워크 우선으로 읽습니다(`cache: 'no-store'`). 실패 시 주소별로 저장한 마지막 정상 스냅샷과 오프라인 배지를 표시합니다. 브라우저 저장소를 사용할 수 없으면 현재 페이지 메모리만 사용합니다.

## 알려진 점

- 아이콘(`public/icon-*.png`)은 임시 이미지입니다. `scripts/gen-icons.mjs`를 바꾸거나 PNG를 교체하세요.
- 번들이 약 784KB입니다(Firebase SDK 포함). 필요하면 화면을 나눠 로드합니다.
- 전략 핵심값(k, 이동평균 기간 등)도 웹에서 바꿀 수 있습니다. 저장 확인 창에서 경고합니다.

## 화면 미리보기 (Firebase 없이)

`npm run dev`를 실행한 뒤 아래 주소를 엽니다. 목업 데이터로 화면 상태를 바꿔 볼 수 있습니다.

- `/mock/index.html?view=settings&state=pending`: 저장 v5, 실행 중 v4 (다음 세션 적용 대기)
- `/mock/index.html`: 홈 화면
- `?view=trading`, `?view=symbols`, `?view=performance`, `?view=settings`: 각 탭
- `?state=halted`, `?state=stale`: 중단 및 오래된 스냅샷 (정규장에 경고)
- `/mock/index.html?state=empty`: 저장된 설정 없음
- `/mock/index.html?view=settings&state=denied`: 권한 없음 안내
- `/mock/index.html?state=offline`: 실행 상태를 가져오지 못함
- `/mock/index.html?view=login`: 로그인 화면

미리보기 코드(`mock/`)는 실제 빌드(`npm run build`)에 들어가지 않습니다.

## 상태와 성과 표시

- 모의 배지는 로그인 전후와 모든 탭에 표시됩니다. 수동 정지·재개 기능은 없습니다.
- 갱신 지연은 양 시장의 평일 정규장 중 `max(180초, 하트비트 × 3)` 초과 시 경고합니다. 미국 서머타임을 반영하며, 휴장일·임시 거래시간은 원본 데이터에 없어 반영하지 않습니다.
- 성과는 한국 날짜 기준 오늘을 포함한 30일의 KR/US 파일 60개를 최대 6개씩 조회합니다. 404는 무시하고 기타 실패는 부분 조회 안내를 표시합니다.
- `summary.account_equity_krw`는 이미 통합 계좌 자산입니다. 전체 필터는 날짜별 마지막 자산과 시장별 일 손익 합계를 사용하며 자산을 중복 합산하지 않습니다. 시장 필터는 해당 세션의 계좌 마감 자산을 표시합니다.
- 일 손익 요약이 없으면 이전 조회일 대비 자산 차이를 사용합니다. 첫 기록의 손익은 추정하지 않습니다. 최대 낙폭 차트는 조회 구간의 고점 기준입니다.
- 현재가가 없어 종목의 목표가 괴리율은 표시하지 않습니다. 실제 적용 설정은 스냅샷 값, 설정 편집은 기존 Firestore 저장 흐름을 사용합니다.
