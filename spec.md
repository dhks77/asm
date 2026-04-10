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
| `n` | 새 세션 (기존 세션 kill 후 재생성) |
| `r` | 과거 세션 resume (`~/.claude/sessions/` 스캔) |
| `d` | 현재 세션 종료 |
| `Tab` | 우측 세션 포커스 이동 |
| `q` | 종료 (활성 세션 있으면 확인 다이얼로그) |

### 좌측 패널 표시 정보
각 worktree 항목:
- 세션 상태 아이콘: `▶` 현재 표시 중 / `●` 활성 세션 / `○` 세션 없음
- 폴더명 (예: `nc-dms-backend-3680`)
- Dooray task 이름 (선택적, 설정 시)
- Git 상태 (브랜치, ahead/behind, staged/unstaged/untracked)

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

## 다음 작업: Claude 세션 상태 표시

### 목표
좌측 picker 리스트에서 각 worktree의 claude 세션이 현재 **어떤 상태인지** 실시간으로 표시한다.

### 표시할 상태
| 상태 | 의미 | 표시 예시 |
|------|------|-----------|
| **idle** | 사용자 입력 대기 중 (프롬프트 `>` 상태) | `⏳ idle` |
| **thinking** | claude가 생각/처리 중 | `🔄 thinking...` |
| **tool-use** | 도구 실행 중 (파일 읽기, 코드 실행 등) | `🔧 running tool` |
| **streaming** | 응답 출력 중 | `💬 responding` |
| **exited** | 세션 종료됨 | `⏹ exited` |

### 구현 방향

#### 방법 1: claude CLI의 `--output-format stream-json` 활용
claude CLI를 `--output-format stream-json`으로 실행하면 실시간 JSON 이벤트 스트림을 받을 수 있다.
이를 파싱하여 현재 상태를 판별할 수 있다.
- 장점: 정확한 상태 파악 가능
- 단점: `stream-json`은 `--print` 모드에서만 동작하며, interactive 모드에서는 사용 불가

#### 방법 2: tmux pane 출력 캡처
`tmux capture-pane -t <pane> -p`로 현재 화면 내용을 주기적으로 읽어서 패턴 매칭.
- `>` 프롬프트 → idle
- `Thinking...`, `(thinking)` → thinking
- `$ <command>` 실행 패턴 → tool-use
- 텍스트가 계속 변경 중 → streaming
- 장점: interactive 모드에서도 동작, claude CLI 변경 없음
- 단점: 패턴 매칭이 fragile할 수 있음, 1-2초 폴링 필요

#### 방법 3: claude CLI status line 활용
claude CLI가 터미널 title이나 status line에 상태 정보를 출력하는 경우 이를 캡처.
- `tmux display-message -p '#{pane_title}'`로 pane title 읽기

### 구현 시 고려사항
- 상태는 좌측 picker 리스트의 각 worktree 항목에 표시
- 현재 표시되지 않은 (hidden window에 있는) 세션의 상태도 확인 가능해야 함
- 성능: 너무 잦은 폴링은 피해야 함 (2-3초 간격 적정)
- claude CLI 버전 의존성 최소화

### 관련 파일
- `tmux/tmux.go` — pane 캡처 함수 추가 필요
- `ui/picker.go` — 상태 표시 렌더링 + 상태 폴링 tick 추가
- `ui/styles.go` — 상태별 스타일 추가
