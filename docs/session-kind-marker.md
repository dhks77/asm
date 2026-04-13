# Session Kind Marker — Spec

## 배경
asm은 한 worktree에 대해 두 종류의 세션을 띄울 수 있다:
- **AI 세션** — tmux window `wt-<name>` (Claude/Codex/...)
- **터미널 세션** — tmux window `term-<name>`

현재는 picker 좌측 리스트와 하단 tmux status bar 모두 **AI 세션만 "활성"으로 간주**하고 터미널을 표시하지 않는다. 그 결과:

1. `ui/picker.go:1139` `activeSet := ListDirectoryWindows()` — `wt-*` 윈도우만 집계. 터미널 세션은 하단 바에서 보이지 않음.
2. `buildLine1`이 `m.workingDir`(AI가 working panel에 떠있는 worktree)만 "current"로 인식. 터미널이 떠있을 땐(`m.workingDir==""`, `m.termDir!=""`) **첫 번째 AI 세션을 임의로 `▶`로 표시**하는 버그.
3. picker 좌측 리스트 indicator(`●`)는 AI/터미널 구분 없음 (`picker.go:1470`). 한 worktree에 둘 다 떠있는지 알 수 없음.

## 목표
1. picker 좌측 리스트와 하단 status bar에서 세션의 종류(AI/Terminal)를 한눈에 구분
2. 터미널이 working panel에 떠있을 때 하단 status bar가 정확히 그 상태를 반영
3. 한 worktree에 AI + 터미널이 동시에 떠있어도 올바르게 표시

---

## 1. 세션 종류 모델

### 1.1 SessionKind
picker 모델에 세션 종류를 나타내는 플래그 집합을 도입한다.

```go
// ui/picker.go
type SessionKind uint8

const (
    SessionAI  SessionKind = 1 << iota // "a"
    SessionTerm                         // "t"
)
```

한 worktree는 `SessionKind` 비트마스크로 상태를 가진다:
- `0` (없음)
- `SessionAI` (AI만)
- `SessionTerm` (터미널만)
- `SessionAI | SessionTerm` (둘 다)

### 1.2 활성 세션 집계
현재 `ListDirectoryWindows()`는 `wt-*`만 반환한다. 터미널 윈도우(`term-*`)도 함께 집계하는 헬퍼를 추가한다.

```go
// tmux/tmux.go
// ListActiveSessions returns per-directory kind flags based on live tmux windows.
// Keys: directory name (without prefix). Values: bitmask of kinds.
func ListActiveSessions() map[string]SessionKind
```

내부 구현:
- `tmux list-windows` 한 번 호출
- `wt-<name>` → `SessionAI` 플래그 추가
- `term-<name>` → `SessionTerm` 플래그 추가

picker가 매 refresh마다 이 맵을 사용하고, 기존 `ListDirectoryWindows`는 내부적으로 이 함수 위에 얇은 래퍼로 유지한다 (다른 호출처 호환).

---

## 2. 하단 status bar

### 2.1 line1 — 현재 working panel 세션
현재는 `target = m.workingDir` 기준. `m.workingDir`이 비었지만 `m.termDir`이 있으면 터미널을 target으로 삼아야 한다.

```go
func (m *PickerModel) buildLine1(active map[string]SessionKind) string {
    var target string
    var currentKind SessionKind
    switch {
    case m.workingDir != "":
        target, currentKind = m.workingDir, SessionAI
    case m.termDir != "":
        target, currentKind = m.termDir, SessionTerm
    default:
        // fallback: 첫 번째 활성 (AI 우선, 없으면 터미널)
    }
    // ...
}
```

**렌더 변경**: folder name 뒤에 kind 배지를 표시.
```
▶ folder-name        [a] │ task-name          │ responding  12m
▶ folder-name        [t] │ (terminal)         │ —          05m
▶ folder-name        [a+t] │ task-name        │ responding  12m   ← 둘 다 떠있는 경우
```

- `[a]` = `colour141` (AI 색상과 동일)
- `[t]` = `colour215` (신규 — 터미널 전용 주황톤)
- `[a+t]` = 두 색을 이어서 `#[fg=colour141]a#[fg=colour244]+#[fg=colour215]t`

터미널 전용(target이 Terminal)일 때 task-name/state 컬럼은 비우고(`—` placeholder), elapsed는 `sessionStartTimes`에 터미널 생성 시각을 별도로 저장하여 표시한다.

### 2.2 line2 — 그 외 활성 세션
모든 활성 worktree(AI 또는 터미널 또는 둘 다) 중 **line1의 target을 제외**하고 한 줄씩. 한 worktree가 둘 다 띄워져 있어도 **한 항목으로 묶고** kind 배지로 표시한다.

```
● name-1    [a]   responding │ ● name-2    [t]        — │ ● name-3    [a+t]  thinking
```

배지 색은 line1과 동일 규칙.

### 2.3 icon / current 강조
- `▶` = 현재 working panel 표시 중 (line1 target, line2에서는 제외)
- `●` = 활성

line2 렌더시 `isCurrent` 분기는 사라진다 (line1과 중복 제거). 현재 코드에서 `wt.Name == m.workingDir`로 `▶`도 line2에 나올 수 있는 경로가 남아있는데, 이를 정리한다.

### 2.4 session start time
지금은 `m.sessionStartTimes[name]`가 AI 세션 생성 시각 하나만 저장. 터미널 elapsed도 보여주려면 별도 맵이 필요.

```go
sessionStartTimes     map[string]time.Time // AI
terminalStartTimes    map[string]time.Time // Terminal
```

line1/line2 렌더시 target의 kind에 따라 적절한 맵에서 조회. `a+t`면 AI 시작시각 우선.

---

## 3. picker 좌측 리스트

### 3.1 indicator
기존 dot 옆에 kind 배지를 추가한다.

```
  ● folder-name           [a]
    task name
    thinking  3m
  ● folder-name           [t]
  ● folder-name           [a+t]
    task name
    responding  12m
  ○ folder-name
```

dot의 색상 규칙은 기존 유지 (`m.workingDir == name || m.termDir == name`이면 activeColor bold, hasSession이면 기본 active, 아니면 inactive).

### 3.2 kind 배지 위치
line 1의 primary name 끝(오른쪽)에 배지를 붙인다. maxNameWidth 계산에서 배지 폭만큼 제외한다.

```go
const kindBadgeWidth = 6 // " [a+t]" 최대 폭
maxNameWidth := m.width - prefixWidth - kindBadgeWidth
```

배지 스타일은 `ui/styles.go`에 추가:
```go
kindAIBadgeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
kindTermBadgeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("215"))
kindSepStyle        = lipgloss.NewStyle().Foreground(dimColor)
```

### 3.3 정렬
`filteredDirectories()`의 active/inactive 파티션 기준을 **AI 또는 터미널 중 하나라도 열림**으로 확장한다. 즉 `activeKinds[name] != 0`이면 active 그룹.

> 주의: 기존 `panel-improvements.md` §1은 "AI 세션 있음"만 active 기준으로 정의했으나, 본 스펙에서 이를 갱신한다. 터미널도 사용자가 의식적으로 열어둔 작업 컨텍스트이므로 상단 노출이 맞다.

그룹 내부 정렬 순서는 기존 `worktree.Scan()` 순서 유지. AI vs 터미널 간 추가 tiebreak는 두지 않는다.

`hasSession`은 `activeKinds[wt.Name] != 0`으로 계산하고, `renderItem`에는 `kind SessionKind`를 함께 넘겨 배지 렌더에 사용한다.

---

## 4. 영향받는 파일

| 파일 | 변경 |
|------|------|
| `tmux/tmux.go` | `ListActiveSessions()` 추가. `ListDirectoryWindows()`는 호환 래퍼로 |
| `ui/picker.go` | `SessionKind` 타입, `activeSet` → `activeKinds` 전환. `buildLine1/2`, `renderStatusItem`, `renderItem`, `filteredDirectories` 수정. `terminalStartTimes` 맵 추가 (터미널 생성/정리 시점에 관리) |
| `ui/styles.go` | kind 배지 스타일 추가 |

---

## 5. 엣지 케이스

- **터미널 종료 → AI 세션만 남음**: `terminalExitedMsg` 처리에서 `terminalStartTimes`도 delete. line1 target은 fallback 규칙에 따라 AI로 이동하거나 비어감.
- **AI 종료 → 터미널만 남음**: `sessionExitedMsg` 처리에서 AI 관련 상태 정리. 터미널 indicator는 계속 `●`로 표시.
- **한 worktree에 둘 다 있을 때 `Ctrl+t` 토글**: 기존 `toggleTerminal()` 로직 유지. target만 바뀌고 둘 다 kind 배지에는 `a+t`로 계속 표시.
- **line1 current가 사라졌는데 line2에 복제 표시 방지**: `buildLine2`에서 `wt.Name == target` 스킵 (기존 `m.workingDir` 체크를 일반화).

---

## 6. 비목표 (Non-goals)
- active 그룹 안에서 AI vs 터미널 우선순위 구분 — 섞여도 무방.
- 터미널 세션에 대한 provider state 감지 (terminal은 항상 `—`).
- 알림(desktop notification)은 AI 세션 기반만 유지.

---

## 7. 수용 기준 (Acceptance)
1. picker 좌측에서 각 worktree 옆에 `[a]`/`[t]`/`[a+t]` 배지가 정확히 표시된다.
2. 하단 line1의 `▶`는 실제 working panel에 떠있는 세션(AI 또는 터미널) 기준이다.
3. 한 worktree에 AI와 터미널이 모두 떠있으면 line1/line2에 **한 항목**으로 표시되고 `[a+t]` 배지가 붙는다.
4. 터미널만 열린 worktree도 하단 line2에 나타난다.
5. 세션을 닫으면 배지가 즉시 업데이트된다 (다음 `refreshStatusSummary` tick).
