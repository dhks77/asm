# Worktree Template Copy — Spec

## 배경
새 worktree를 만들 때마다 git에 올라가지 않는 로컬 전용 파일을 기존 worktree에서 직접 복사해 옮기는 수작업이 반복된다. 대표적인 파일:
- `.env`, `.env.local` 등 비밀/환경 변수
- `.vscode/settings.json`, `.idea/` 같은 IDE 설정
- `CLAUDE.md.local` 같은 프로젝트 로컬 AI 컨텍스트
- DB 접속 설정 등 팀에 공유하지 않는 런타임 설정

이 파일들은 repo마다 구성이 다르므로 **repo 단위로 템플릿을 관리**하고, `Ctrl+W` 로 worktree를 생성하는 순간 자동으로 뿌려지는 플로우가 필요하다.

## 목표
1. `{projectRoot}/.asm/templates/{repo}/` 하위의 **파일**을 새 worktree의 동일한 상대 경로로 복사 (디렉토리는 경로 표현 수단일 뿐, 복사 대상 아님)
2. 파일 목록을 별도로 유지하지 않는다 — 템플릿 디렉토리 자체가 "복사 대상" 선언
3. 충돌(이미 존재) 정책은 전역 설정 하나로 단순화 (`skip` / `overwrite`)
4. 복사 실패는 worktree 생성을 롤백하지 않고 경고만 표시

## 1. 템플릿 디렉토리 규약

### 경로
```
{projectRoot}/.asm/templates/{repo}/...
```
- `{projectRoot}` — `asm` 실행 시 `--path` 로 지정된 루트 (worktree들이 살고 있는 상위 디렉토리)
- `{repo}` — 새 worktree가 속하는 repo의 식별자. `worktree.RepoName(repoDir)` 결과 사용 (`worktree/branch.go`)
  - origin URL 마지막 세그먼트 > main repo 폴더명 순으로 해석
  - 둘 다 실패 시 복사 스킵 (경고 1회)

### 복사 모델 — "파일만 싹싹 복사"
**디렉토리는 복사 대상이 아니다**. 템플릿 디렉토리 구조는 단지 "각 파일의 상대 경로"를 표현하는 수단일 뿐.
- `filepath.WalkDir` 로 하위를 순회하며 **regular file 만** 복사
- 파일 상대 경로(템플릿 루트 기준) → worktree의 동일 상대 경로로 매핑
- 복사 시 필요한 중간 디렉토리는 `os.MkdirAll` 로 암묵적으로 생성 (기본 권한 0755)
- **빈 디렉토리는 생성하지 않음** (템플릿에 빈 디렉토리만 있으면 no-op)
- **디렉토리 자체의 권한/mtime 은 복사/보존하지 않음**
- 심볼릭 링크(파일/디렉토리 불문)는 **무시** (단순화)

### 자동 디렉토리 생성
디렉토리는 두 지점에서 자동으로 만들어진다.

1. **worktree 생성 시 (`ApplyTemplate` 진입)** — `{projectRoot}/.asm/templates/{repo}/` 가 없으면 자동으로 빈 디렉토리 생성. 처음 worktree 를 만들면 해당 repo dir만 생기고 복사는 일어나지 않으며(비어 있음), 이후 사용자가 파일을 채우면 다음 worktree 부터 복사된다.
2. **Settings 의 `Open templates directory` 액션** — `{projectRoot}/.asm/templates/` 를 만든 뒤 `projectRoot` 를 스캔해서 찾은 모든 repo 에 대해 `{projectRoot}/.asm/templates/{repo}/` 를 미리 만든다. 파일 탐색기로 열면 각 repo 에 대응하는 폴더가 이미 준비돼 있다. repo 식별은 `worktree.RepoName` (origin URL 마지막 세그먼트 > main repo 폴더명) 기준.

### 예시
```
projectRoot/
├── .asm/
│   ├── config.toml
│   └── templates/
│       └── nc-dms-backend/          # repo name
│           ├── .env
│           ├── .vscode/
│           │   └── settings.json
│           └── config/
│               └── local.json
├── nc-dms-backend/                   # main repo
├── nc-dms-backend-3680/              # worktree
└── nc-dms-backend-4049/              # worktree (새로 만든 것 → 자동 복사 대상)
```

복사되는 파일 (디렉토리는 필요한 만큼만 자동 생성):
- `.env` → `{new_worktree}/.env`
- `.vscode/settings.json` → `{new_worktree}/.vscode/settings.json`
- `config/local.json` → `{new_worktree}/config/local.json`

### 설계 근거
- **파일 목록 관리 불필요**: 템플릿 디렉토리 자체가 "무엇을 복사할지"를 선언. config에 경로 리스트를 중복 저장하지 않음
- **파일 단위 복사**: 디렉토리 속성(권한, 소유자, 빈 디렉토리)을 복사 대상에서 제외해 로직 단순화
- **repo별 분리**: 하나의 `projectRoot` 아래 여러 repo를 관리하는 asm의 실제 사용 패턴과 일치
- **git 제외 권장**: `.asm/templates/` 는 비밀을 담을 가능성이 크므로 `.gitignore` 에 추가 권장

## 2. 충돌 정책

### 설정
`.asm/config.toml` (프로젝트 스코프만):
```toml
[worktree_template]
on_conflict = "skip"        # "skip" (default) | "overwrite"
```

### 동작
정책은 **파일 단위**로만 적용 (디렉토리는 복사 대상이 아님).

| 상황 | skip | overwrite |
|------|------|-----------|
| 대상 경로에 아무것도 없음 | 복사 | 복사 |
| 대상 경로에 파일 존재 | 건너뜀 (로그만) | 덮어씀 |
| 대상 경로가 디렉토리 | 건너뜀 + 경고 (파괴적 변환 금지) | 건너뜀 + 경고 |
| 중간 경로가 파일(디렉토리여야 하는데) | 건너뜀 + 경고 | 건너뜀 + 경고 |

### 설계 근거
- **skip 기본**: 충돌이 나는 건 템플릿 자체가 repo에 커밋된 경우뿐이라 `skip` 이 안전
- **디렉토리를 파일로 덮어쓰는 케이스는 항상 skip**: `overwrite` 모드여도 데이터 손실 위험 제거

## 3. 트리거 & UI 플로우

### 트리거
`Ctrl+W` 로 worktree 생성이 **성공**한 직후. git worktree add 에러 시에는 복사를 시도하지 않음.

### 구현 위치
`ui/worktree_dialog.go` 의 `createWorktreeFromBranchCmd` / `createWorktreeNewBranchCmd`:
- `worktree.CreateWorktreeFromBranch()` / `CreateWorktreeNewBranch()` 성공 이후
- `WorktreeCreatedMsg` 반환 직전
- 실행: `worktree.ApplyTemplate(projectRoot, repoName, targetPath, policy)` 호출

복사 실패는 `WorktreeCreatedMsg` 에 `TemplateCopied int`, `TemplateWarnings []string` 필드를 추가해 전달. worktree 생성 자체는 성공 취급하고 picker에서 토스트로 노출.

### UI 피드백
- 템플릿 디렉토리가 없으면 조용히 no-op (가장 흔한 초기 상태)
- 복사 발생 시 picker 상단에 `copied N files from template` 토스트
- 부분 실패 시 warning 메시지 토스트

## 4. 복사 세부 규칙

| 항목 | 규칙 |
|------|------|
| 복사 단위 | **파일(regular file)만**. `filepath.WalkDir` 로 순회, 디렉토리 엔트리는 traversal 용도로만 사용 |
| 중간 디렉토리 | 파일을 쓸 때 `os.MkdirAll(parent, 0755)` 로 자동 생성 |
| 빈 디렉토리 | 생성하지 않음 |
| 심볼릭 링크 | 무시 (파일/디렉토리 링크 불문) — 경고 없이 skip |
| 파일 권한 | 원본 mode 보존 (`os.Chmod`) |
| 숨김 파일 (`.xxx`) | 그대로 복사 (`.env` 등이 핵심 유스케이스) |
| `.git/` 하위 | 안전 장치로 traversal 시 제외 |

## 5. 설정 (`config/config.go` 변경)

```go
// WorktreeTemplateConfig controls post-create file templating.
type WorktreeTemplateConfig struct {
    OnConflict string `toml:"on_conflict"` // "skip" (default) | "overwrite"
}

type Config struct {
    // ... 기존 필드 ...
    WorktreeTemplate WorktreeTemplateConfig `toml:"worktree_template"`
}
```

`merge()` 에 추가:
```go
if overlay.WorktreeTemplate.OnConflict != "" {
    base.WorktreeTemplate.OnConflict = overlay.WorktreeTemplate.OnConflict
}
```

Helper:
```go
func (c *Config) TemplateConflictPolicy() string {
    if c.WorktreeTemplate.OnConflict == "overwrite" {
        return "overwrite"
    }
    return "skip" // default
}
```

### 설정 UI
`ui/settings_dialog.go` 에 독립된 **Worktree 섹션**을 두고 아래 항목 배치:
- `Template on Conflict` — `skip` ↔ `overwrite` 2-way 토글 (user/project 스코프 공통, 기존 override 패턴 그대로)
- `Open templates directory` — Enter 로 `{projectRoot}/.asm/templates/` 를 `MkdirAll` 후 OS 파일 탐색기(`open`/`xdg-open`/`explorer`)로 연다

섹션 분리 이유: General(프로바이더/트래커/IDE/줌/피커폭)은 앱 전반 설정이고, Worktree 관련은 생성 플로우 전용이라 UX 상 다른 그룹으로 묶는 편이 탐색이 쉽다.

## 6. 구현 영향 범위

| 파일 | 변경 내용 |
|------|----------|
| `config/config.go` | `WorktreeTemplateConfig` 구조체, `Config.WorktreeTemplate` 필드, merge 로직, `TemplateConflictPolicy()` helper |
| `worktree/template.go` (신규) | `ApplyTemplate(projectRoot, repoName, targetPath, policy)` — `filepath.WalkDir` 기반 파일 단위 복사. repo 디렉토리 없으면 자동 `MkdirAll`. `DiscoverRepoNames(projectRoot)` 로 하위 worktree 스캔 후 unique repo 이름 수집. `OpenTemplatesDir(projectRoot)` 는 templates root + 발견된 모든 repo 폴더를 선제 생성하고 OS 파일 탐색기 호출 |
| `ui/worktree_dialog.go` | `createWorktreeFromBranchCmd` / `createWorktreeNewBranchCmd` 에서 성공 후 `ApplyTemplate` 호출. `WorktreeCreatedMsg` 에 `TemplateCopied`, `TemplateWarnings` 필드 추가 |
| `ui/settings_dialog.go` | 신규 **Worktree 섹션** + `Template on Conflict` 토글 + `Open templates directory` 액션. 새 item kind `action` 및 `renderActionField` helper 도입 |
| `ui/picker.go` | `WorktreeCreatedMsg` 수신 시 복사 결과 토스트 표시 |
| `spec.md` | 설정 파일 섹션에 `[worktree_template]` 항목 및 `.asm/templates/` 디렉토리 구조 반영 |

## 7. 구현 순서
1. `worktree/template.go` — `ApplyTemplate` + 단위 테스트
2. `config/config.go` — 설정 추가 + merge
3. `ui/worktree_dialog.go` — 생성 플로우에 통합, `WorktreeCreatedMsg` 확장
4. `ui/settings_dialog.go` — 토글 항목 추가
5. `ui/picker.go` — 토스트 표시
6. `spec.md` 동기화

## 8. 엣지 케이스
- **repo name 해석 실패**: 복사 스킵, warning 1회 표시
- **`.asm/templates/{repo}/` 없음**: no-op (초기 상태 — 조용히 통과)
- **템플릿 디렉토리는 있는데 비어있음**: no-op, warning 없음
- **복사 중 디스크 에러**: 부분 성공 허용 (복사된 파일은 유지). warning 리스트에 실패 경로 추가
- **템플릿 파일 읽기 불가(권한)**: 해당 파일만 스킵 + warning
- **`{repo}` 이름 sanitize**: `filepath.Base` 로 마지막 세그먼트만 사용해 디렉토리 traversal 방지

## 9. 비목표 (out of scope)
- 템플릿 파일을 TUI에서 직접 편집하는 UI
- per-item overwrite 정책
- 변수 치환 (`{{branch}}` → `feature-3038` 등)
- user 스코프 템플릿 (`~/.asm/templates/`)
- 기존 worktree에 수동으로 템플릿 재적용하는 단축키
