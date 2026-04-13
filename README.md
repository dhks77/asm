# ASM (AI Session Manager)

tmux 기반 멀티 AI 세션 매니저. git worktree별로 AI CLI 세션을 관리하는 TUI 프로그램.

## Screenshot

```
 my-project                      │  Claude Code
                                 │
 ⠹ thinking… 12m                 │  ● Changes not staged for commit:
 ● Add user auth API endpoint    │    (use "git add <file>..." to update)
   app-auth-4012                 │
   feature/4012  ↑1              │    modified:   src/auth/handler.go
                                 │    modified:   src/auth/middleware.go
   idle 45m                      │    modified:   src/auth/token.go
   Fix payment retry logic       │
   app-payment-3891              │  Untracked files:
   feature/3891  ✓+2~1           │    (use "git add <file>..." to include)
                                 │
 ✓ done! 8m                      │    src/auth/handler_test.go
   Refactor DB connection pool   │    src/auth/middleware_test.go
   app-refactor-3756             │
   feature/3756  ↑2              │
                                 │  ● staged: commit message for 2 files?
   closed                        │    (Continue to edit)
   Setup CI/CD pipeline          │
   app-cicd-3680                 │
   feature/3680                  │
                                 │
   closed                        │
   Update API documentation      │
   app-docs-3544                 │
   feature/3544  ↑1              │
                                 │
 ^g: focus  ^t: term  ^n: new  ^k: task  ^p: AI  ^s: settings  ^w: worktree
```

## Features

- **멀티 AI 프로바이더** -- Claude, Codex 빌트인 + 플러그인으로 확장
- **tmux 네이티브** -- AI CLI를 100% 원본 그대로 표시
- **Worktree 관리** -- 브랜치별 독립 AI 세션 + 터미널
- **실시간 상태 감지** -- idle/thinking/tool-use/responding 표시
- **완료 알림** -- busy->idle 시 데스크톱 알림 + 마지막 응답 미리보기
- **Tracker 플러그인** -- Dooray 빌트인 + Jira 등 커스텀 트래커 플러그인으로 확장
- **Settings UI** -- Ctrl+S로 플러그인 설정 통합 관리

## Install

```bash
go install github.com/nhn/asm@latest
```

### Requirements

- Go 1.24+
- tmux 3.x+

## Usage

```bash
asm --path ~/worktrees
```

### Keybindings

| Key | Action |
|-----|--------|
| `Enter` | 세션 열기 (기본 AI) |
| `Ctrl+G` | picker <-> 세션 전환 |
| `Ctrl+T` | 터미널 <-> AI 토글 |
| `Ctrl+N` | 새 세션 (기존 kill) |
| `Ctrl+K` | Task URL 브라우저 열기 |
| `Ctrl+P` | AI 프로바이더 선택 |
| `Ctrl+S` | 설정 |
| `Ctrl+W` | Worktree 생성 |
| `Ctrl+D` | 디렉토리 삭제 |
| `Ctrl+X` | 일괄 선택 |
| `Ctrl+Q` | 종료 |
| `o` | Task URL 브라우저 열기 (picker) |

## Plugin System

### AI Provider

`~/.config/asm/plugins/<name>` 에 실행파일 배치.

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

**커스텀 플러그인**: `~/.config/asm/trackers/<name>` 에 실행파일 배치.

```bash
# 브랜치명 -> 이슈 이름 + URL
<tracker> resolve <branch-name>
# → {"name":"Fix login button alignment", "url":"https://..."}

# 설정 (선택, AI provider와 동일)
<tracker> config-fields / config-get / config-set
```

## Config

`~/.config/asm/config.toml`

```toml
default_path = ""
git_refresh_interval = 5
desktop_notifications = true
default_provider = "claude"
default_tracker = "dooray"

[providers.claude]
command = "claude"

[providers.codex]
command = "codex"

[trackers.dooray]
token = ""
project_id = ""
api_base_url = "https://api.dooray.com"
web_url = "https://nhnent.dooray.com"
```

## Architecture

```
asm/
├── main.go              # Entry point, registry/tracker setup
├── provider/            # AI provider interface + builtins + plugin
├── tracker/             # Issue tracker interface + builtins (Dooray) + plugin
├── plugincfg/           # Plugin config protocol (fields/get/set)
├── config/              # TOML config loader
├── tmux/                # tmux session/pane/window management
├── ui/                  # Bubble Tea TUI (picker, dialogs, settings)
├── worktree/            # Git worktree utilities
├── notification/        # Desktop notifications (macOS/Linux/Windows)
└── internal/            # TTL cache
```
