# CSM (Claude Session Manager) - Spec

## 현재 아키텍처

### 개요
git worktree 기반으로 여러 브랜치를 동시에 작업할 때, 각 worktree별 Claude 세션을 관리하는 TUI 프로그램.
tmux를 터미널 멀티플렉서로 사용하여 claude CLI를 네이티브 100% 동일하게 표시한다.

### tmux 세션 구조
```
tmux session "csm"
├── main window (2 panes)
│   ├── Pane 0 (left ~30%): picker UI (Bubble Tea)
│   └── Pane 1 (right ~70%): 현재 선택된 claude 세션 또는 placeholder(cat)
├── wt-dcm-feature-3038 (hidden window): claude 실행 중
├── wt-dcm-hotfix-4049 (hidden window): claude 실행 중
└── ...
```

worktree를 선택하면 해당 hidden window의 pane을 `tmux swap-pane`으로 main.1에 표시.
다른 worktree로 전환 시 현재 pane을 원래 window로 swap-back 후 새 worktree를 swap-in.

### 실행 모드
- `csm [--path <dir>]` — 오케스트레이터: tmux 세션 생성, 좌/우 분할, picker 실행 후 attach
- `csm --picker --path <dir>` — 피커: tmux 좌측 pane 안에서 실행되는 Bubble Tea TUI

### 패널 전환
| 어디서 | 키 | 어디로 |
|--------|-----|--------|
| picker (좌측) | `Tab` or `Enter` | claude 세션 (우측) |
| claude 세션 (우측) | `Tab` 두번 (400ms 이내) | picker (좌측) |

tmux key table 활용: 첫 Tab은 claude에 정상 전달 + `csm-tab` 테이블 진입 → 두번째 Tab → picker 포커스.

### 세션 종료 감지
- claude 명령 뒤에 `; tmux wait-for -S csm-exit-<name>` 추가
- picker에서 `tmux wait-for csm-exit-<name>` 블로킹 대기 (tea.Cmd)
- claude 종료 → 시그널 발화 → 즉시 `sessionExitedMsg` → 정리 + 좌측 포커스 복귀
- 폴링 없음, 이벤트 기반

### 키바인딩 (picker)
| 키 | 동작 |
|----|------|
| `j/k` or `↑/↓` | 목록 탐색 |
| `Enter` | 세션 열기 (기존 있으면 전환, 없으면 새로 시작) |
| `Ctrl+n` | 새 세션 (기존 세션 kill 후 재생성) |
| `Ctrl+t` | 터미널 ↔ Claude 토글 |
| `Ctrl+g` | 우측 세션 포커스 또는 세션 시작 |
| `Ctrl+s` | 설정 열기 |
| `Ctrl+w` | 새 worktree 생성 |
| `Ctrl+d` | 디렉토리 삭제 |
| `Ctrl+x` | 일괄 선택 토글 |
| `Ctrl+q` | 종료 (활성 세션 있으면 확인 다이얼로그) |
| `k` | (선택 모드) 선택된 세션 일괄 종료 |
| `x` | (선택 모드) 선택된 worktree 일괄 삭제 |
| `Esc` | 선택 해제 → 검색 해제 |

### 좌측 패널 표시 정보
각 worktree 항목:
- 일괄 선택 체크박스: `◆` 선택됨 / `◇` 미선택 (선택 모드일 때만 표시)
- 세션 상태 아이콘: `●` 현재 표시 중(green) / `●` 활성 세션 / `○` 세션 없음
- 폴더명 (예: `nc-dms-backend-3680`)
- Dooray task 이름 (선택적, 설정 시)
- Claude 상태 + 경과 시간 (예: `thinking… 12m`, `✓ done! 45m`, `idle 1h30m`)
- Git 상태 (브랜치, ahead/behind, staged/unstaged/untracked)

### 완료 알림
Claude 세션이 busy(thinking/tool-use/responding) → idle로 전환되면:
- picker에서 해당 항목에 `✓ done!` 깜빡임 표시 (3초)
- 데스크톱 알림 (macOS: osascript, Linux: notify-send, Windows: PowerShell toast)
- `desktop_notifications = false`로 비활성화 가능

### 파일 구조
```
csm/
├── main.go                  # 엔트리포인트 (오케스트레이터 / 피커 모드)
├── config/config.go         # ~/.config/csm/config.toml 로드
├── tmux/tmux.go             # tmux 세션/패널/윈도우 관리, 키바인딩 설정
├── ui/
│   ├── picker.go            # 메인 Bubble Tea 모델 (worktree 리스트 + 세션 관리)
│   ├── resume_dialog.go     # 과거 세션 선택 팝업
│   ├── confirm_dialog.go    # 종료 확인 다이얼로그
│   ├── status_bar.go        # 하단 키 힌트
│   └── styles.go            # Lipgloss 스타일 정의
├── session/resume.go        # ~/.claude/sessions/ JSON 파일 스캔
├── worktree/
│   ├── scanner.go           # 하위 디렉토리 스캔 (.git 감지)
│   └── git.go               # git status 조회 (브랜치, ahead/behind, 변경 수)
├── integration/dooray.go    # Dooray API 클라이언트 (선택적)
├── notification/notify.go   # 크로스 플랫폼 데스크톱 알림 (macOS/Linux/Windows)
└── internal/cache.go        # 파일 기반 캐시 (TTL 지원)
```

### 설정 파일
`~/.config/csm/config.toml`:
```toml
default_path = ""                    # --path 미지정 시 기본 경로
task_id_pattern = '(\d{4,})'         # 브랜치명에서 task ID 추출 정규식
git_refresh_interval = 5             # git 상태 갱신 주기 (초)
claude_path = ""                     # claude 바이너리 경로 (기본: PATH에서 탐색)
claude_args = []                     # claude 실행 시 추가 플래그
desktop_notifications = true         # busy→idle 전환 시 데스크톱 알림 (기본: true)

[dooray]
enabled = false
token = ""
api_url = ""
```

### 의존성
- Go 1.22+
- tmux 3.x+
- `github.com/charmbracelet/bubbletea` — TUI
- `github.com/charmbracelet/lipgloss` — 스타일링
- `github.com/BurntSushi/toml` — 설정 파싱

---

## 구현 완료된 기능

### Claude 세션 상태 표시 (구현됨)
- tmux pane title + content 파싱으로 idle/thinking/tool-use/responding 실시간 감지
- 1초 주기 폴링 (`claudeStateTickMsg`)
- `claude/state.go`에서 상태 감지 로직 관리

### 세션 경과 시간 표시 (구현됨)
- 각 세션 시작 시각 추적, picker에 `12m`, `1h30m` 배지 표시
- CSM 재시작 시 관찰 시점부터 측정

### 완료 알림 (구현됨)
- busy→idle 전환 감지 시 picker에 `✓ done!` 3초 깜빡임
- 크로스 플랫폼 데스크톱 알림 (macOS/Linux/Windows)
- `desktop_notifications` 설정으로 제어

### 일괄 작업 (구현됨)
- `Ctrl+X`로 다중 선택 토글
- 선택 모드에서 `k`(일괄 종료), `x`(일괄 삭제) 지원
- 확인 다이얼로그 (dirty worktree 경고 포함)
