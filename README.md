# ASM (AI Session Manager)

tmux 기반 멀티 AI 세션 매니저. Claude Code, Codex 같은 AI CLI 세션과
보조 터미널을 하나의 TUI 안에서 관리한다.

- picker는 **지금 열려 있는 세션 목록**을 보여준다
- 새 target은 `Ctrl+N` launcher에서 연다
- asm session 은 `session id` 로 구분된다

## At A Glance

```text
 billing-api                    │  Codex
                                │
 billing                        │  > investigate flaky checkout test
  ● Fix retry bug              │
    billing-api-4012           │  ...
    feature/4012               │
    [a] thinking… 8m           │
                                │
  ● main                       │
    main                       │
    [a+t] idle 32m             │
                                │
  ● scratch-db                 │
    [t] 5m                     │
                                │
 ^g: focus  ^t: term  ^n: launch  ^]: next  ^e: IDE  ^s: settings  ^w: worktree
```

- `[a]` = AI session
- `[t]` = terminal session
- `[a+t]` = 둘 다 열려 있음

## Features

- **Session ID Based** -- asm tmux session / restore snapshot / 재접속 기준은 경로가 아니라 `session id` 다.
- **Picker = Open Session List** -- 좌측 picker는 현재 asm 인스턴스에서 이미 열린 target만 보여준다.
- **Launcher = Open New Targets** -- 새 target은 `Ctrl+N` launcher에서 연다. launcher는 항상 user home에서 시작하고, top-level 탭 순서는 `Favorites`, `Directories`, `Recent` 이다.
- **Repo Drill-down** -- favorite repo 또는 repo directory를 고르면 main repo / linked worktree 목록으로 바로 들어간다.
- **멀티 인스턴스 안전성** -- 서로 다른 `session id` 로 여러 asm 세션을 동시에 띄울 수 있다.
- **AI + Terminal 듀얼 세션** -- target마다 AI 세션과 보조 terminal을 따로 띄우고 `Ctrl+T` 로 전환할 수 있다.
- **Provider Resume** -- built-in Claude/Codex provider는 target cwd 기준으로 이전 대화를 감지해 가능한 경우 자동 resume 한다.
- **실시간 상태 감지** -- `idle`, `busy`, `thinking`, `tool_use`, `responding` 상태를 추적하고, busy -> idle 전환 시 완료 flash와 데스크톱 알림을 보낸다.
- **Tracker 연동** -- branch 이름에서 task title / URL 을 해석해 picker, launcher, worktree dialog 검색에 반영한다.
- **IDE 실행** -- built-in IntelliJ / VS Code launcher를 제공하고, config로 Cursor / Zed 같은 custom IDE를 추가할 수 있다.
- **Worktree 생성** -- repo-backed target에서 branch 선택 또는 새 branch 생성 후 worktree를 만들고, repo별 템플릿 파일을 자동 복사한다.
- **Safer Delete** -- `Ctrl+D` 는 일반 디렉토리와 main repo를 휴지통으로 보내고, linked git worktree만 `git worktree remove` 로 정리한다.
- **Settings Scope** -- global user config와 project-local config를 함께 다루며, repo/worktree는 main repo 단위로 설정을 공유한다.
- **Session Restore** -- asm 종료 시 열린 AI / terminal snapshot을 저장하고, 다음 실행 때 복원 여부를 묻는다.
- **Repo Grouping + Colors** -- picker는 repo label 기준으로 세션을 묶고 repo별 accent color를 표시한다.

## Install

```bash
git clone https://github.com/dhks77/asm.git
cd asm
go install .
```

바이너리는 보통 `~/go/bin/asm` 에 설치된다.

### Requirements

- Go 1.24+
- tmux 3.x+
- git
- 실행할 provider CLI (`claude`, `codex`, 또는 provider plugin이 선언한 command)

## Usage

```bash
# 마지막으로 쓰던 session id 를 이어갈지 묻는다 (기본값 Y)
asm

# 특정 session id 를 바로 이어가거나 새로 연다
asm -s billing
asm -session tc-dcm

# 현재 살아 있는 asm session id 목록
asm --list
```

## Workflow

### 1. Start asm

`asm` 은 tmux session을 만들고 좌측 picker + 우측 working panel을 띄운다.

- 좌측 picker: 현재 열린 세션 목록만 표시
- 우측 working panel: 실제 AI CLI 또는 terminal

- plain `asm`: 마지막 `session id` 를 이어갈지 먼저 묻는다. 기본값은 `Y` 다.
- `asm -s <id>` / `asm -session <id>`: 확인 없이 해당 `session id` 를 바로 연다.
- 같은 `session id` 의 asm tmux session 이 이미 살아 있으면 새로 만들지 않고 바로 attach / switch 한다.
- 저장된 snapshot 이 있으면 마지막으로 열려 있던 AI / terminal 세션을 복원한다.

### 2. Open Targets With Launcher

`Ctrl+N` 으로 launcher를 연다.

launcher 탭:

- `Favorites` -- 즐겨찾은 dir / repo
- `Directories` -- home directory 기준으로 direct child를 lazy browse
- `Recent` -- 최근에 연 target

repo favorite 또는 repo directory를 선택하면 main repo / linked worktree 목록으로 drill-down 한다.

동일 target이 이미 현재 asm 인스턴스 안에서 열려 있으면 새로 만들지 않고 기존 세션으로 focus 한다.

### 3. Work In AI / Terminal

- `Enter` -- 선택한 target의 AI 세션 열기 또는 focus
- `Ctrl+T` -- AI/terminal 전환 또는 terminal 세션 열기
- `Ctrl+G` -- picker <-> working pane focus 토글
- `Ctrl+]` -- 활성 세션 순환

picker 검색은 **열려 있는 세션들**에만 적용되며 다음을 매칭한다.

- 폴더명
- repo명
- task명
- branch명
- path

## Keybindings

### Main Picker

| Key | Action |
|-----|--------|
| `Enter` | 선택한 target의 AI 세션 열기 / 기존 세션 focus |
| `Ctrl+N` | launcher 열기 |
| `Ctrl+G` | picker <-> working pane focus 토글 |
| `Ctrl+T` | terminal 열기 / focus / AI와 전환 |
| `Ctrl+]` | 다음 활성 세션으로 순환 |
| `Ctrl+L` | picker panel 표시/숨김 |
| `Ctrl+P` | AI provider 선택 (`2개 이상`일 때) |
| `Ctrl+E` | target을 IDE에서 열기 |
| `Ctrl+W` | worktree 생성 dialog 열기 (repo context에서만) |
| `Ctrl+S` | settings 열기 |
| `Ctrl+K` | 현재 선택 target의 AI/terminal 세션 종료 |
| `Ctrl+D` | 현재 선택 target 삭제 (`dir/repo → 휴지통`, `linked worktree → git remove`) |
| `Ctrl+X` | 다중 선택 토글 |
| `Ctrl+O` | tracker가 제공한 task URL 열기 |
| `Ctrl+Q` | asm 종료 |
| `Esc` | 선택 해제 / 검색 해제 |
| `↑` / `↓` | 목록 이동 |
| `Backspace` | 검색어 한 글자 삭제 |
| _(타이핑)_ | open session 검색 |

### Batch Selection

여러 target을 `Ctrl+X` 로 선택한 뒤:

- `Ctrl+K` -- 선택된 세션 일괄 종료
- `Ctrl+D` -- 선택된 target 일괄 삭제 (`dir/repo → 휴지통`, `linked worktree → git remove`)
- `Esc` -- 선택 해제

### Launcher

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | 탭 전환 |
| `↑` / `↓` | 항목 이동 |
| `←` / `→` | 부모 이동 / 진입 / repo drill-down |
| `Enter` | launch 또는 repo/worktree 선택 |
| `Ctrl+F` | favorite 토글 |
| `Backspace` | filter 한 글자 삭제 |
| _(타이핑)_ | 현재 탭 filter |
| `Esc` / `Ctrl+C` | launcher 닫기 |

### Settings

`Ctrl+S` 로 settings를 연다.

- `Scope` 는 `global` / `local` 로 전환된다.
- `global` 에서는 provider / tracker / default IDE / picker width / plugin config / IDE launcher 목록을 편집한다.
- `local` 에서는 repo color, worktree template 충돌 정책, worktree base path를 편집한다. repo-backed target에서만 보인다.
- `Ctrl+R` 은 현재 local override를 지우고 global 값을 다시 상속한다.
- `Ctrl+X` 는 현재 IDE 항목을 삭제하거나 built-in IDE override를 reset 한다.
- `Enter` 또는 `Ctrl+S` 로 저장한다.

## Plugin System

### AI Provider

`~/.asm/plugins/<name>` 에 실행파일을 두면 provider로 로드된다.

```bash
# 시작 시 1회 호출
<plugin> info
# → {
#     "name":"aider",
#     "display_name":"Aider",
#     "command":"aider",
#     "args":[],
#     "resume_args":["--resume"],
#     "needs_content":true
#   }

# 상태 감지
echo '{"title":"...","content":"..."}' | <plugin> detect-state
# → {"state":"thinking"}

# 설정 필드 (선택)
<plugin> config-fields
# → [{"key":"api_key","label":"API Key","secret":true}]

# 설정 조회/저장 (선택)
<plugin> config-get
echo JSON | <plugin> config-set
```

state 값:

- `idle`
- `busy`
- `thinking`
- `tool_use`
- `responding`

빌트인 provider:

- `claude`
- `codex`

### Tracker

빌트인 tracker는 `dooray` 이고, 커스텀 tracker도 `~/.asm/trackers/<name>` 에 둘 수 있다.

```bash
# branch -> task name + URL
<tracker> resolve <branch-name>
# → {"name":"Fix login button alignment", "url":"https://..."}

# 설정 (선택)
<tracker> config-fields
<tracker> config-get
echo JSON | <tracker> config-set
```

## IDE Launching

`Ctrl+E` 로 현재 target을 IDE에서 연다.

- IDE가 1개만 있으면 바로 실행
- `default_ide` 가 설정돼 있으면 selector를 건너뜀
- 아니면 selector dialog를 띄움

빌트인:

- `intellij`
- `vscode`

예시:

```toml
default_ide = "cursor"

[ides.intellij]
command = "open"
args = ["-a", "IntelliJ IDEA Ultimate"]

[ides.cursor]
command = "open"
args = ["-a", "Cursor"]

[ides.zed]
command = "zed"
```

## Worktree Creation

repo-backed target에서 `Ctrl+W` 를 누르면 worktree 생성 dialog가 열린다.

- best-effort로 `git fetch --all` 후 branch 목록을 읽음
- 기존 branch를 골라 새 worktree 생성 가능
- `Ctrl+N` 으로 새 branch 생성 모드로 들어갈 수 있음
- `Tab` 으로 repo choice를 순환할 수 있음
- 생성 성공 후 repo 템플릿 파일을 새 worktree에 자동 복사

새 worktree 위치는 다음 우선순위로 정해진다.

1. 가장 최근 linked worktree의 부모 디렉토리
2. project config의 `worktree_base_path`
3. 기본값 `~/worktrees/{repo}`

repo mode 첫 진입 때 기존 linked worktree 배치를 감지하면 project scope에
`worktree_base_path` 를 자동 시딩한다.

## Worktree Template Copy

새 worktree 를 만들 때 repo별 템플릿 파일을 자동 복사한다.

경로:

```text
{projectRoot}/.asm/templates/{repo}/...
```

예:

- `.env`
- `.env.local`
- `.vscode/settings.json`
- `CLAUDE.md.local`

사용법:

1. `Ctrl+S` → `Worktree` 섹션 → `Open templates directory`
2. `{projectRoot}/.asm/templates/{repo}/` 아래에 원하는 파일 배치
3. `Ctrl+W` 로 worktree 생성

충돌 정책:

- `skip` -- 기존 파일 유지
- `overwrite` -- 템플릿으로 덮어씀

```toml
[worktree_template]
on_conflict = "skip"
```

상세: [`docs/worktree-template-copy.md`](docs/worktree-template-copy.md)

## Session Restore

asm 종료 시 현재 열린 target snapshot을 저장한다.

snapshot 은 `session id` 기준으로 저장된다.

다음 번 `asm` 실행 시:

- 마지막 `session id` 를 이어갈지 묻고
- 허용하면 같은 `session id` 로 다시 들어가고
- 저장된 target 이 있으면 세션을 다시 살린다

`asm -s <id>` / `asm -session <id>` 는 이 확인을 건너뛰고 해당 `session id` 를 바로 연다.

복원되는 정보:

- 어떤 target이 열려 있었는지
- AI / terminal 중 무엇이 열려 있었는지
- front session / focus / zoom 상태

## Config

### Locations

- User config: `~/.asm/config.toml`
- Project config: `<projectRoot>/.asm/config.toml`

project root 해석:

- git repo / linked worktree: **main repo root**
- plain directory: 가장 가까운 상위 `.asm/config.toml` 보유 디렉토리, 없으면 자기 자신

runtime에서는 user config 위에 project config를 merge 한다.
map 계열 필드는 key 기준으로 merge 된다.

### Typical User Config

`~/.asm/config.toml`

```toml
desktop_notifications = true
picker_width = 24

default_provider = "claude"
default_tracker = "dooray"
default_ide = ""                  # 비우면 IDE selector 표시

[providers.codex]
command = "codex"

[trackers.dooray]
token = ""
project_id = ""
api_base_url = "https://api.dooray.com"
web_url = "https://nhnent.dooray.com"
task_pattern = "[0-9]+"

[ides.cursor]
command = "open"
args = ["-a", "Cursor"]

[ides.zed]
command = "zed"
```

### Typical Project Config

`<projectRoot>/.asm/config.toml`

```toml
default_provider = "codex"
default_ide = "cursor"
worktree_base_path = "~/worktrees/{repo}"
repo_color = "sky"                # preset / ansi / hex / rgb(...)

[worktree_template]
on_conflict = "skip"              # "skip" | "overwrite"
```

`repo_color` 는 preset alias, ANSI 0-255, hex, `rgb(r,g,b)` 형식을 지원한다.
예전 user-scope `repo_colors` 값이 있으면 repo metadata를 읽는 시점에 project-scope `repo_color`
로 자동 migration 된다.

## Architecture

```text
asm/
├── main.go              # entry point / tmux orchestrator
├── ui/                  # picker, launcher, dialogs, settings
├── tmux/                # tmux session/window/pane management
├── provider/            # AI provider interface + builtins + plugins
├── tracker/             # tracker interface + builtins + plugins
├── worktree/            # git/worktree helpers + template copy
├── config/              # layered config + project identity
├── favorites/           # launcher favorites store
├── recent/              # recent target store
├── sessionstate/        # session snapshot / restore
├── ide/                 # IDE launchers
├── plugincfg/           # plugin config protocol helpers
└── notification/        # desktop notifications
```
