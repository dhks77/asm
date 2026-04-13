# ASM (AI Session Manager) - Spec

## 아키텍처

### 개요
git worktree 기반으로 여러 브랜치를 동시에 작업할 때, 각 worktree별 AI 세션을 관리하는 TUI 프로그램.
tmux를 터미널 멀티플렉서로 사용하여 AI CLI를 네이티브 100% 동일하게 표시한다.
기본 preset으로 Claude와 Codex를 지원하며, 플러그인으로 임의의 AI CLI 도구를 추가할 수 있다.

### tmux 세션 구조
```
tmux session "asm"
├── main window (2 panes)
│   ├── Pane 0 (left ~30%): picker UI (Bubble Tea)
│   └── Pane 1 (right ~70%): 현재 선택된 AI 세션 또는 placeholder(cat)
├── wt-dcm-feature-3038 (hidden window): AI 실행 중
├── wt-dcm-hotfix-4049 (hidden window): AI 실행 중
└── ...
```

worktree를 선택하면 해당 hidden window의 pane을 `tmux swap-pane`으로 main.1에 표시.
다른 worktree로 전환 시 현재 pane을 원래 window로 swap-back 후 새 worktree를 swap-in.

### 멀티 AI 프로바이더
- **Provider 인터페이스**: `provider/provider.go` — Name, DisplayName, Command, Args, DetectState, NeedsContent, SessionDir, ResumeArgs
- **Preset**: Claude (pane title 기반 감지), Codex (content 기반 감지)
- **Plugin**: `~/.config/asm/plugins/<name>` 외부 실행파일이 Provider 인터페이스를 구현
- **Registry**: 프로바이더 등록/조회/기본값 관리

### 실행 모드
- `asm [--path <dir>]` — 오케스트레이터: tmux 세션 생성, 좌/우 분할, picker 실행 후 attach
- `asm --picker --path <dir>` — 피커: tmux 좌측 pane 안에서 실행되는 Bubble Tea TUI

### 세션 종료 감지
- AI 명령 뒤에 `; tmux wait-for -S asm-exit-<name>` 추가
- picker에서 `tmux wait-for asm-exit-<name>` 블로킹 대기 (tea.Cmd)
- AI 종료 → 시그널 발화 → 즉시 `sessionExitedMsg` → 정리 + 좌측 포커스 복귀
- 폴링 없음, 이벤트 기반

### 키바인딩 (picker)
| 키 | 동작 |
|----|------|
| `j/k` or `↑/↓` | 목록 탐색 |
| `Enter` | 세션 열기 (기존 있으면 전환, 없으면 기본 AI로 시작) |
| `Ctrl+n` | 새 세션 (기존 세션 kill 후 같은 AI로 재생성) |
| `Ctrl+p` | AI 프로바이더 선택 다이얼로그 |
| `Ctrl+t` | 터미널 ↔ AI 토글 |
| `Ctrl+g` | 우측 세션 포커스 또는 세션 시작 |
| `Ctrl+]` | 활성 세션 순환 (단방향, 마지막 → 처음) |
| `Ctrl+s` | 설정 열기 |
| `Ctrl+w` | 새 worktree 생성 |
| `Ctrl+d` | 디렉토리 삭제 |
| `Ctrl+x` | 일괄 선택 토글 |
| `Ctrl+q` | 종료 (활성 세션 있으면 확인 다이얼로그) |
| `k` | (선택 모드) 선택된 세션 일괄 종료 |
| `x` | (선택 모드) 선택된 worktree 일괄 삭제 |
| `Esc` | 선택 해제 → 검색 해제 |

### 목록 정렬 (picking panel)
- 활성 AI 세션이 열려있는 worktree가 **최상단**
- 나머지는 아래 — 각 그룹 내부 순서는 `worktree.Scan()` 결과 유지

### 풀스크린 / 상단 요약 바 (working panel)
- `auto_zoom=true` (기본) 이면 세션 열 때 working pane을 tmux zoom
- 상단 tmux status line에 모든 활성 세션 요약: `▶ 이름 상태 │ ● 이름 상태 │ …`
  - 이름: tracker task name 우선, 없으면 폴더명
  - 상태: `thinking` / `responding` / `tool-use` / `idle` / `done`
  - `▶` = 현재 표시 중, `●` = 그 외 활성

### 좌측 패널 표시 정보
각 worktree 항목:
- 일괄 선택 체크박스: `◆` 선택됨 / `◇` 미선택 (선택 모드일 때만 표시)
- 세션 상태 아이콘: `●` 현재 표시 중(green) / `●` 활성 세션 / `○` 세션 없음
- 폴더명 (예: `nc-dms-backend-3680`)
- Dooray task 이름 (선택적, 설정 시)
- AI 상태 + 경과 시간 (예: `Claude thinking… 12m`, `✓ done! 45m`, `idle 1h30m`)
- Git 상태 (브랜치, ahead/behind, staged/unstaged/untracked)
- 프로바이더가 2개 이상 활성일 때 AI 이름 표시

### 완료 알림
AI 세션이 busy(thinking/tool-use/responding) → idle로 전환되면:
- picker에서 해당 항목에 `✓ done!` 깜빡임 표시 (3초)
- 데스크톱 알림 (macOS: osascript, Linux: notify-send, Windows: PowerShell toast)
- `desktop_notifications = false`로 비활성화 가능

### 파일 구조
```
csm/
├── main.go                  # 엔트리포인트 (오케스트레이터 / 피커 모드)
├── provider/
│   ├── provider.go          # Provider 인터페이스
│   ├── state.go             # State enum (idle/busy/thinking/tool-use/responding)
│   ├── claude.go            # Claude preset (pane title + content 감지)
│   ├── codex.go             # Codex preset (content 기반 감지)
│   ├── plugin.go            # 외부 실행파일 플러그인 provider
│   └── registry.go          # 프로바이더 레지스트리
├── config/
│   ├── config.go            # ~/.config/asm/config.toml 로드 (csm fallback)
│   └── settings.go          # .asm/settings.json 프로젝트 설정
├── tmux/tmux.go             # tmux 세션/패널/윈도우 관리, 키바인딩 설정
├── ui/
│   ├── picker.go            # 메인 Bubble Tea 모델 (worktree 리스트 + 세션 관리)
│   ├── provider_dialog.go   # AI 프로바이더 선택 다이얼로그
│   ├── resume_dialog.go     # 과거 세션 선택 팝업
│   ├── status_bar.go        # 하단 키 힌트
│   └── styles.go            # Lipgloss 스타일 정의
├── session/resume.go        # 세션 디렉토리 JSON 파일 스캔
├── worktree/
│   ├── scanner.go           # 하위 디렉토리 스캔 (.git 감지)
│   ├── git.go               # git status 조회 (브랜치, ahead/behind, 변경 수)
│   └── branch.go            # 브랜치 관리
├── integration/dooray.go    # Dooray API 클라이언트 (선택적)
└── notification/notify.go   # 크로스 플랫폼 데스크톱 알림 (macOS/Linux/Windows)
```

### 설정 파일
`~/.config/asm/config.toml` (fallback: `~/.config/csm/config.toml`):
```toml
default_path = ""
git_refresh_interval = 5
desktop_notifications = true
auto_zoom = true                      # working pane 자동 풀스크린
default_provider = "claude"           # 기본 AI 프로바이더

# Preset 오버라이드 (선택사항 — command/args 커스터마이즈)
[providers.claude]
command = "claude"
args = []

[providers.codex]
command = "codex"
args = []
```

### 플러그인 시스템
커스텀 AI 프로바이더는 `~/.config/asm/plugins/` 디렉토리에 실행파일로 추가.
각 플러그인은 두 가지 서브커맨드를 구현:

**`<plugin> info`** — 메타정보 (시작 시 1회 호출, 캐싱):
```json
{
  "name": "aider",
  "display_name": "Aider",
  "command": "aider",
  "args": ["--model", "sonnet"],
  "needs_content": true,
  "session_dir": "",
  "resume_args_flag": ""
}
```

**`<plugin> detect-state`** — 상태 감지 (stdin으로 JSON 수신, 1초마다 호출):
```bash
echo '{"title":"...","content":"..."}' | <plugin> detect-state
→ {"state": "idle"}
```

state 값: `unknown`, `idle`, `busy`, `thinking`, `tool_use`, `responding`

### 하위 호환
- `~/.config/asm/` 없으면 `~/.config/csm/` fallback
- `.asm/settings.json` 없으면 `.csm/settings.json` fallback
- 기존 `claude_path`/`claude_args` 필드 → `providers.claude`로 자동 마이그레이션

### 의존성
- Go 1.24+
- tmux 3.x+
- `github.com/charmbracelet/bubbletea` — TUI
- `github.com/charmbracelet/lipgloss` — 스타일링
- `github.com/BurntSushi/toml` — 설정 파싱
