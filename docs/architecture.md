# Organon 아키텍처

- **상태**: MVP-A(task)와 task 편집·목록·프로젝트·CLI까지 구현, MVP-B(org-roam)는 설계 단계 (2026-10-03)
- **한 줄 요약**: Emacs + Org-mode + org-roam을 headless 엔진으로 쓰고, 그 위에 내가 통제하는
  Personal API를 계약으로 둔다.

> 실측값은 2026-10-02 스파이크(Raspberry Pi 4, Debian 13, Emacs 30.1, Org 9.7.11,
> org-roam 2.3.1, podman 5.4.2)에서 얻었다. 근거는 [부록 A](#부록-a-스파이크-실측-결과)에 있다.

### 문서 지도

| 문서 | 다루는 것 | 언어 |
|---|---|---|
| `docs/architecture.md` (이 문서) | **왜, 어떻게**: 경계, 보안, 시간 모델, 실측 근거, 전체에 걸친 결정 | 한국어 |
| `docs/usage.md`, `docs/deployment.md` | 사용자 문서: CLI·API 사용법, 설치·설정·노출·백업 | 영어 |
| `openspec/specs/<capability>/spec.md` | **무엇을**: 관찰 가능한 동작 계약 (requirement + WHEN/THEN scenario) | 영어 |
| `openspec/changes/<change>/` | 진행 중인 변경 (proposal, design, spec delta, tasks) | 영어 |
| `api/openapi.yaml` | HTTP wire 형식 계약 | 영어 |

동작 계약이 이 문서와 스펙에서 다르면 **스펙이 우선**한다. 이 문서는 스펙을 바꾸는 결정이 있을 때 함께 개정한다.

## 1. 목표와 비목표

### 목표

- 개인 Task Manager, Knowledge Manager, Daily Journal, AI agent backend를 하나의 plain-text 데이터로 대체한다.
- **바퀴를 재발명하지 않는다.** parser, TODO state machine, repeater, agenda, capture, ID, backlink
  index는 모두 Org/org-roam의 것을 쓴다. 새 코드는 이 기능들을 안전하게 외부로 노출하는 **adapter**다.
- 데이터와 UX의 소유권은 사용자에게 있다.

### 비목표

자체 Org parser, 자체 recurrence engine, editor, Android 앱, graph UI, multi-user/ACL, 실시간 공동 편집,
임의 Elisp 원격 실행, Git 기반 노트 동기화는 하지 않는다.

### "교체 가능한 엔진"에 대한 정직한 정의

데이터 포맷(Org)과 의미론(repeater, agenda)은 Org에 묶여 있다. Emacs를 다른 엔진으로 바꾸려면
그 의미론을 다시 구현해야 하므로 비목표와 충돌한다. 따라서 이 프로젝트가 보장하는 것은 다음 두 가지다.

1. **데이터 생존**: 언제든 plain-text `.org` 파일과 백업만으로 전체를 복원할 수 있다.
2. **안정적인 API 계약**: API는 Org 내부 표현(타임스탬프 문자열, buffer 위치 등)이 아니라
   정규화된 JSON을 노출한다. 그래서 클라이언트는 Emacs의 존재를 몰라도 된다.

## 2. 컴포넌트

```
   AI 에이전트 / Web / Android / CLI
              │  HTTPS (앞단 reverse proxy나 터널에서 TLS 종료)
              ▼
 ┌──────────────────────────┐   container: organon-api
 │ Personal API (Go)        │   - 인증·scope·입력 검증·멱등성
 │                          │   - JSON 정규화, UTC 변환
 └────────────┬─────────────┘   - 데이터 디렉터리 mount 없음
              │ JSON-RPC over Unix socket  (/run/organon/rpc.sock)
              │ 화이트리스트 method만 존재. eval 경로 없음.
 ┌────────────▼─────────────┐   container: organon-engine (Network=none)
 │ Emacs daemon             │
 │  organon.el (adapter)    │   - 모든 읽기/쓰기의 유일한 경로
 │  Org / Agenda / org-id   │   - 단일 스레드라 요청이 자연스럽게 직렬화됨
 │  org-roam (+ sqlite)     │
 └────────────┬─────────────┘
              │ find-file / save-buffer (atomic rename)
              ▼
     데이터 디렉터리  (host bind mount, canonical)
```

| 컴포넌트 | 책임 | 하지 않는 것 |
|---|---|---|
| `organon-api` (Go) | 인증, scope 검사, 입력 형식 검증, Idempotency-Key, 에러를 HTTP로 매핑, 시각을 UTC로 변환 | Org 파싱, 날짜 계산, 파일 접근 |
| `organon.el` (Elisp) | RPC 서버, method dispatch, 변경 공통 래퍼(§6), Org/org-roam API 호출, 결과 직렬화 | 네트워크 노출, 임의 eval |
| Org-mode | task 상태, repeater, agenda, ID | — |
| org-roam | node, backlink, link index (SQLite cache) | canonical 저장 |
| 클라이언트 | UX, 현지 시각 표시, 자연어 해석(AI 에이전트) | 파일 직접 접근, 날짜 의미 계산 |

## 3. 데이터 소유권과 Source of Truth

이 문서에서 `<data>`는 호스트의 데이터 디렉터리를 뜻한다(Compose의 `ORGANON_DATA`, 기본값 `./data`).

| 데이터 | 위치 | 성격 | 삭제하면 |
|---|---|---|---|
| `.org` 파일 | `<data>/org` | **canonical** | 백업에서 복원 |
| 첨부 파일 | `<data>/attachments` | **canonical** | 백업에서 복원 |
| 인스턴스 선언 | `<data>/organon.json` | **canonical** (데이터의 일부) | 백업에서 복원 |
| org-roam DB | `organon-cache` volume | cache | 기동할 때 재구축 |
| `org-id-locations` | `organon-cache` volume | cache | 기동할 때 재구축 |
| native-comp `.eln` | 이미지 안 (빌드 시 AOT) | build artifact | 이미지 재빌드 |
| RPC socket | `organon-run` volume | runtime | 기동할 때 재생성 |
| API 토큰 해시 | 토큰 파일 (Compose secret / podman secret) | 설정 | 재발급 |

**불변식**: cache, 컨테이너, 이미지, volume을 모두 지워도 `<data>`만 있으면 같은 상태로 복원된다.

**쓰기 규칙**: `<data>`에 쓰는 프로세스는 `organon-engine` 하나뿐이다. 사람이 직접 편집하는 것은
정상 경로가 아니다. 비상시 절차는 §10.5에 있다.

## 4. 시간 모델

Org 타임스탬프(`<2026-10-25 Sun>`)에는 타임존이 없다. 이 값은 특정 순간이 아니라 **달력 날짜/벽시계
시각**이다. "오늘이 며칠인가"는 Emacs 프로세스의 로컬 타임존이 결정하고, 그 결과(완료 시각, `.+`
repeater 기준일)는 파일에 기록된다. 그래서 클라이언트가 나중에 보정할 수 없다. 실측 근거는 부록 A의
"TZ 비교"다.

| 계층 | 타임존 |
|---|---|
| 호스트 OS, 컨테이너, cron/timer, 로그 | UTC |
| Emacs 프로세스 | `organon.json`의 **`calendar_tz`** (IANA 이름, 필수, 기본값 없음) |
| API 시각(instant) 필드 | RFC 3339 UTC (`2026-10-02T05:29:00Z`) |
| API 날짜 필드 | 타임존 없는 `YYYY-MM-DD` (+ 시각이 있으면 `HH:MM` 벽시계) |
| 클라이언트 | instant를 현지화한다. 필요하면 `?date=`로 자기 기준의 "오늘"을 조회한다 |

```json
// <data>/organon.json
{ "calendar_tz": "Asia/Seoul", "doing_limit": 3 }
```

- 인스턴스 하나 = 달력 하나 = 타임존 하나다. 가족이 공유하는 달력도 집 기준 타임존 하나를 쓴다.
- 엔진은 기동할 때 `calendar_tz`가 zoneinfo의 실제 지역 시간대(TZif 데이터)인지 확인하고 `TZ`로 적용한다.
  `localtime`, `posixrules`, `Factory`처럼 지역이 아닌 이름도 거부한다. 선언이 없거나 검증에 실패하면
  **요청을 받지 않는다**(`unavailable`). 조용히 UTC나 호스트 타임존으로 떨어지지 않게 하기 위해서다.
- `organon init`은 `--calendar-tz`를 필수로 받는다. 빠뜨리면 호스트 타임존을 감지해 실행할 명령을 제안할
  뿐 파일을 쓰지 않는다.
- **알려진 한계**: `calendar_tz`를 바꾸면 과거 LOGBOOK 벽시계 시각이 새 타임존으로 해석된다. Org
  타임스탬프가 offset을 저장하지 못하기 때문이다. 필요해지면 `tz_history`(적용 시작일별 이력)를 추가한다.
- DST 지역: repeater는 벽시계를 유지한다. 존재하지 않는 벽시계 시각이나 두 번 나오는 시각을 UTC로
  변환하는 규칙(앞쪽 offset 우선)은 테스트 벡터로 고정한다.

## 5. Org 파일 레이아웃

```
<data>/
├── organon.json                 인스턴스 선언 (calendar_tz 등)
├── org/                         org-roam-directory
│   ├── tasks/
│   │   ├── inbox.org            빠른 입력의 기본 대상
│   │   ├── personal.org
│   │   └── recurring.org
│   ├── projects/                파일 하나 = 프로젝트 하나 이상 (하위 디렉터리 허용)
│   ├── knowledge/               지식 node
│   ├── journal/YYYY/YYYY-MM-DD.org
│   └── archive/
└── attachments/
```

| 디렉터리 | agenda 대상 | org-roam 인덱싱 | 기본 검색 노출 |
|---|---|---|---|
| `tasks/` | ✅ | ✅ (task heading도 인덱스에 들어감) | ✗ (task는 node가 아님) |
| `projects/` | ✅ (재귀) | ✅ | task가 아닌 heading(프로젝트 heading 등) |
| `knowledge/` | ✗ | ✅ | ✅ |
| `journal/` | ✗ | ✅ (file node) | ✅ |
| `archive/` | ✗ | ✅ (ID 해석과 backlink 보존) | ✗ |

- `org-agenda-files`는 **매 호출마다 재귀로 계산**한다. Org는 디렉터리 항목을 재귀 탐색하지 않는다(실측).
- task heading을 org-roam에서 제외하지 않는다. 제외 함수를 쓰면 task에서 노트로 거는 backlink가
  사라지기 때문이다(실측). 대신 node를 조회할 때 task를 거른다. task는 6개 workflow 상태 중 하나와 ID가
  있는 heading이다(`todo IS NULL OR todo NOT IN (6개 상태)`). 파일의 `#+TODO:`로 선언한 다른 키워드
  (예: `IDEA`)의 heading은 task가 아니므로 node다.

### 객체 모델

| 객체 | Org 표현 | ID |
|---|---|---|
| Task | TODO keyword가 있는 heading | heading의 `:ID:` (UUID) |
| Project | `projects/` 아래 level-1 heading (ID 보유) | heading의 `:ID:` |
| Node | ID가 있는 file 또는 heading 중 task가 아닌 것. API로 만들면 `knowledge/<YYYYMMDDHHMMSS>-<slug>.org` 파일 하나 | `:ID:` |
| Journal | `journal/YYYY/YYYY-MM-DD.org` (file-level ID) | 날짜가 API 키이고, 내부적으로 file ID |

## 6. Emacs ↔ API IPC

### 6.1 방식

- Emacs 안에서 `make-network-process :family 'local :server t`로 **전용 Unix socket**을 연다.
- 프로토콜은 줄 단위 JSON(UTF-8)이고 연결 하나에 요청 하나다.
  ```json
  → {"id":"r-1","method":"task.complete","params":{"id":"…","expected_state":"NEXT"}}
  ← {"id":"r-1","ok":true,"result":{…}}
  ← {"id":"r-1","ok":false,"error":{"code":"conflict","message":"…"}}
  ```
- method는 `organon.el` 안의 **고정 dispatch 테이블**에만 있다. 문자열을 Lisp로 읽거나 eval하는
  경로가 없다.
- 요청은 process filter 밖(`run-at-time 0`)에서 처리한다.
- Emacs 기본 `server-start` socket은 컨테이너 내부 `/tmp`에만 존재한다. 이 socket은 eval이 가능하므로
  공유 volume에 절대 두지 않는다. 디버깅은 `podman exec -it organon-engine emacsclient -t`로 한다.
- 실측: ping 0.6ms, warm agenda 17ms. 한글, 따옴표, `(insert …)` 같은 문자열도 그대로 왕복된다.

### 6.2 변경 공통 래퍼 (`organon-with-entry`)

모든 쓰기 method는 이 래퍼를 통과한다. 각 항목은 스파이크에서 발견한 실제 실패 모드에 대응한다.

1. **프롬프트 차단**: `yes-or-no-p`, `y-or-n-p`, `read-*`, `completing-read`를 즉시 에러로
   바꾼다(`code: "prompt_blocked"`). headless 데몬이 프롬프트에서 멈추면 API 전체가 멈추기 때문이다.
   네이티브 컴파일된 호출자(Debian의 `files.el` 등)에도 적용되도록 C primitive의 trampoline을 이미지
   빌드 때 만든다.
2. **외부 변경 감지**: `revert-without-query`를 쓴다. buffer가 깨끗하면 조용히 revert하고, 저장 안 된
   변경이 있으면 `conflict`를 반환한다. `find-file-noselect`가 스스로 프롬프트를 띄우기 때문이다(실측).
3. **대상 찾기**: ID로 위치를 찾는다(org-id 인덱스 → 파일 안에서 `:ID:` 재확인 → 실패하면, 마지막 스캔 이후
   파일이 바뀐 경우에만 rescan → `not_found`).
4. **Org API로 변경**: `org-todo`, `org-deadline`, `org-schedule`, `org-set-tags`, `org-id-get-create`
   등을 쓴다. regex로 파일을 고치지 않는다.
5. **LOGBOOK flush**: `post-command-hook`에 걸린 `org-add-log-note`를 직접 실행한다. command loop가
   없으면 상태 변경 로그가 **조용히 누락**되기 때문이다(실측).
6. **저장**: `save-buffer` + `file-precious-flag`(temp 파일에 쓰고 fsync한 뒤 rename)로 저장한다. 그러면
   after-save-hook에서 그 파일을 org-roam 인덱스에 반영한다(`org-roam-db-update-file`, 실측 0.03s).
   `org-roam-db-autosync-mode`는 켜지 않는다. 이 모드는 primitive(`rename-file`, `delete-file`)에
   advice를 걸고, 훅 안의 에러가 `save-buffer` 밖으로 나와 성공한 저장을 실패로 보이게 한다.
7. **저장 실패 처리**(디스크 가득 참 등): buffer를 디스크 상태로 되돌리고 `internal` 에러를 반환한다.
   메모리에만 있는 변경을 남기지 않는다.
8. **여러 파일에 걸친 변경**(archive, refile): **대상 파일을 먼저 저장하고 원본을 나중에 저장**한다.
   중간에 실패하면 최악의 경우에도 항목이 중복될 뿐 사라지지 않는다.
9. **undo 비활성화**: 장기 실행 buffer의 메모리 증가를 막는다.

`write-region`처럼 저장 훅을 우회하는 쓰기는 금지한다(실측: sync 전까지 인덱스에 없음). 다른 프로세스가
바꾼 파일은 node 조회 직전에 잡아낸다. 마지막 색인 때의 파일 크기·수정 시각과 지금을 비교하고(stat만),
다르면 `org-roam-db-sync`를 실행한다. sync는 내용 hash가 바뀐 파일만 다시 읽는다.

### 6.3 Emacs 전역 설정 (보안 관련)

`enable-local-variables nil`, `enable-local-eval nil`, `org-confirm-babel-evaluate t`,
`make-backup-files nil`, `create-lockfiles nil`, `auto-save-default nil`. Org 링크(`elisp:`, `shell:`)를
여는 함수와 babel 평가 함수는 어떤 method에서도 호출하지 않는다.

## 7. Task 의미론

### 7.1 상태

```
TODO ─▶ NEXT ─▶ DOING ─▶ DONE
  │       │        │
  └───────┴──▶ WAITING ──┘        (어느 상태에서든 CANCELLED 가능)
```

```elisp
(setq org-todo-keywords
      '((sequence "TODO(t)" "NEXT(n)" "DOING(s!)" "WAITING(w!)" "|" "DONE(d!)" "CANCELLED(c!)")))
```

- `@`(note 입력)는 쓰지 않는다. headless에서는 프롬프트가 되기 때문이다. 사유가 필요하면 API 필드로
  받고 adapter가 LOGBOOK에 기록한다.
- `org-log-done 'time`, `org-log-repeat 'time`, `org-log-into-drawer t`를 쓴다.
- 전이 순서는 Org가 강제하지 않는다. API는 어떤 상태에서든 전이를 허용하되 `expected_state`로
  낙관적 동시성을 검사한다.
- WIP limit: 개수가 `doing_limit`(기본 3)을 넘으면 `start` 응답에 `warnings`로 알린다. 거부하지는 않는다.

### 7.2 반복 task

반복 task를 완료해도 새 항목이 생기지 않는다. Org가 **같은 heading의 날짜를 다음 회차로 옮기고 상태를
되돌린다.** 그래서 ID는 바뀌지 않고, 완료 이력은 LOGBOOK에 남는다.

| repeater | 다음 날짜 (8/25 마감, 10/2 완료, 실측) | 용도 |
|---|---|---|
| `+1m` | 9/25: 한 번만 이동하므로 밀린 회차를 하나씩 처리한다 | 요금 납부 |
| `++1m` | 10/25: 오늘 이후가 될 때까지 이동한다 | 고정 날짜, 밀린 회차는 버림 |
| `.+1m` | 11/2: 완료일 기준 | 마지막으로 한 날로부터 간격 |

- **복귀 상태**: `org-todo-repeat-to-state "NEXT"`가 기본이다. task별 예외는 `REPEAT_TO_STATE`
  property로 지정하며, API에서는 `TODO`와 `NEXT`만 허용한다. Org 기본값(nil)은 시퀀스의 첫 상태
  TODO로 되돌리고, `t`는 직전 상태(DOING 등)로 되돌린다(실측). 둘 다 쓰지 않는다.
- **skip과 cancel**: Org에서는 반복 task를 CANCELLED로 바꿔도 이번 회차만 건너뛴다(실측).
  - `skip`: 이번 회차를 건너뛴다. Org 기본 동작이다.
  - `cancel`: 시리즈를 종료한다. repeater를 제거한 뒤 CANCELLED로 바꾼다.
  - 반복이 없는 task에서는 둘이 같다.
- **완료 조회**: 반복 task는 DONE 상태로 남지 않으므로, `completed?date=`는 상태가 아니라
  **LOGBOOK의 state-change 기록**으로 판정한다.

### 7.3 Agenda (today / overdue)

- agenda 버퍼를 만든 뒤 각 줄의 **text property**(`org-hd-marker`, `type`, `ts-date`)로 항목을
  추출한다. 화면 텍스트를 regex로 읽지 않는다.
- Org의 `type` 값: `scheduled`, `past-scheduled`, `deadline`(지난 deadline 포함), `upcoming-deadline`,
  `timestamp`(일정/약속).
- `today` = 지정 날짜의 1일 agenda다. 기본 경고 기간은 `org-deadline-warning-days 7`이고, 항목에 `-3d`
  같은 개별 경고가 있으면 그것이 우선한다. 완료 항목은 제외한다.
- `overdue` = today 결과 중 `past-scheduled`이거나 deadline 날짜가 기준일보다 앞선 항목이다. Org에는
  별도의 overdue view가 없으므로 날짜 비교 필터로 만든다.
- `waiting` = `org-tags-view`의 `TODO="WAITING"` 매치다.

## 8. API 계약 (v1)

모든 경로는 `/api/v1` 아래에 있다. 에러 본문은 RFC 9457(problem+json) 형식이다.

### 8.1 Task와 Project

| method | path | scope | 설명 |
|---|---|---|---|
| GET | `/meta` | read | `calendar_tz`, 서버 기준 오늘 날짜, 버전 |
| GET | `/agenda?date=` | read | 1일 agenda 전체 (task + 일정) |
| GET | `/tasks/today?date=` | read | agenda 중 task |
| GET | `/tasks/overdue?date=` | read | |
| GET | `/tasks/waiting` | read | |
| GET | `/tasks/completed?date=` | read | `CLOSED:`와 LOGBOOK 기반 |
| GET | `/tasks?state=&project=&tag=` | read | 날짜와 무관한 목록. 기본은 열린 상태 |
| GET | `/tasks/{id}` | read | |
| POST | `/tasks` | tasks:write | 생성. `Idempotency-Key` 지원 |
| PATCH | `/tasks/{id}` | tasks:write | 편집. `expected_version` 필수 |
| POST | `/tasks/{id}/{start,wait,complete,skip,cancel,todo,next}` | tasks:write | body: `{"expected_state":"NEXT"}` 필수 |
| GET | `/projects` | read | |
| POST | `/projects` | tasks:write | `projects/` 아래 파일 하나. `Idempotency-Key` 지원 |

정확한 형식은 `api/openapi.yaml`이 정한다. `date`를 생략하면 `calendar_tz` 기준 오늘이다.

### 8.2 MVP-B: Knowledge

| method | path | scope | 설명 |
|---|---|---|---|
| POST | `/nodes` | nodes:write | 생성(`title`, `body`, `tags`, `aliases`). `Idempotency-Key` 지원 |
| GET | `/nodes?q=&tag=` | read | 검색. `archive/`와 task는 제외 |
| GET | `/nodes/{id}` | read | |
| GET | `/nodes/{id}/backlinks` | read | 이 node로 `id:` 링크를 거는 node와 task |
| GET | `/nodes/{id}/links` | read | 이 node가 `id:` 링크로 가리키는 node와 task |

- 검색은 title과 alias의 부분 일치(대소문자 무시)와 tag 필터다. 초안의 `/nodes/search?q=` 대신
  `GET /tasks?state=`처럼 컬렉션에 필터를 붙인다. org-roam에는 본문 전문 검색이 없다. 전문 검색은 이후
  재생성 가능한 cache로 추가한다.
- 링크는 body에 쓴 Org ID 링크(`[[id:<uuid>][label]]`)다. backlinks와 links의 각 항목은
  `{id, title, kind}`이고, `kind`(`node`/`task`)로 어느 endpoint에서 읽을지 알려 준다.
- `nodes:write`는 `tasks:write`와 별개다. 노트만 쓰는 클라이언트(AI 에이전트 등)가 task를 바꿀 수 없게
  하기 위해서다.
- node 수정·삭제는 아직 없다.

### 8.3 이후 단계

- `GET/POST /journal/{date}`: org-roam-dailies 사용, append-only, AI 에이전트의 초안은 고정 ID heading에 넣고 교체한다.
- `/capture/*`
- node promotion: `POST /nodes`의 `source` 필드

### 8.4 Task 표현

```json
{
  "id": "6f1c…",
  "title": "Spotify 가족 요금제 납부",
  "state": "NEXT",
  "priority": "A",
  "tags": ["bills"],
  "scheduled": null,
  "deadline": { "date": "2026-10-25", "time": null, "repeat": "+1m", "warning_days": 3 },
  "repeat_to_state": "NEXT",
  "agenda": { "kind": "upcoming-deadline", "days": -23 },
  "project": { "id": "a9e2…", "title": "Household" },
  "closed_at": null,
  "location_hint": "tasks/recurring.org"
}
```

- `id`는 Org `:ID:`(UUID)이며 유일한 안정 식별자다. 파일 이동, refile, archive를 해도 바뀌지 않는다.
- `location_hint`는 디버깅용이다. 클라이언트는 이 값에 의존하지 않는다(계약 외).
- 날짜를 다음 회차로 옮기는 계산은 API가 하지 않는다. 생성할 때 받은 구조화 필드(`date`, `time`,
  `repeat`, `warning_days`)를 엄격히 검증하고, Org 타임스탬프 문법으로 조립하는 일만 한다.

### 8.5 입력 처리 규칙

- **title**: 한 줄, 최대 500자. 제어문자, priority cookie(`[#A]`), 앞의 `COMMENT`, 끝의 태그 목록은 거부한다.
- **tags**: 영문자·숫자·`_@#%`만. `ARCHIVE`는 agenda에서 task를 숨기므로 거부한다.
- **body**: Org 구조로 해석될 수 있는 줄(`*` heading, `#+` keyword, `:DRAWER:`, `CLOCK:`, `%%(`·`&%%(` diary
  sexp)은 Org 고유의 comma
  escape(`org-escape-code-in-string`)를 적용하고, 읽을 때 되돌린다. active timestamp `<…>`는 inactive
  `[…]`로 바꿔서 agenda에 섞여 들어가지 않게 한다.
- **ID 경로 인자**: UUID 형식만 받는다. **date**: `YYYY-MM-DD`만 받는다. 파일 경로는 입력으로 받지 않는다.
- capture template에 사용자 입력을 문자열로 이어 붙이지 않는다. `%(…)`가 eval되기 때문이다.

### 8.6 멱등성과 동시성

- 상태 전이 요청은 `expected_state`가 다르면 `409`를 반환한다. 재시도해도 반복 task가 두 회차 밀리지 않는다.
- `POST /tasks`와 `POST /projects`는 `Idempotency-Key`를 지원한다(토큰·endpoint별, 24시간). API는 응답을
  메모리 LRU에 저장해 그대로 재전송한다. 엔진도 키의 hash와 만든 ID를 메모리에 기억한다. 그래서 API가 엔진을
  기다리다 포기한(503) 요청을 엔진이 끝까지 처리했더라도, 같은 키로 재시도하면 새로 만들지 않고 그 결과를
  돌려준다. 엔진이 재시작하면 엔진 쪽 키는 사라진다.
- 쓰기는 Emacs 안에서 직렬화된다. RPC timeout(기본 10초)이 나면 `503`을 반환한다.

| RPC error code | HTTP |
|---|---|
| `invalid` | 422 |
| `not_found` | 404 |
| `conflict` | 409 |
| `prompt_blocked`, `internal` | 500 |
| timeout / socket 없음 | 503 |

## 9. 보안 경계

```
[Internet] ─▶ TLS 종료 reverse proxy 또는 터널 (배포 선택)
   ─▶ organon-api :8080                             ← 경계 1: 인증·scope·검증
   ─▶ rpc.sock                                      ← 경계 2: 화이트리스트 method
   ─▶ organon-engine (Network=none)                  ← 경계 3: Emacs 하드닝(§6.3)
   ─▶ <data>
```

| 위협 | 대응 |
|---|---|
| 임의 코드 실행 | `/eval` 없음. RPC는 화이트리스트. eval 가능한 server socket은 공유하지 않음. file-local 변수, babel, 링크 실행 차단 |
| API 컨테이너 탈취 | 데이터 mount 없음. 할 수 있는 일은 화이트리스트 method뿐이고 delete method도 없음 |
| Org 구조 injection | body escape, timestamp 비활성화(§8.5) |
| capture template eval | 사용자 입력을 template 문자열로 쓰지 않음 |
| path traversal | 경로 입력 없음. ID와 날짜는 형식 검증 |
| 토큰 유출 | 토큰은 SHA-256 해시로만 저장, scope 분리, 상수 시간 비교, 실패 rate limit |
| AI 에이전트 prompt injection | 에이전트 토큰은 `read` + `journal:write`(이후 단계)로 제한. task 상태 변경 권한 없음 |
| Emacs 컨테이너 탈출 경로 | `Network=none`, read-only rootfs, `--cap-drop all`, rootless + `keep-id` |

- 오픈소스 기본값: API는 `127.0.0.1`에 bind하고, 토큰 없이는 어떤 endpoint도 응답하지 않는다(probe인 `/livez`, `/readyz`, `/healthz` 제외. 이들은 상태만 알려 준다).
- TLS는 앞단(reverse proxy나 터널: Caddy, Cloudflare Tunnel, Tailscale 등)에서 종료한다. 프록시 뒤에서는
  `ORGANON_CLIENT_IP_HEADER`로 실제 클라이언트 주소를 받아 실패 rate limit에 쓴다.

## 10. 컨테이너와 스토리지

### 10.1 이미지

이미지는 하나이고 컨테이너는 둘이다.

```
FROM golang:1.27 AS build           → organon (Go 정적 바이너리)
FROM debian:trixie-slim             → emacs-nox, elpa-org-roam (apt 고정), organon.el
                                      native-comp AOT, organon 바이너리 복사
```

- 패키지는 Debian 패키지로 고정한다. 런타임에 MELPA나 네트워크에 접근하지 않는다.
- `emacs -Q`는 Debian elpa 패키지를 로드하지 못하므로 `-q`와 명시적 init을 쓴다(실측).
- native-comp는 빌드 시 AOT로 한다(프롬프트 차단용 trampoline 포함). 그러지 않으면 기동할 때마다 JIT가
  반복된다(실측).
- 공개 배포: `main`의 커밋이 CI를 통과하면 GHCR에 multi-arch(amd64, arm64) 이미지를 `:main`과 `:sha-<7>`
  태그로 올린다. 릴리스 태그는 아직 없다.

#### 이미지 중립 요구사항

이미지는 특정 런타임(Docker, Podman, Kubernetes)의 기능에 의존하지 않는다.

1. **임의의 non-root UID로 실행된다.** 고정 사용자나 홈 디렉터리를 가정하지 않는다. 쓰기 가능한 경로는
   데이터, cache, run 세 곳뿐이고 모두 환경변수로 지정한다.
2. **read-only rootfs**에서 동작한다.
3. `organon-engine`는 **네트워크 없이**(lo만) 동작한다.
4. 두 컨테이너가 named volume의 Unix socket으로 통신한다. 같은 UID로 실행된다는 것만 가정한다.
5. healthcheck는 Dockerfile `HEALTHCHECK`에 의존하지 않는다(podman이 OCI 형식으로 빌드하면 버림).
   각 배포 정의가 `organon healthcheck` / `organon rpc-ping`을 명시한다.
6. 설정은 환경변수와 `organon.json`으로만 받는다.

### 10.2 배포 정의: 지원 수준

| 정의 | 위치 | 지원 수준 |
|---|---|---|
| **Docker Compose** | `compose.yaml` | **공식 지원.** README와 `docs/deployment.md`의 기본 설치 경로 |
| Podman Quadlet | `contrib/quadlet/` | 예시. "메인테이너가 실제로 쓰는 구성"이지만 지원은 약속하지 않음 |

Quadlet을 예시로 두는 이유: 셀프호스팅 프로젝트의 사실상 표준은 Compose다. GitHub 코드 검색으로
대략 세어 보면 Quadlet 파일은 Compose 파일의 약 1/300이다(2026-10-02). 다만 Quadlet이 요구하는
제약(rootless, `keep-id`, `Network=none`)은 이미지 중립 요구사항과 같으므로 **CI가 rootless Podman
e2e로 계속 검증한다**(§16).

### 10.3 컨테이너

| | `organon-engine` | `organon-api` |
|---|---|---|
| Exec | `emacs -q --fg-daemon -l /opt/organon/emacs/init.el -f organon-start` | `organon serve` |
| Network | 없음 (Compose `network_mode: none` / Quadlet `Network=none`) | 앱 network |
| 데이터 (`<data>`) | rw bind | **mount 없음** |
| `organon-cache` volume | rw | — |
| `organon-run` volume | rw (socket 생성) | rw (socket 연결) |
| 토큰 | — | secret 파일 (Compose `secrets` / podman secret) |
| Health | `organon rpc-ping` | `organon healthcheck` |
| 기동 순서 | — | Compose `depends_on: service_healthy` / Quadlet `Requires=` + `Notify=healthy` |
| 파일 소유권 | Compose: `user: "${UID}:${GID}"` / Quadlet: `UserNS=keep-id` | 같은 UID |

- rootless에서 두 컨테이너의 socket 공유, `keep-id`로 호스트 uid 소유 파일 생성, `Network=none`
  (lo만 존재)을 모두 실측으로 확인했다.
- Quadlet `.pod`는 쓰지 않는다. pod는 network namespace를 공유하므로 Emacs만 `Network=none`으로 둘 수 없다.
- Docker는 rootful이 기본이다. `user:`를 지정하지 않으면 데이터 파일이 root 소유가 되므로 `compose.yaml`
  기본값에 포함한다.

### 10.4 메인테이너 배포

- 메인테이너는 자신의 인프라 저장소에서 `contrib/quadlet/`을 원본으로 삼아 Quadlet으로 배포한다.
  그 저장소와 호스트 세부 사항은 이 저장소의 범위 밖이다.
- `contrib/quadlet/`이 바뀌면 릴리스 노트에 적고, 실제 배포 구성과의 차이를 L3 체크리스트에서 확인한다.

### 10.5 실패와 복구 모델

| 상황 | 동작 / 복구 |
|---|---|
| Emacs crash | `Restart=always`. 모든 변경은 즉시 저장되므로 유실 없음. 진행 중이던 요청은 503 |
| 프롬프트 유발 상황 | 즉시 `prompt_blocked` 에러. hang 없음 |
| 외부에서 파일 변경 | buffer가 깨끗하면 자동 revert, 아니면 409 |
| 저장 실패 | buffer를 디스크 상태로 되돌리고 500. 메모리에만 있는 변경 없음 |
| org-roam DB 손상/삭제 | DB가 없거나 읽을 수 없으면 기동할 때 자식 batch Emacs가 임시 파일에 새 DB를 만들고, 끝나면 데몬이 rename으로 교체한 뒤 그사이 쓴 파일을 증분 sync한다(1,000 노트 기준 약 25초). 엔진은 바로 healthy이고 task는 계속 동작한다. 그동안 node 요청은 `503 index_rebuilding` + `Retry-After`, 진행률은 `/meta`의 `index`. 재구축이 실패하면 node 요청은 `internal`이고 재기동하면 다시 시도한다 |
| 엔진 실행 중 외부 파일 변경 (node) | 다음 node 조회 직전에 바뀐 파일만 재색인. 재기동 불필요 |
| API 재시작 | Idempotency 캐시만 사라지고, 상태 전이는 `expected_state`로 보호됨 |
| 컨테이너/volume 전부 삭제 | `<data>`만으로 재기동 (Acceptance 8) |
| **비상 수동 편집** | `organon-engine`를 정지 → 파일 편집 → 기동. 기동할 때 파일을 새로 읽고 sync함 |
| 데이터 손실 | restic 복원(§11) |

## 11. 백업

- 대상: `<data>` 전체(org, attachments, `organon.json`). cache는 제외한다.
- 방식: **restic**(스냅샷을 지원하지 않는 파일시스템이어도 동작), 일 단위, 저장소는 반드시 호스트 밖(off-site)에 둔다.
- 일관성: 파일 단위 저장이 atomic rename이므로 파일 하나가 깨진 상태로 백업되지는 않는다. 여러 파일에
  걸친 변경은 §6.2-8의 순서 덕분에 "중복은 가능, 손실은 불가"다.
- 복원 리허설: 빈 디렉터리에 restore → 기동 → e2e smoke를 문서화된 절차로 정기 실행한다.
- Git은 데이터 history로 쓰지 않는다. 코드, Emacs 설정, 배포 설정에만 쓴다.
- 백업 timer 자체는 배포하는 쪽의 인프라에서 구현한다(이 저장소의 범위 밖).

## 12. AI 에이전트 연동 (향후)

원칙: **Facts → deterministic code, Narrative → LLM.**

- **Morning brief** (UTC cron): `GET /tasks/today`, `/overdue`, `/waiting` → 에이전트가 요약한다. 우선순위와
  마감 판단은 API가 준 사실을 그대로 쓴다.
- **/day-close** (사용자 trigger): completed, 남은 task, 오늘 변경한 node(이후 단계: node 단위 변경 추적은
  재생성 가능한 hash cache가 필요함)를 조회하고 → 초안을 만들어 → `POST /journal/{date}`로 쓴다. 초안은
  고정 heading에만 쓰고, 사람이 쓴 내용은 덮어쓰지 않는다.
- **Knowledge promotion**: 에이전트가 후보만 제시하고, 사용자가 [생성 / 병합 / 무시]를 고른다. 생성은
  `POST /nodes` + `source`(journal 원문 링크)로 한다.
- 캘린더와 Git activity는 에이전트가 각 출처에서 직접 조회한다. Organon은 캘린더가 아니다.
- 에이전트가 MCP를 쓰면 이 API 위에 얇은 MCP adapter를 둔다(같은 토큰 scope 적용).
- 에이전트도 다른 클라이언트와 같은 경로(앞단 프록시 + 토큰)로 접속한다.

## 13. 기술 선택

| 영역 | 선택 | 이유 |
|---|---|---|
| 엔진 | Emacs 30.1 + Org 9.7 + org-roam 2.3 (Debian 패키지) | 성숙하고, 내장 sqlite·JSON이 있으며, arm64 패키지로 고정 가능 |
| API | Go 1.27, **표준 라이브러리만** | 작은 언어라 처음 보는 사람도 읽기 쉽다. 정적 바이너리라 이미지가 작다. 에러 처리가 명시적이다. Unix socket과 HTTP가 표준 라이브러리에 있다 |
| IPC | Unix socket + JSON-RPC (Emacs 내장 `make-network-process`) | eval 경로가 없고 실측 지연이 작다 |
| 배포 | Docker Compose (공식), Podman Quadlet (예시) | 셀프호스팅의 사실상 표준 + 메인테이너 인프라 |
| 테스트 시계 | `faketime` | `org-today`까지 고정됨(실측) |
| 개발 도구 고정 | mise (`mise.toml`: go, node, OpenSpec CLI) | 기여자와 CI가 같은 버전을 쓴다. Emacs는 이미지로 고정하므로 제외 |
| 스펙 관리 | OpenSpec (spec-driven schema) | 가볍고, change 단위 delta가 단계별 MVP와 맞으며, WHEN/THEN 시나리오가 테스트로 그대로 이어진다 |

## 14. 저장소 구조

```
organon/
├── README.md
├── CONTRIBUTING.md
├── mise.toml                  개발 도구 버전 고정
├── compose.yaml               공식 배포 정의
├── docs/architecture.md
├── openspec/                  동작 계약 (config.yaml, specs/, changes/)
├── emacs/
│   ├── init.el                전역 설정(§6.3, §7.1)
│   ├── site-start.el, build.el  런타임 JIT 차단, AOT 컴파일
│   ├── organon.el             RPC 서버 + dispatch + 변경 래퍼
│   ├── organon-task.el        task·project method
│   └── test/                  ERT
├── api/                       Go module
│   ├── openapi.yaml           HTTP 계약
│   ├── cmd/organon/           serve | healthcheck | rpc-ping | init | token | CLI 명령
│   └── internal/{rpc,httpapi,auth,idem,instance,model,client,cli}
├── container/Containerfile
├── contrib/quadlet/           Podman Quadlet 예시
├── scripts/                   e2e.sh (--runtime compose|podman), test-elisp.sh, check-openspec-archived.sh
├── tests/fixtures/            고정 org 트리 + golden 결과
└── .github/workflows/         L2 CI, main 이미지 publish
```

## 15. MVP 범위와 Acceptance

| 단계 | 내용 | Acceptance |
|---|---|---|
| **MVP-A** | RPC, task 조회/생성/전이, repeater, agenda, 컨테이너, bind mount, 영속성 | S1 생성, S2 반복, S3 Today, S4 완료, S8 재생성 |
| task-essentials | task 편집, 목록·필터, 프로젝트 생성·목록, 재개(`todo`/`next`), CLI | `openspec/specs/{task-listing,projects,cli}` |
| **MVP-B** | org-roam node 생성/조회/검색/backlink, 재구축 | S5 node, S6 backlink, S7 재구축, S8 재확인 |
| 이후 | journal, capture, completed 고도화, 전문 검색, AI 에이전트 연동, 메인테이너 인프라 배포, restic | — |

검증 기준(요약):

- **S2**: 위 §7.2 표의 세 벡터와 `-3d` 경고, `REPEAT_TO_STATE`, skip/cancel 차이를 faketime으로 확인한다.
  API 코드에 날짜 덧셈 로직이 없어야 한다.
- **S3**: 고정 날짜에서 scheduled, past-scheduled, deadline(지난 것), upcoming-deadline, 완료 항목 제외,
  하위 디렉터리 project task 포함을 확인한다.
- **S7**: DB 삭제 전후의 node 수, link 수, 특정 backlink 집합이 같아야 한다.
- **S8**: 컨테이너와 volume 삭제 전후의 `<data>` sha256 manifest가 같고, API 결과도 같아야 한다.

## 16. 테스트 전략

### 16.1 테스트 종류

- **Elisp ERT** (이미지 안에서 `emacs --batch` + `faketime`): fixture 트리를 tmp에 복사하고 method를
  호출한 뒤, 결과 JSON과 **결과 `.org` golden diff**를 비교한다.
- **Go 단위 테스트**: fake RPC server로 라우팅, 인증, scope, 검증, 에러 매핑, UTC 변환(DST 벡터 포함),
  응답의 `openapi.yaml` 적합성을 확인한다.
- **e2e** (`scripts/e2e.sh --runtime compose|podman`): 실제 컨테이너 2개를 띄우고 curl로 S1–S8을 실행한다.
  시나리오는 한 벌만 유지한다.
- **장애 주입**: RPC 도중 Emacs kill, 외부 파일 변경, 디스크 가득 참(작은 tmpfs) 상황을 만든다.
- **동시성**: POST 50개를 병렬로 보낸 뒤 heading 수와 ID 유일성을 확인한다.
- **soak** (배포 전): 24시간 동안 분당 mutation을 보내며 RSS, 응답 p99, buffer 수를 관찰한다.

스펙 추적: OpenSpec scenario 하나마다 그것을 검증하는 테스트 하나 이상을 둔다. 테스트 이름이나
주석에 scenario 제목을 적는다.

### 16.2 검증 단계 (누가 무엇을 책임지는가)

| 단계 | 책임 | 내용 | 시점 |
|---|---|---|---|
| L1 | 기여자 (로컬) | `mise run test`: ERT(이미지 안) + Go 단위 테스트. 컨테이너 런타임 하나와 mise만 필요 | PR 전 |
| L2 | CI (머지 필수 체크) | ① Docker Compose e2e ② rootless Podman e2e (이미지 중립 요구사항 그대로) ③ `quadlet -dryrun`으로 `contrib/quadlet/` 문법 검증 ④ amd64 + arm64 ⑤ `openspec validate` | 모든 PR |
| L3 | 메인테이너 | 실제 호스트에서 Quadlet + systemd 배포 smoke, 배포 구성과 `contrib/quadlet/`의 차이 확인 | 릴리스 전 |

기여자의 의무는 "L1 통과 + 관련 스펙 시나리오와 테스트 동반 + L2가 초록"이다. 기여자가 Podman이나
systemd를 갖고 있을 필요는 없다.

## 17. 열린 항목

- `calendar_tz` 변경 이력(`tz_history`)
- 여러 사람이 함께 쓸 때 토큰별 actor를 LOGBOOK에 기록하는 기능
- 전문 검색 cache 방식 (`rg` 위임 vs SQLite FTS)
- node 단위 변경 추적 (/day-close의 "오늘 바뀐 노트")
- 공개 저장소 문서 언어 (현재 한국어, 공개 전 영어판 검토)

---

## 부록 A. 스파이크 실측 결과

2026-10-02, Raspberry Pi 4 (`dev`), Debian 13.

| 항목 | 결과 |
|---|---|
| repeater `+1m`/`++1m`/`.+1m` (8/25 마감, 10/2 완료) | 9/25 / 10/25 / 11/2 |
| `<2026-10-25 Sun +1m -3d>` 완료 | 11/25로 이동, `-3d` 유지 |
| 반복 후 상태 | 기본값은 첫 keyword(TODO). `REPEAT_TO_STATE`가 우선함. `t`는 직전 상태 |
| CANCELLED + repeater | repeater 발동(회차 건너뛰기) |
| LOGBOOK (batch/데몬) | `org-add-log-note`를 직접 호출하지 않으면 누락됨 |
| agenda 디렉터리 항목 | 재귀 탐색 안 함 |
| agenda 지연 | 첫 호출 2.9s, warm 17ms. task 300개/파일 30개에서 0.33s |
| org-roam 전체 sync (노트 1,000 + task 300, 링크 5,300) | 25.4s. 증분 0.56s, 저장 시 autosync 0.03s |
| org-roam node include 함수로 task 제외 | 파일 수준 node까지 제외됨(가드 필요). task에서 나가는 backlink 소실 |
| `todo IS NULL` 필터 | 검색에서 task 제외, backlink 유지 |
| `write-region`으로 쓴 파일 | sync 전까지 인덱스에 없음 |
| 외부 변경 + `find-file-noselect` | "Reread from disk?" 프롬프트 → `revert-without-query`로 해결. 수정된 buffer는 차단 에러 |
| JSON-RPC Unix socket | ping 0.6ms, UTF-8 왕복 정상 |
| 데몬 RSS | 62MB |
| rootless 컨테이너 2개 | socket 공유 OK, `keep-id`로 uid 1000 소유, `Network=none`은 lo만 |
| 컨테이너 기본 TZ | UTC |
| TZ 비교 (KST 10-03 08:30 = UTC 10-02 23:30) | UTC: 오늘 10-02, `.+1m` → 11-02, 로그 `10-02 23:30`. Asia/Seoul: 10-03, 11-03, `10-03 08:30` |
| `faketime` | `org-today` 고정됨 |
| `emacs -Q` | Debian elpa 패키지를 로드하지 못함 |
| native-comp | 기동할 때마다 JIT (`.eln` 5개) |
| 이미지 빌드 (trixie-slim + emacs-nox + org-roam) | 1분 27초, 435MB |
