# ASM (AI Session Manager)

tmux 기반 멀티 AI 세션 매니저. git worktree별로 AI CLI 세션을 관리하는 TUI 프로그램.

## Screenshot

```
 my-project                      │  Claude Code
                                 │
 ⠹ thinking… 12m                 │  ● Changes not staged for commit:
 ● Add user auth API endpoint    │    (use "git add <file>..." to update)
   app-auth-4012                 │
   feature/4012                  │    modified:   src/auth/handler.go
                                 │    modified:   src/auth/middleware.go
   idle 45m                      │    modified:   src/auth/token.go
   Fix payment retry logic       │
   app-payment-3891              │  Untracked files:
   feature/3891                  │    (use "git add <file>..." to include)
                                 │
 ✓ done! 8m                      │    src/auth/handler_test.go
   Refactor DB connection pool   │    src/auth/middleware_test.go
   app-refactor-3756             │
   feature/3756                  │
                                 │  ● staged: commit message for 2 files?
   closed                        │    (Continue to edit)
   Setup CI/CD pipeline          │
   app-cicd-3680                 │
   feature/3680                  │
                                 │
   closed                        │
   Update API documentation      │
   app-docs-3544                 │
   feature/3544                  │
                                 │
 ^g: focus  ^t: term  ^n: new  ^k: task  ^e: IDE  ^p: AI  ^s: settings  ^w: worktree
```

## Features

- **멀티 AI 프로바이더** -- Claude, Codex 빌트인 + 플러그인으로 확장
- **tmux 네이티브** -- AI CLI를 100% 원본 그대로 표시
- **Worktree 관리** -- 브랜치별 독립 AI 세션 + 터미널
- **Dir / Repo 두 가지 모드** -- `--path`가 worktree 모음 디렉토리면 그 하위를 스캔, git repo 본체면 `git worktree list` 기반으로 전개
- **←/→ 네비게이션** -- 피커 안에서 상위/하위 디렉토리로 이동(프로세스 재실행); 활성 AI 세션이나 목적지에 이미 asm 세션이 있으면 확인 다이얼로그
- **실시간 상태 감지** -- idle/thinking/tool-use/responding 표시
- **완료 알림** -- busy->idle 시 데스크톱 알림 + 마지막 응답 미리보기
- **Tracker 플러그인** -- Dooray 빌트인 + Jira 등 커스텀 트래커 플러그인으로 확장
- **IDE 실행** -- Ctrl+E로 워크트리를 IntelliJ/VSCode 에서 바로 열기 (config로 Cursor/Zed 등 자유 추가)
- **Worktree 템플릿 자동 복사** -- `.env` / `.vscode` / `CLAUDE.md.local` 등 git 에 안 올리는 파일을 repo별 템플릿으로 관리 → 새 worktree 만들면 자동 복사
- **worktree_base_path 자동 시딩** -- 첫 진입 때 기존 linked worktree 배치를 감지해 프로젝트 설정에 기록
- **Settings UI** -- Ctrl+S로 플러그인 설정 통합 관리

## Install

```bash
git clone https://github.com/dhks77/asm.git
cd asm
go install .
```

바이너리는 `$GOPATH/bin/asm` (보통 `~/go/bin/asm`) 에 설치됨.

### Requirements

- Go 1.24+
- tmux 3.x+

## Usage

```bash
# Dir mode — 여러 worktree/repo를 묶어놓은 부모 디렉토리
asm --path ~/worktrees

# Repo mode — git repo 본체 (worktree 있으면 git worktree list로 발견)
asm --path ~/projects/myrepo
```

`--path` 생략 시 `default_path` → 현재 작업 디렉토리 순으로 폴백.

### Keybindings

| Key | Action |
|-----|--------|
| `Enter` | 세션 열기 (기본 AI) |
| `←`/`→` | 상위 / 하위 디렉토리로 네비게이트 (asm 재실행) |
| `Ctrl+G` | picker <-> 세션 전환 |
| `Ctrl+T` | 터미널 <-> AI 토글 |
| `Ctrl+N` | 새 세션 (기존 kill) |
| `Ctrl+]` | 활성 세션 순환 |
| `Ctrl+K` / `o` | Task URL 브라우저 열기 |
| `Ctrl+E` | 워크트리를 IDE 로 열기 |
| `Ctrl+P` | AI 프로바이더 선택 |
| `Ctrl+S` | 설정 |
| `Ctrl+W` | Worktree 생성 (repo 모드 전용, + 템플릿 자동 복사) |
| `Ctrl+D` | 디렉토리 삭제 |
| `Ctrl+X` | 일괄 선택 토글 |
| `Ctrl+Q` | 종료 |
| `Esc` | 선택 해제 / 검색 해제 |
| `↑`/`↓` | 목록 탐색 |
| _(타이핑)_ | 폴더명 / task명 / 브랜치 검색 |

**일괄 선택 모드** (`Ctrl+X` 로 진입):

| Key | Action |
|-----|--------|
| `k` | 선택된 세션 일괄 종료 |
| `x` | 선택된 worktree 일괄 삭제 |
| `Esc` | 선택 해제 |

## Plugin System

### AI Provider

`~/.asm/plugins/<name>` 에 실행파일 배치.

```bash
# 메타정보 (시작 시 1회)
<plugin> info
# → {"name":"aider","display_name":"Aider","command":"aider","args":[],"needs_content":true}

# 상태 감지 (매초)
echo '{"title":"...","content":"..."}' | <plugin> detect-state
# → {"state":"thinking"}

# 설정 필드 (선택)
<plugin> config-fields
# → [{"key":"api_key","label":"API Key","secret":true}]

# 설정 조회/저장 (선택)
<plugin> config-get    # → {"api_key":"..."}
echo JSON | <plugin> config-set
```

state: `unknown`, `idle`, `busy`, `thinking`, `tool_use`, `responding`

### Tracker

**빌트인**: `dooray`. Settings(Ctrl+S)에서 토큰/프로젝트 ID 등 구성.

**커스텀 플러그인**: `~/.asm/trackers/<name>` 에 실행파일 배치.

```bash
# 브랜치명 -> 이슈 이름 + URL
<tracker> resolve <branch-name>
# → {"name":"Fix login button alignment", "url":"https://..."}

# 설정 (선택, AI provider와 동일)
<tracker> config-fields / config-get / config-set
```

## IDE 실행

`Ctrl+E` 로 커서 워크트리를 선택한 IDE 에서 연다. 여러 IDE 가 설정되어
있으면 selector 가 뜨고, `Default IDE` 가 지정돼 있거나 하나만 있으면
바로 실행. 실행은 `{command} {args...} {worktree_path}` 형태로 detached.

**빌트인**: `intellij`, `vscode`.
- macOS: `open -a "IntelliJ IDEA"` / `open -a "Visual Studio Code"` — 앱만
  설치돼 있으면 동작 (CLI 별도 등록 불필요).
- Linux/Windows: `idea` / `code` CLI 사용 — 각각 PATH 에 있어야 함.

나머지 IDE 는 **Settings(`Ctrl+S`) 의 IDEs 섹션** 또는 **config 파일**
에서 자유롭게 추가/오버라이드 한다.

- Settings: `+ Add IDE` 에서 Enter → 새 항목의 Name/Command/Args 편집
  후 Enter 로 저장. 기존 항목의 `Ctrl+X` 는 제거(커스텀) 또는 디폴트
  복원(빌트인).
- Args 는 `-a "Visual Studio Code"` 처럼 공백이 들어간 토큰을 큰따옴표로
  묶어서 입력.

```toml
# 기본 IDE 고정 (selector 건너뜀). 비워두면 매번 selector.
default_ide = "intellij"

# macOS — IntelliJ Community Edition 을 쓰는 경우
[ides.intellij]
command = "open"
args = ["-a", "IntelliJ IDEA CE"]

# 신규 IDE 추가
[ides.cursor]
command = "open"
args = ["-a", "Cursor"]

[ides.webstorm]
command = "open"
args = ["-a", "WebStorm"]

[ides.zed]
command = "zed"
```

저장 후 `asm` 재시작하면 selector 와 Settings 의 `Default IDE` 목록에 반영.

## Worktree 템플릿 자동 복사

새 worktree 를 만들 때마다 `.env` / `.vscode/settings.json` / `CLAUDE.md.local`
같은 git 에 안 올리는 로컬 파일을 손으로 복사하던 수고를 없애준다.

### 원리
`{projectRoot}/.asm/templates/{repo}/` 하위의 **파일들을 동일 상대 경로로
복사**. 디렉토리는 복사 단위가 아니라 경로 표현 수단 — 대상에 같은 디렉토리가
이미 있으면 그 안의 기존 파일은 건드리지 않고 템플릿에 있는 파일만 채워 넣는다.

- `{repo}` 는 `git remote get-url origin` 의 마지막 세그먼트 (없으면 main repo 폴더명)
- 심볼릭 링크 / 빈 디렉토리 / `.git/` 은 복사 대상 제외

### 사용법
1. `Ctrl+S` → **Worktree** 섹션 → `Open templates directory` Enter
   - `{projectRoot}/.asm/templates/` 와 발견된 모든 repo 의 하위 폴더가
     자동 생성되고 OS 파일 탐색기로 열린다
2. 원하는 repo 폴더에 `.env`, `.vscode/settings.json` 등을 그대로 드롭
3. `Ctrl+W` 로 새 worktree 생성 → 그 파일들이 자동 복사됨

### 충돌 정책
대상 worktree 에 이미 동일 파일이 있을 때 동작 — Settings 에서 토글:

- `skip` (기본): 기존 파일 유지
- `overwrite`: 템플릿 내용으로 덮어씀 (디렉토리 타입이면 안전장치로 skip + 경고)

```toml
[worktree_template]
on_conflict = "skip"   # "skip" | "overwrite"
```

상세: [`docs/worktree-template-copy.md`](docs/worktree-template-copy.md)

## Config

**User**: `~/.asm/config.toml` — 모든 프로젝트에서 공통 사용

**Project**: `<rootPath>/.asm/config.toml` — 해당 프로젝트에서만 override (선택)

runtime에서 project가 user를 override (map 필드는 per-key merge). Settings(Ctrl+S)에서 scope 선택해서 저장.

```toml
default_path = ""
desktop_notifications = true
auto_zoom = true                  # working pane 자동 풀스크린
picker_width = 22                 # picker pane 폭 (%, 10~50)
default_provider = "claude"
default_tracker = "dooray"
default_ide = ""  # "(none)" — 매번 selector. 또는 "intellij" 등으로 고정

[providers.claude]
command = "claude"

[providers.codex]
command = "codex"

[trackers.dooray]
token = ""
project_id = ""
api_base_url = "https://api.dooray.com"
web_url = "https://nhnent.dooray.com"

# 빌트인 오버라이드 예시 (필요시)
[ides.intellij]
command = "open"
args = ["-a", "IntelliJ IDEA Ultimate"]

[ides.cursor]
command = "open"
args = ["-a", "Cursor"]

# 새 worktree 생성 시 {projectRoot}/.asm/templates/{repo}/ 하위 파일을 자동 복사
[worktree_template]
on_conflict = "skip"   # "skip" (default) | "overwrite"

# Repo 모드에서 Ctrl+W로 새 worktree를 만들 때 쓰이는 부모 경로.
# {repo} → origin URL 끝(없으면 repo 폴더명). 처음 진입할 때 기존 linked
# worktree가 있으면 그 부모 디렉토리로 project scope에 자동 시딩된다.
worktree_base_path = "~/worktrees/{repo}"
```

## Architecture

```
asm/
├── main.go              # Entry point, registry/tracker setup
├── provider/            # AI provider interface + builtins + plugin
├── tracker/             # Issue tracker interface + builtins (Dooray) + plugin
├── ide/                 # IDE launcher (builtins + config overrides)
├── plugincfg/           # Plugin config protocol (fields/get/set)
├── config/              # TOML config loader
├── tmux/                # tmux session/pane/window management
├── ui/                  # Bubble Tea TUI (picker, dialogs, settings)
├── worktree/            # Git worktree utilities
└── notification/        # Desktop notifications (macOS/Linux/Windows)
```
