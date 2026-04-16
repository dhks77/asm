# Multi-root Session Launcher — Spec

## 배경

현재 asm은 하나의 `rootPath`를 기준으로 동작한다.

- **dir mode**: `rootPath` 하위 디렉토리를 스캔해서 후보를 만든다.
- **repo mode**: `rootPath` 가 속한 단일 repo의 `git worktree list` 결과를 후보로 만든다.

이 구조는 단순하지만, 실제 사용 패턴과는 어긋난다.

1. 하나의 asm 인스턴스에서 여러 repo / 여러 plain dir 을 동시에 다루기 어렵다.
2. picker가 "열려 있는 세션 목록"과 "열 수 있는 대상 목록"을 동시에 맡아 역할이 섞여 있다.
3. `←/→` 는 browse를 위해 asm/tmux session 자체를 재시작하므로 UX 비용이 크다.
4. window 이름, provider state, 시작 시각 등이 basename 중심이라 같은 이름의 target끼리 충돌 위험이 있다.
5. 설정도 `rootPath` 하나를 기준으로 합쳐져 있어서, "어디서든 열고 target별로 다르게 동작"하는 모델로 확장하기 어렵다.

사용자가 원하는 쪽은 더 가깝다.

- asm은 **아무 곳에서나 띄울 수 있는 앱**처럼 느껴져야 한다.
- picker는 "지금 열려 있는 세션"만 보여주는 전환기여야 한다.
- 새 세션을 여는 행위는 launcher modal에서 수행되어야 한다.
- launcher는 plain dir 과 repo/worktree 둘 다 찾을 수 있어야 한다.
- 설정은 path가 늘어날수록 폭발하지 않도록 계층이 단순해야 한다.

---

## 목표

1. asm은 별도 준비 단계 없이, 아무 경로에서나 바로 실행할 수 있어야 한다.
2. picker panel은 항상 **open session list**만 표시한다.
3. 새 세션 생성은 별도 **launcher modal**에서 수행한다.
4. launcher에서 다음 두 흐름을 모두 지원한다.
   - plain dir 을 찾아서 세션 열기
   - repo를 선택한 뒤 main repo 또는 linked worktree 중 하나 열기
5. 여러 asm 인스턴스를 동시에 실행할 수 있어야 한다.
6. target별 설정을 지원하되, 설정 구조는 사용자가 설명 가능한 수준으로 유지한다.

---

## 비목표

1. 디스크 전체를 재귀적으로 인덱싱하는 전역 파일 검색기 구현
2. 전역 singleton asm 프로세스 강제
3. 서로 다른 asm 인스턴스 사이의 open session 공유
4. 동일 target에 대한 다중 AI session 동시 생성
5. v1에서 bookmark / pin / recent sync / fuzzy global search까지 모두 구현

---

## 핵심 원칙

### 1. Launch Anywhere

asm은 먼저 관리 루트를 정해두고 그 안에서만 움직이는 도구가 아니다.
그냥 실행하면 launcher가 뜨고, 거기서 원하는 곳을 찾아 세션을 여는 앱에 가깝다.

`--path` 는 유지하되 의미를 바꾼다.

- 기존: 단일 browse root
- 변경 후: launcher의 **초기 진입 위치 힌트**

예:

```bash
asm
asm --path ~/projects
asm --path ~/sandbox/infra
```

세 경우 모두 앱은 동일하게 동작하고, launcher가 처음 어느 위치를 보여줄지만 달라진다.

### 2. Picker = Open Session List

picker는 catalog가 아니다.
현재 asm 인스턴스 안에서 **열려 있는 세션들만** 보여준다.

### 3. Launcher = Target Catalog

새 세션을 어디에 붙일지 찾는 일은 launcher가 맡는다.
Directories/Repos/Recent 는 launcher 안의 browse state일 뿐, asm 전체 상태를 다시 시작시키지 않는다.

### 4. Session Identity = Absolute Path

basename이 아니라 absolute path 가 내부 key다.

- `/Users/nhn/worktrees/api-4012`
- `/Volumes/ssd/worktrees/api-4012`

는 이름이 같아도 다른 target이다.

### 5. Settings Are Layered, Not Accumulated

설정 문제를 해결하려고 `~/.asm/config.toml` 하나에 path rule을 계속 누적하는 모델은 피한다.
기본값, local config, 개인 예외를 분리해서 본다.

---

## 제안 UX

## 1. Main Picker = Open Session List

picker는 더 이상 "탐색 가능한 directory/worktree 전체 목록"을 보여주지 않는다.
항상 현재 asm 인스턴스 안에서 **열려 있는 세션들만** 보여준다.

### row 정보

- display name
- repo / worktree 문맥
- branch / task name
- provider
- state (`idle`, `thinking`, `tool-use`, `responding`)
- elapsed
- path hint

예시:

```text
● Fix login retry bug           [claude] thinking   8m
  repo: app-api · worktree: feature/4021
  ~/worktrees/app-api-4021

● infra-sandbox                 [codex] idle       31m
  plain dir
  ~/sandbox/infra
```

### 정렬

- 1순위: currently fronted session
- 2순위: 나머지 open session 을 `last_focused_at desc`

### empty state

open session이 0개면 launcher 진입 안내를 보여준다.

```text
No open sessions

Press Ctrl+N to launch a session
```

### 검색

검색은 세션 목록에만 적용한다.

- display name
- branch
- task name
- repo name
- path

---

## 2. Launcher Modal

새 세션 생성 전용 UI.

호출 키:

- `Ctrl+N`: launcher 열기
- empty state 에서 `Enter`: launcher 열기

launcher는 settings/worktree dialog처럼 **working panel 전체를 차지하는 modal 화면**으로 본다.

### 모드

v1은 3개 모드로 시작한다.

1. `Recent`
2. `Directories`
3. `Repos`

#### Recent

최근 열었던 target 목록.

- last opened / last focused 순
- 이미 열려 있는 대상은 badge 표시
- `Enter`:
  - 이미 open → 해당 세션 focus
  - 미오픈 → 세션 생성

#### Directories

lazy directory browser.

- 전체 디스크 recursive scan은 하지 않는다.
- 현재 디렉토리의 direct children만 `os.ReadDir` 로 읽는다.
- hidden dir 기본 숨김
- plain dir / repo dir 모두 launch target 가능

기본 동작:

- `Enter`: 선택한 directory를 launch target으로 연다
- `→` or `l`: 해당 directory 안으로 들어감
- `←` or `h`: 부모로 이동
- `/` 또는 타이핑: 현재 레벨 검색

##### 초기 진입 위치

Directories 탭은 아래 순서로 시작 위치를 정한다.

1. `--path` 로 전달된 경로
2. 마지막으로 launcher가 보고 있던 경로
3. asm 실행 당시의 current working directory
4. `~`

즉 browse root가 고정되어 있지 않다.

#### Repos

현재 launcher 위치를 기준으로 repo 후보를 보여준다.

repo 후보 규칙:

1. 현재 디렉토리 자체가 repo면 후보에 포함
2. 현재 디렉토리의 direct child 중 `.git` 이 있는 디렉토리를 후보에 포함
3. 재귀 discovery는 하지 않음

repo 선택 후 2단계로 worktree 목록을 연다.

- main repo
- linked worktrees (`git worktree list`)

예시:

```text
Repos
  app-api
  billing
  devtools

app-api
  main
  feature/4012
  bugfix/3988
```

### 중복 target 처리

동일 absolute path target은 **같은 asm 인스턴스 안에서** 1개 AI session 만 허용한다.

- launcher에서 이미 열려 있는 target을 선택하면 새로 만들지 않고 기존 세션으로 focus

### cross-instance duplicate target

다른 asm 인스턴스에서 이미 같은 absolute path target을 열어둔 상태라면, v1 기본 동작은 **사용자에게 기존 세션을 닫고 여기서 이어서 열지, 새로 열지 묻는 것**이다.

즉 정책은 아래와 같다.

1. **same instance**
   - 같은 asm 안에 이미 열려 있으면 기존 세션 focus
2. **different instance**
   - 다른 asm 이 같은 target을 열고 있으면 prompt 를 띄운다

추천 UX:

```text
This target is already open in another asm session.

Close the existing session and reopen it here?

- Reopen here and continue previous conversation
- Reopen here as a new session
- Cancel
```

기본 선택은 `Reopen here and continue previous conversation`.

#### reopen here and continue 동작

`Reopen here and continue previous conversation` 을 고르면:

1. 기존 owner asm 에서 해당 target session을 종료한다
2. 필요하면 hidden window / terminal window / session metadata 를 정리한다
3. 현재 asm 인스턴스에서 그 target을 다시 열고, 가능한 경우 **직전 대화**를 이어서 resume 한다

즉:

- 기존 세션은 닫힌다
- 현재 asm이 새로운 owner가 된다
- attach/switch는 하지 않는다
- 사용자가 보던 대화를 이어올 수 있다

#### reopen here as a new session 동작

`Reopen here as a new session` 을 고르면:

1. 기존 owner asm 에서 해당 target session을 종료한다
2. 필요하면 hidden window / terminal window / session metadata 를 정리한다
3. 현재 asm 인스턴스에서 그 target을 **fresh session** 으로 새로 연다

즉:

- 기존 세션은 닫힌다
- 현재 asm이 새로운 owner가 된다
- 이전 대화는 이어오지 않는다

#### 왜 이렇게 가는가

이 방식이 가장 덜 애매하다.

1. 같은 target에 대한 owner가 항상 하나라서 상태 해석이 쉽다.
2. "지금 이 dir은 어느 asm에서 작업 중인가"가 분명하다.
3. attach처럼 포커스만 이동하는 애매한 상태가 없다.
4. 사용자는 상황에 따라 "이어서 할지 / 새로 할지"를 선택할 수 있다.

#### provider resume semantics

이 정책에서는 provider resume이 **사용자 선택과 일치하게** 동작해야 한다.

예를 들어 Claude/Codex가 "현재 cwd의 마지막 세션"을 다시 붙는 방식이면:

- asm A 에서 연 세션
- 그걸 닫고 asm B 에서 `continue` 또는 `new` 로 다시 여는 세션

이 둘이 구분되지 않으면 UX가 깨진다.

따라서 v1 요구사항에 아래를 추가한다.

1. asm은 `continue previous conversation` 과 `start fresh` 를 구분해서 호출할 수 있어야 한다.
2. `continue` 를 고르면 가능한 경우 직전 대화를 이어와야 한다.
3. `new` 를 고르면 이전 대화를 resume 하지 않아야 한다.

스펙 수준에서는 우선 아래 요구를 둔다.

- cross-instance duplicate target detection 은 한다
- cross-instance attach/switch 는 하지 않는다
- 사용자가 확인하면 기존 owner를 닫고 여기서 continue 또는 fresh reopen 한다
- provider resume은 장기적으로 `continue` 와 `fresh` 를 안정적으로 구분할 수 있어야 한다

### worktree 생성

`Repos` 모드에서 repo가 선택된 상태면 `Ctrl+W` 로 기존 worktree 생성 dialog를 열 수 있다.

생성 성공 후:

1. worktree 목록 refresh
2. 새 worktree를 current selection 으로 이동
3. 이어서 Enter 로 세션 시작 가능

---

## 3. 키 동작 변경

| 키 | 변경 전 | 변경 후 |
|----|--------|--------|
| `Enter` | 현재 목록의 directory/worktree 열기 | picker에서는 선택한 open session focus. empty state에서는 launcher 열기 |
| `Ctrl+N` | 현재 cursor target에 새 세션 | launcher 열기 |
| `←` / `→` | asm 재실행 기반 rootPath 이동 | main picker에서는 제거. launcher 안에서 hierarchy 이동 |
| `Ctrl+W` | repo mode에서만 현재 rootPath 기준 worktree 생성 | launcher repo context 또는 repo-backed session context에서 repo 선택 후 worktree 생성 |
| `Ctrl+G` | picker <-> working focus | 유지 |
| `Ctrl+T` | terminal toggle | 유지 |
| `Ctrl+]` | active session rotate | picker row 기준으로 유지 |

`←/→` 재실행 네비게이션은 제거한다. browse는 launcher 내부 상태 전환으로 해결하고, asm/tmux session 자체는 유지한다.

---

## 상태 모델

## 1. SessionRef

```go
type SessionRef struct {
    ID            string    // stable hash of absolute target path
    TargetPath    string
    DisplayName   string
    RepoRoot      string    // empty for plain dir
    RepoName      string    // empty for plain dir
    Branch        string
    Provider      string
    Kind          SessionKind // AI / Terminal / AI|Terminal
    StartedAt     time.Time
    LastFocusedAt time.Time
}
```

규칙:

- picker 내부 key는 `DisplayName` 이 아니라 `ID`
- task / branch / provider / startedAt 등 기존 name-keyed map도 `SessionID` 또는 `TargetPath` 기준으로 이동

## 2. LaunchTarget

```go
type LaunchTarget struct {
    Path        string
    Kind        TargetKind // plain_dir / repo_main / repo_worktree
    RepoRoot    string
    RepoName    string
    DisplayName string
}
```

---

## tmux 아키텍처 변경

## 1. tmux session naming

더 이상 path 기반 singleton을 두지 않는다.
top-level `asm` 실행 한 번이 tmux session 하나를 만든다.

예시:

```text
asm-api-8f31c2
asm-sandbox-a91d44
asm-20260416-103501
```

요구사항:

- 서로 다른 asm 인스턴스는 항상 동시에 공존 가능
- session name 충돌을 path로 막지 않고, unique suffix로 막음
- 같은 경로에서 asm을 두 번 켜도 두 인스턴스가 따로 뜰 수 있음

v1에서는 "이전에 떠 있던 asm 인스턴스에 자동 재attach"를 전역적으로 해결하려고 하지 않는다.

## 2. hidden window naming

현재 `wt-<basename>`, `term-<basename>` 방식은 multi-root에서 충돌한다.

다음 형태로 변경한다.

- AI: `ai-<session-id-short>`
- terminal: `term-<session-id-short>`

basename은 window 이름에 직접 넣지 않는다.

### metadata 저장

tmux window option 또는 pane option에 아래 값을 저장한다.

- `@asm-id`
- `@asm-path`
- `@asm-display-name`
- `@asm-repo-root`
- `@asm-repo-name`
- `@asm-kind`
- `@asm-provider`
- `@asm-started-at`
- `@asm-last-focused-at`

이 metadata를 기반으로 picker가 open session list를 복원한다.

## 3. session rehydration

asm 인스턴스 내부에서 picker/dialog subprocess가 다시 뜰 때는 directory scan으로 목록을 재구성하지 않는다.

대신:

1. tmux window metadata 조회
2. `@asm-path` 가 있는 window만 asm-managed hidden session 으로 간주
3. 해당 metadata로 `SessionRef` 목록 재구성

결과:

- browse 위치와 open session state가 분리된다
- launcher가 어디를 보고 있든 open session list는 정확히 유지된다

---

## 설정 모델

이 스펙에서 더 중요한 축은 multi-root 자체보다 **설정 계층을 어떻게 단순하게 유지하느냐**다.

핵심 결론:

- 전역 config 하나에 per-dir / per-repo path rule을 계속 누적하는 방식은 기본안으로 채택하지 않는다.
- 대신 `global defaults + target-local config` 의 2층으로 간다.

## 1. User Global

경로: `~/.asm/config.toml`

용도:

- 개인 전역 기본값
- credential / token / secret
- provider binary override
- tracker credential
- IDE registry
- picker / launcher UI 기본값

예시 필드:

- `default_path` (launcher 초기 진입 기본값)
- `picker_width`
- `desktop_notifications`
- `providers.*`
- `trackers.*`
- `ides.*`
- 전역 fallback `default_provider`
- 전역 fallback `default_tracker`
- 전역 fallback `default_ide`

## 2. Target-local Config

경로는 target 종류에 따라 다르다.

### repo-backed target

경로: `<repo-root>/.asm/config.toml`

main repo 와 linked worktree 는 같은 repo-root config를 공유한다.

### plain dir target

경로: target path 에서 시작해 위로 올라가며 찾은 **nearest** `.asm/config.toml`

즉 git repo는 아니지만 어떤 상위 폴더 아래의 작업 디렉토리들이 공통 규칙을 갖고 싶을 때 쓸 수 있다.

용도:

- 그 target 또는 그 target 그룹의 기본 provider/tracker/IDE
- repo/worktree 관련 기본값

예시 필드:

- `default_provider`
- `default_tracker`
- `default_ide`
- `worktree_base_path`
- `worktree_template.*`

## 3. Precedence

최종 precedence:

```text
user global
  < target-local config
```

즉:

- repo나 dir 자체가 선언한 기본값은 존중
- 전역 기본값은 target-local config에서 덮어쓴다

## 4. 무엇을 어디에 둘 것인가

### user global 에 둬야 하는 값

- secret / token / credential
- provider command / args
- tracker credential
- IDE binary definitions
- `picker_width`
- `desktop_notifications`

### target-local config 에 두는 값

- `default_provider`
- `default_tracker`
- `default_ide`
- `worktree_base_path`
- `worktree_template.*`

반대로 아래 값은 target-local config에 두지 않는다.

- `picker_width`
- `desktop_notifications`
- provider command / args
- tracker credential

이 값들은 특정 target 하나보다 앱 전체 / 사용자 전체 성격이 더 강하기 때문이다.

## 5. 왜 전역 path registry 하나로 안 가는가

`~/.asm/config.toml` 안에 이런 식으로 계속 쌓는 모델은 피한다.

```toml
[path_rules."/Users/nhn/projects/billing"]
default_provider = "codex"
```

이 모델은 시간이 갈수록 아래 문제가 커진다.

1. 파일이 path registry 역할까지 떠안아 빠르게 비대해진다.
2. repo 이동 / 경로 변경 후 stale entry 정리가 어렵다.
3. repo 공통 설정과 전역 기본값이 한 파일에서 뒤섞인다.
4. settings UI에서 "이 값이 어디서 왔는지" 설명하기 어려워진다.

따라서:

- repo/shared default → target-local config

로 역할을 분리한다. path별 개인 예외 파일은 v1 범위에서 도입하지 않는다.

## 6. Settings UI 제안

settings는 선택한 session 또는 launcher에서 현재 선택한 target 기준으로 scope를 보여준다.

scope:

1. `Global`
2. `Target Local`

### Global

`~/.asm/config.toml` 편집

### Target Local

- repo-backed target이면 `<repo-root>/.asm/config.toml`
- plain dir target이면 nearest `.asm/config.toml`
- 없으면 "Create local config for this target" 액션 제공

이 구조면 사용자는 항상 이렇게 이해할 수 있다.

- 전역 기본값
- 이 target 자체 기본값

---

## Discovery 규칙

## 1. Directories 탭

- current launcher location 을 top-level context 로 사용
- lazy load only
- hidden dir 숨김
- symlink traversal 안 함
- plain dir도 launch target 허용

### repo badge

directory가 git repo면 badge를 보여준다.

```text
backend
docs-site [repo]
sandbox
```

repo dir는 두 가지 방식으로 사용할 수 있다.

1. 그대로 repo-main target으로 열기
2. Repos 탭에서 worktree selection 흐름으로 들어가기

## 2. Repos 탭

repo candidate는 **현재 launcher 위치 기준**으로 수집한다.

- current dir itself if `.git`
- direct child with `.git`

이 규칙을 택하는 이유:

1. 디스크 전체 recursive search는 느리고 예측 가능성이 낮다.
2. "아무 데서나 실행" UX와도 잘 맞는다. 사용자는 launcher 위치만 옮기면 된다.
3. 더 깊은 repo는 Directories 탭으로 그 위치까지 이동한 뒤 Repos 탭을 열면 된다.

---

## Worktree 관리 변경

현재 `Ctrl+W` 는 repo mode에서만 의미가 있다.
변경 후에는 "현재 browse root가 repo인지"보다 "repo context가 있는지"가 중요하다.

다음 두 위치에서만 `Ctrl+W` 허용:

1. launcher의 `Repos` 탭에서 repo를 선택한 상태
2. picker에서 현재 선택된 session이 repo-backed target인 상태

plain dir session에서는 `Ctrl+W` 비활성.

---

## 영향 범위

| 파일 | 변경 내용 |
|------|----------|
| `main.go` | `--path` 를 initial launcher path 힌트로 전환. path 기반 singleton 제거 |
| `config/config.go` | global config + target-local resolve 로직 추가 |
| `tmux/tmux.go` | path-hash session/window id 도입, hidden window metadata 저장/조회 helper 추가 |
| `ui/picker.go` | directory/worktree catalog 모델 제거, open session list 모델로 재작성, `navigateTo`/handoff 제거, launcher modal 연동 |
| `ui/launcher_dialog.go` (신규) | `Recent / Directories / Repos` launcher 구현 |
| `ui/settings_dialog.go` | `Global / Target Local` scope 모델로 재구성 |
| `ui/worktree_dialog.go` | repo context를 외부에서 주입받도록 변경, launcher repo 탭과 연결 |
| `worktree/scanner.go` | current-directory-context 기준 repo candidate discovery helper 추가 |
| `worktree` 패키지 신규 파일 | launch target resolution / recent target store helper |
| `README.md` | usage / keybinding / 설정 계층 설명 갱신 |

---

## 단계적 구현 순서

## Phase 1. Path-based session identity

1. basename 대신 absolute path hash 기반 session/window id 도입
2. tmux metadata에 `@asm-path` 등 저장
3. provider state / startedAt / provider map 키를 path/id 기반으로 이동
4. provider resume을 cwd-only가 아니라 asm-owned session identity로 붙일 수 있는 저장 모델 설계

## Phase 2. Launcher 도입

1. `ui/launcher_dialog.go` 추가
2. `Recent / Directories / Repos` 모드 구현
3. `Ctrl+N` 을 launcher open 으로 전환

## Phase 3. Picker를 open-session list로 전환

1. 기존 directory scan 기반 main picker 제거
2. `navigateTo` / handoff / `←/→` root restart 제거
3. row 렌더를 session-centric 정보로 재작성

## Phase 4. Settings model 정리

1. target-local config resolution 구현
2. settings scope 를 `Global / Target Local` 로 재구성

---

## 수용 기준

1. 서로 다른 두 directory tree 아래의 session을 하나의 picker에서 동시에 볼 수 있다.
2. 서로 다른 repo에 같은 basename의 worktree가 있어도 충돌 없이 동시에 열 수 있다.
3. picker는 open session만 표시하고, 새 세션 생성은 launcher에서만 수행된다.
4. launcher의 `Directories` 탭에서 asm 재시작 없이 디렉토리를 이동하며 target을 찾을 수 있다.
5. launcher의 `Repos` 탭에서 현재 위치 기준으로 repo를 찾고, main repo 또는 linked worktree를 열 수 있다.
6. 이미 열려 있는 target을 launcher에서 다시 선택하면 중복 생성하지 않고 focus 한다.
7. 다른 asm 인스턴스에서 이미 열린 target을 선택하면, 기존 owner를 닫고 여기서 `continue` 또는 `new session` 으로 reopen 할지 prompt 가 뜬다.
8. cross-instance attach/switch 동작은 발생하지 않는다.
9. 사용자가 `Reopen here and continue` 를 선택하면 기존 owner session은 종료되고 현재 asm이 새 owner가 되며, 가능하면 직전 대화를 이어온다.
10. 사용자가 `Reopen here as a new session` 을 선택하면 기존 owner session은 종료되고 현재 asm이 새 owner가 되며, 이전 대화를 이어오지 않는다.
11. 여러 asm 인스턴스를 동시에 실행해도 tmux session 충돌이 없다.
12. 설정 precedence 가 `global < target-local` 로 일관되게 동작한다.
13. settings UI에서 현재 값이 global / local 중 어디서 왔는지 설명 가능하다.

---

## 확정 사항

1. 별도 관리 루트 개념은 v1에서 도입하지 않는다.
2. `--path` 는 browse root가 아니라 **launcher initial path hint** 다.
3. picker는 **open session list**, launcher는 **launch target catalog** 로 역할을 분리한다.
4. 여러 asm 인스턴스의 동시 실행은 기본 지원한다.
5. multi-root의 내부 key는 basename이 아니라 **absolute path 기반 session id** 로 간다.
6. 설정은 `global / target-local` 2층으로 간다.
7. 전역 config 안에 per-dir / per-repo rule registry를 계속 누적하는 모델은 기본안으로 채택하지 않는다.
8. 같은 absolute path target이 다른 asm 에서 이미 열려 있으면, 기본은 `기존 owner 종료 후 여기서 continue 또는 fresh reopen` 확인 prompt 다.
