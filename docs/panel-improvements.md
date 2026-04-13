# Panel Improvements — Spec

## 배경
- **Picking panel**: 현재 worktree 목록은 `worktree.Scan()` 순서(디렉토리명) 그대로. 활성 세션이 있는 worktree도 목록 중간에 묻혀 있어 스크롤해서 찾아야 함.
- **Working panel**: tmux 분할뷰(좌 30% picker / 우 70% working) 구조라 AI 출력 복사 시 가용 폭이 좁아 불편. 열려있는 세션 간 전환은 매번 picker로 돌아가서 선택해야 함.

## 목표
1. Picking panel — 활성 세션을 항상 최상단에 표시
2. Working panel — 기본적으로 풀스크린, 세션 요약 바와 세션 로테이션 단축키 제공

---

## 1. Picking Panel — 활성 세션 우선 정렬

### 동작
- **1순위**: 활성 AI 세션이 열려있는 worktree (tmux `wt-*` window 존재)
- **2순위**: 활성 세션 없는 worktree
- 각 그룹 **내부 정렬은 기존과 동일** (worktree.Scan 결과 순서)
- 터미널만 열려있는 경우(AI 세션 없음)는 "열림"으로 간주하지 않음 — AI 세션 존재 여부 기준

### 구현 위치
`ui/picker.go`의 `filteredDirectories()` — 검색 필터를 적용한 뒤 active/inactive로 partition 해서 반환.

```go
func (m *PickerModel) filteredDirectories() []int {
    // ... (기존 검색 필터링)
    activeSet := tmuxActiveSet() // cache: ListDirectoryWindows()
    var active, inactive []int
    for _, i := range matched {
        if activeSet[m.directories[i].Name] {
            active = append(active, i)
        } else {
            inactive = append(inactive, i)
        }
    }
    return append(active, inactive...)
}
```

### 엣지 케이스
- 세션이 새로 열리거나 종료되면 목록 순서가 바뀜 → 커서 위치가 "같은 worktree"에 머무르도록 보정 (indice가 아닌 worktree name 기반 복원)
  - 세션 열기/종료 직후의 시각적 점프는 허용. 단, 현재 cursor가 가리키던 worktree를 기억해서 정렬 후에도 그 worktree에 cursor를 맞춤

---

## 2. Working Panel — 풀스크린화

### 2.1 풀스크린 전환 (tmux pane zoom)

**구조는 유지**: 좌측 picker pane + 우측 working pane 분할은 그대로.
**차이**: `auto_zoom = true` 일 때, 세션을 보고 있으면 항상 working pane을 **zoom**(tmux 내장)해서 풀스크린으로 표시. `false` 일 때는 기존 분할뷰 유지 (현재 동작과 동일).

#### 설정 (config.toml)
```toml
# user scope (~/.asm/config.toml) 또는 project scope (.asm/config.toml)
auto_zoom = true   # default: true
```

- `Config` struct에 `AutoZoom *bool` 필드 추가 (`toml:"auto_zoom"`, nil → default true)
- `(c *Config) IsAutoZoomEnabled() bool` helper 추가 (DesktopNotifications 패턴 재사용)
- merge: overlay.AutoZoom != nil 일 때 덮어씀
- 설정 UI(`settings_dialog.go`)에 토글 항목 추가

```
tmux resize-pane -Z -t asm:main.1   # working pane을 zoom
```

#### 동작 규약
| 상황 | 동작 |
|------|------|
| Enter로 세션 열기 | working pane zoom + focus |
| Ctrl+G (picker에서) | working pane zoom + focus (세션 없으면 기존대로 새 세션 시작) |
| Ctrl+G (working에서) | working pane unzoom + picker focus |
| 세션 종료 감지 | unzoom + picker focus |
| Settings/Worktree/Delete/Provider dialog 열림 | unzoom (dialog는 pane 1을 일시 사용) |
| Dialog 닫힘 후 세션이 표시 중이면 | 다시 zoom |
| Ctrl+T (터미널 토글) | 유지 — 터미널도 zoom된 상태로 표시 |
| Ctrl+N (새 세션) | 기존 세션 kill 후 재생성 → zoom |

#### tmux zoom 상태 체크
`tmux display-message -p '#{window_zoomed_flag}'` → `1`이면 zoomed.

### 2.2 상단 요약 바 (tmux status-left) — 전체 활성 세션 표시

풀스크린 상태에서 picker가 숨겨지므로, **모든 활성 세션**의 정보를 tmux status line으로 노출.

#### 포맷
```
 ▶ API 문서 thinking │ ● 로그인 idle │ ● wt-5110 responding
```

- `▶` + bold = 현재 working panel에 표시 중인 세션
- `●` + 상태별 색상 = 나머지 활성 세션
- 각 항목: `아이콘 이름 상태`
  - **이름**: task name (tracker 해석값) 우선, 없으면 worktree 폴더명
  - **상태**: 한 단어 (`thinking` / `responding` / `tool-use` / `idle` / `done`)
- 경과시간, 프로바이더명 등은 **미표시** (picker에서 확인)
- 구분자: ` │ `
- 순서: picker 정렬과 동일 (active 그룹 내부 = worktree.Scan 순서)

#### 이름 표시 (scroll)
- 각 이름은 **고정 폭** (기본 20 columns) 내에서 표시
- 폭 초과 시 picker의 `scrollText(name, 20, m.scrollTick)` 그대로 재사용 (좌 슬라이드 + 앞뒤 pause)

#### 구현
- tmux status line 초기 설정 (CreateSession 시):
  ```
  tmux set-option -t asm status on
  tmux set-option -t asm status-position top
  tmux set-option -t asm status-style "bg=colour236,fg=colour252"
  tmux set-option -t asm status-left-length 500
  tmux set-option -t asm status-right ""
  tmux set-option -t asm status-left "#{@asm-working-summary}"
  ```
- picker가 `@asm-working-summary` 세션 변수를 갱신:
  ```
  tmux set-option -t asm @asm-working-summary "#[fg=cyan,bold]▶ ...#[default]│..."
  ```
- 색상은 lipgloss가 아닌 **tmux format 문자열** (`#[fg=colour10,bold]...#[default]`)로 렌더링
- 활성 세션 0개면 status line **OFF** (picker 전체 화면일 때 불필요)

#### 업데이트 주기 & 캐싱
- `scrollTickCmd` (200ms) 에 맞춰 요약 문자열 재생성
- 이전 값과 동일하면 `tmux set-option` **호출 생략** (fork 비용 절감)
- provider state는 1초 tick으로 별도 갱신되며, 그 값을 캐시에서 읽어와 렌더에 사용

### 2.3 세션 로테이션 단축키

열려있는(active) 세션끼리 순환. 현재 zoom 상태는 유지.

#### 단축키
- **Ctrl+>** (= `Ctrl+Shift+.`) — 다음 활성 세션
- **Ctrl+<** (= `Ctrl+Shift+,`) — 이전 활성 세션

터미널 호환성을 위해 `Ctrl+.` / `Ctrl+,` 도 함께 바인딩 (Terminal.app 등은 Ctrl+Shift 구분 못 하므로 shift 없는 조합을 대신 전달). 어느 쪽으로 눌러도 동일 동작.

#### 순환 순서
picker 목록 정렬과 동일 (active 그룹 내부 순서 = worktree.Scan 순서).

#### 구현
- `tmux/tmux.go`의 `CreateSession()`에 바인딩 추가:
  ```
  bind-key -T root C-> send-keys -t asm:main.0 F14
  bind-key -T root C-< send-keys -t asm:main.0 F13
  bind-key -T root C-. send-keys -t asm:main.0 F14
  bind-key -T root C-, send-keys -t asm:main.0 F13
  ```
- `ui/picker.go`의 `handleKey()`에 F13/F14 case 추가:
  ```go
  case "f14": // Ctrl+>: rotate to next active session
      return m, m.rotateSession(+1)
  case "f13": // Ctrl+<: rotate to prev active session
      return m, m.rotateSession(-1)
  ```
- `rotateSession(delta int)`:
  1. 활성 세션 목록 = picker 정렬 순서 중 active 그룹
  2. 현재 `m.workingDir`의 index 찾아서 `(idx + delta) % len` 으로 다음 세션 결정
  3. `showInWorkingPanel(next)` — 기존 swap-pane 로직 재사용
  4. zoom 유지 (showInWorkingPanel이 unzoom하지 않도록)

#### 엣지 케이스
- 활성 세션이 0개 → 무시
- 활성 세션이 1개 → 무시 (또는 현재 세션에 focus만)
- 활성 세션이 있지만 `workingDir == ""` (터미널 보고 있거나 none) → 첫 번째 활성 세션으로 이동

---

## 3. 키바인딩 요약 (변경/추가)

| 키 | 동작 | 상태 |
|----|------|------|
| Ctrl+> / Ctrl+. | 다음 활성 세션 | **신규** |
| Ctrl+< / Ctrl+, | 이전 활성 세션 | **신규** |
| Ctrl+G | picker ↔ working (auto_zoom=true면 zoom toggle 포함) | 동작 변경 |
| Enter | 세션 열기 (+ auto_zoom=true면 zoom) | 동작 변경 |
| 그 외 | — | 기존과 동일 |

---

## 4. 영향 범위

| 파일 | 변경 내용 |
|------|----------|
| `config/config.go` | `AutoZoom *bool` 필드 + `IsAutoZoomEnabled()` + merge 로직 |
| `ui/settings_dialog.go` | auto_zoom 토글 UI |
| `tmux/tmux.go` | `ZoomWorkingPanel()`, `UnzoomWorkingPanel()`, `IsWorkingPanelZoomed()`, `SetStatusSummary()`, `EnableTopStatusBar()`, `DisableTopStatusBar()` 추가. CreateSession에 Ctrl+>,<,.,' 바인딩 + status-bar 초기 설정 |
| `ui/picker.go` | `filteredDirectories()` 정렬 변경, `showInWorkingPanel()`/`swapOutWorkingPanel()` 등에 zoom 관리, F13/F14 핸들러, providerStateTickMsg에서 status summary 업데이트 |
| `ui/status_bar.go` | 하단 키 힌트에 rotation 키 추가 |
| `spec.md` | 완료 후 키바인딩 테이블 갱신 |

## 5. 구현 순서
1. Picking panel 정렬 — 독립적, 작은 변경
2. tmux zoom helpers + EnableTopStatusBar 유틸
3. Working panel fullscreen flow (enter/exit 모든 경로)
4. Status summary 업데이트
5. Alt+J/K 로테이션
6. spec.md 동기화

## 6. 확정 사항
- status bar: **상단**
- 요약 바: **모든 활성 세션 나열**, 이름은 task name 우선 / 없으면 폴더명, 폭 초과 시 scroll
- 로테이션 키: **Ctrl+> / Ctrl+<** (호환용 `Ctrl+. / Ctrl+,` 병행)
- 풀스크린: **`auto_zoom` 설정으로 토글 가능** (config.toml, default true)
