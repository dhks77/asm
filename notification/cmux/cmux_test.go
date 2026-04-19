package cmuxnotify

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nhn/asm/terminaldetect"
)

func TestResolveCLIPathPrefersBundledPath(t *testing.T) {
	prevStat := statPath
	prevLookPath := lookPath
	defer func() {
		statPath = prevStat
		lookPath = prevLookPath
	}()

	statPath = func(name string) (os.FileInfo, error) {
		if name != "/tmp/cmux-bin" {
			t.Fatalf("stat path = %q, want %q", name, "/tmp/cmux-bin")
		}
		return fakeFileInfo{}, nil
	}
	lookPath = func(name string) (string, error) {
		t.Fatalf("lookPath should not be called")
		return "", nil
	}

	got, err := resolveCLIPath(terminaldetect.CMUXMetadata{BundledCLIPath: "/tmp/cmux-bin"})
	if err != nil {
		t.Fatalf("resolveCLIPath() error = %v", err)
	}
	if got != "/tmp/cmux-bin" {
		t.Fatalf("resolveCLIPath() = %q, want %q", got, "/tmp/cmux-bin")
	}
}

func TestBuildEnvUsesMinimalBaseEnv(t *testing.T) {
	prevEnv := currentEnv
	defer func() { currentEnv = prevEnv }()

	currentEnv = func() []string {
		return []string{
			"TMUX=/tmp/tmux",
		}
	}

	got := buildEnv(terminaldetect.Info{
		Env: map[string]string{
			"HOME":    "/tmp/home",
			"PATH":    "/usr/bin:/bin",
			"LANG":    "en_US.UTF-8",
			"USER":    "tester",
			"LOGNAME": "tester",
		},
		CMUX: &terminaldetect.CMUXMetadata{
			WorkspaceID:    "workspace:1",
			SurfaceID:      "surface:2",
			BundledCLIPath: "/tmp/cmux-bin",
		},
	})

	for _, want := range []string{
		"HOME=/tmp/home",
		"PATH=/usr/bin:/bin",
		"LANG=en_US.UTF-8",
		"USER=tester",
		"LOGNAME=tester",
		"CMUX_WORKSPACE_ID=workspace:1",
		"CMUX_SURFACE_ID=surface:2",
		"CMUX_BUNDLED_CLI_PATH=/tmp/cmux-bin",
	} {
		if !slices.Contains(got, want) {
			t.Fatalf("buildEnv() missing %q in %#v", want, got)
		}
	}
}

func TestSendUsesClaudeHookForClaudeProvider(t *testing.T) {
	prevStat := statPath
	prevRun := runCommand
	defer func() {
		statPath = prevStat
		runCommand = prevRun
	}()

	statPath = func(name string) (os.FileInfo, error) { return fakeFileInfo{}, nil }

	var gotName string
	var gotArgs []string
	var gotEnv []string
	var gotStdin string
	runCommand = func(ctx context.Context, name string, args []string, env []string, stdin []byte) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		gotStdin = string(stdin)
		return nil
	}

	err := Send("ASM - task", "done", "claude", terminaldetect.Info{
		Kind: terminaldetect.KindCMUX,
		CMUX: &terminaldetect.CMUXMetadata{
			WorkspaceID:    "workspace:1",
			SurfaceID:      "surface:2",
			BundledCLIPath: "/tmp/cmux-bin",
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotName != "/tmp/cmux-bin" {
		t.Fatalf("name = %q, want %q", gotName, "/tmp/cmux-bin")
	}
	wantArgs := []string{"claude-hook", "notification", "--workspace", "workspace:1", "--surface", "surface:2"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	if !strings.Contains(gotStdin, `"message":"done"`) {
		t.Fatalf("stdin = %q, want JSON message", gotStdin)
	}
	if len(gotEnv) == 0 {
		t.Fatalf("env should not be empty")
	}
}

func TestSendFallsBackToGenericNotifyWhenClaudeHookFails(t *testing.T) {
	prevStat := statPath
	prevRun := runCommand
	defer func() {
		statPath = prevStat
		runCommand = prevRun
	}()

	statPath = func(name string) (os.FileInfo, error) { return fakeFileInfo{}, nil }

	var calls [][]string
	runCommand = func(ctx context.Context, name string, args []string, env []string, stdin []byte) error {
		calls = append(calls, append([]string(nil), args...))
		if len(calls) == 1 {
			return errors.New("broken pipe")
		}
		return nil
	}

	err := Send("ASM - task", "done", "claude", terminaldetect.Info{
		Kind: terminaldetect.KindCMUX,
		CMUX: &terminaldetect.CMUXMetadata{
			WorkspaceID:    "workspace:1",
			SurfaceID:      "surface:2",
			BundledCLIPath: "/tmp/cmux-bin",
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("runCommand calls = %d, want 2", len(calls))
	}
	if calls[0][0] != "claude-hook" || calls[1][0] != "notify" {
		t.Fatalf("calls = %#v, want claude-hook then notify", calls)
	}
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "cmux" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0o755 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }
