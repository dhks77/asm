package notification

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nhn/asm/terminaldetect"
)

func TestSendRequestUsesCMUXWhenDetected(t *testing.T) {
	prevDetect := detectTerminal
	prevCMUX := sendCMUXNotification
	prevOS := sendOSNotification
	prevInside := asmtmuxIsInsideTmux
	prevEnable := enableTMUXPass
	prevPassthrough := sendTMUXPassthrough
	prevTTY := sendClientTTYNotice
	prevStdout := stdoutSupportsTMUX
	defer func() {
		detectTerminal = prevDetect
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
		sendClientTTYNotice = prevTTY
		stdoutSupportsTMUX = prevStdout
	}()

	cmuxCalls := 0
	osCalls := 0
	asmtmuxIsInsideTmux = func() bool { return false }
	enableTMUXPass = func() {}
	sendTMUXPassthrough = func(title, body string) error { return nil }
	sendClientTTYNotice = func(ttyPath, title, body string) error { return nil }
	stdoutSupportsTMUX = func() bool { return true }
	detectTerminal = func(sessionName string) (terminaldetect.Info, error) {
		if sessionName != "asm-default" {
			t.Fatalf("Detect() session = %q, want %q", sessionName, "asm-default")
		}
		return terminaldetect.Info{
			Kind: terminaldetect.KindCMUX,
			CMUX: &terminaldetect.CMUXMetadata{WorkspaceID: "workspace:1"},
		}, nil
	}
	sendCMUXNotification = func(title, body, hook string, info terminaldetect.Info) error {
		cmuxCalls++
		if hook != "claude-hook" {
			t.Fatalf("hook = %q, want %q", hook, "claude-hook")
		}
		return nil
	}
	sendOSNotification = func(title, body string, info terminaldetect.Info) error {
		osCalls++
		return nil
	}

	SendRequest(Request{Title: "ASM", Body: "done", Provider: "claude", CMUXHook: "claude-hook", SessionName: "asm-default"})

	if cmuxCalls != 1 {
		t.Fatalf("cmux calls = %d, want 1", cmuxCalls)
	}
	if osCalls != 0 {
		t.Fatalf("os calls = %d, want 0", osCalls)
	}
}

func TestSendRequestFallsBackToOSWhenCMUXFails(t *testing.T) {
	prevDetect := detectTerminal
	prevCMUX := sendCMUXNotification
	prevOS := sendOSNotification
	prevInside := asmtmuxIsInsideTmux
	prevEnable := enableTMUXPass
	prevPassthrough := sendTMUXPassthrough
	prevTTY := sendClientTTYNotice
	prevStdout := stdoutSupportsTMUX
	defer func() {
		detectTerminal = prevDetect
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
		sendClientTTYNotice = prevTTY
		stdoutSupportsTMUX = prevStdout
	}()

	cmuxCalls := 0
	osCalls := 0
	asmtmuxIsInsideTmux = func() bool { return false }
	enableTMUXPass = func() {}
	sendTMUXPassthrough = func(title, body string) error { return nil }
	sendClientTTYNotice = func(ttyPath, title, body string) error { return nil }
	stdoutSupportsTMUX = func() bool { return true }
	detectTerminal = func(sessionName string) (terminaldetect.Info, error) {
		return terminaldetect.Info{
			Kind: terminaldetect.KindCMUX,
			CMUX: &terminaldetect.CMUXMetadata{WorkspaceID: "workspace:1"},
		}, nil
	}
	sendCMUXNotification = func(title, body, hook string, info terminaldetect.Info) error {
		cmuxCalls++
		return errors.New("broken pipe")
	}
	sendOSNotification = func(title, body string, info terminaldetect.Info) error {
		osCalls++
		return nil
	}

	SendRequest(Request{Title: "ASM", Body: "done"})

	if cmuxCalls != 1 {
		t.Fatalf("cmux calls = %d, want 1", cmuxCalls)
	}
	if osCalls != 1 {
		t.Fatalf("os calls = %d, want 1", osCalls)
	}
}

func TestSendRequestUsesHelperForCMUXInsideTmux(t *testing.T) {
	prevDetect := detectTerminal
	prevSpawn := spawnHelper
	prevCMUX := sendCMUXNotification
	prevOS := sendOSNotification
	prevInside := asmtmuxIsInsideTmux
	prevEnable := enableTMUXPass
	prevPassthrough := sendTMUXPassthrough
	prevTTY := sendClientTTYNotice
	prevStdout := stdoutSupportsTMUX
	defer func() {
		detectTerminal = prevDetect
		spawnHelper = prevSpawn
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
		sendClientTTYNotice = prevTTY
		stdoutSupportsTMUX = prevStdout
	}()

	spawnCalls := 0
	enableCalls := 0
	detectTerminal = func(sessionName string) (terminaldetect.Info, error) {
		return terminaldetect.Info{
			Kind: terminaldetect.KindCMUX,
			CMUX: &terminaldetect.CMUXMetadata{WorkspaceID: "workspace:1"},
		}, nil
	}
	asmtmuxIsInsideTmux = func() bool { return true }
	enableTMUXPass = func() { enableCalls++ }
	sendTMUXPassthrough = func(title, body string) error { return errors.New("passthrough failed") }
	sendClientTTYNotice = func(ttyPath, title, body string) error { return errors.New("tty failed") }
	stdoutSupportsTMUX = func() bool { return true }
	spawnHelper = func(req Request, info terminaldetect.Info) error {
		spawnCalls++
		return nil
	}
	sendCMUXNotification = func(title, body, hook string, info terminaldetect.Info) error {
		t.Fatal("cmux backend should not be called directly when helper spawn succeeds")
		return nil
	}
	sendOSNotification = func(title, body string, info terminaldetect.Info) error {
		t.Fatal("os backend should not be called when helper spawn succeeds")
		return nil
	}

	SendRequest(Request{Title: "ASM", Body: "done"})

	if spawnCalls != 1 {
		t.Fatalf("helper spawn calls = %d, want 1", spawnCalls)
	}
	if enableCalls != 1 {
		t.Fatalf("enable passthrough calls = %d, want 1", enableCalls)
	}
}

func TestSendRequestUsesTMUXPassthroughForCMUXInsideTmux(t *testing.T) {
	prevDetect := detectTerminal
	prevSpawn := spawnHelper
	prevCMUX := sendCMUXNotification
	prevOS := sendOSNotification
	prevInside := asmtmuxIsInsideTmux
	prevEnable := enableTMUXPass
	prevPassthrough := sendTMUXPassthrough
	prevTTY := sendClientTTYNotice
	prevStdout := stdoutSupportsTMUX
	defer func() {
		detectTerminal = prevDetect
		spawnHelper = prevSpawn
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
		sendClientTTYNotice = prevTTY
		stdoutSupportsTMUX = prevStdout
	}()

	passthroughCalls := 0
	enableCalls := 0
	detectTerminal = func(sessionName string) (terminaldetect.Info, error) {
		return terminaldetect.Info{
			Kind: terminaldetect.KindCMUX,
			CMUX: &terminaldetect.CMUXMetadata{WorkspaceID: "workspace:1"},
		}, nil
	}
	asmtmuxIsInsideTmux = func() bool { return true }
	enableTMUXPass = func() { enableCalls++ }
	sendTMUXPassthrough = func(title, body string) error {
		passthroughCalls++
		return nil
	}
	sendClientTTYNotice = func(ttyPath, title, body string) error {
		t.Fatal("client tty should not be called when passthrough succeeds")
		return nil
	}
	stdoutSupportsTMUX = func() bool { return true }
	spawnHelper = func(req Request, info terminaldetect.Info) error {
		t.Fatal("helper spawn should not be called when passthrough succeeds")
		return nil
	}
	sendCMUXNotification = func(title, body, hook string, info terminaldetect.Info) error {
		t.Fatal("cmux backend should not be called when passthrough succeeds")
		return nil
	}
	sendOSNotification = func(title, body string, info terminaldetect.Info) error {
		t.Fatal("os backend should not be called when passthrough succeeds")
		return nil
	}

	SendRequest(Request{Title: "ASM", Body: "done"})

	if passthroughCalls != 1 {
		t.Fatalf("passthrough calls = %d, want 1", passthroughCalls)
	}
	if enableCalls != 1 {
		t.Fatalf("enable passthrough calls = %d, want 1", enableCalls)
	}
}

func TestSendRequestUsesTMUXPassthroughForNonASCIIInsideTmux(t *testing.T) {
	prevDetect := detectTerminal
	prevSpawn := spawnHelper
	prevCMUX := sendCMUXNotification
	prevOS := sendOSNotification
	prevInside := asmtmuxIsInsideTmux
	prevEnable := enableTMUXPass
	prevPassthrough := sendTMUXPassthrough
	prevTTY := sendClientTTYNotice
	prevStdout := stdoutSupportsTMUX
	defer func() {
		detectTerminal = prevDetect
		spawnHelper = prevSpawn
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
		sendClientTTYNotice = prevTTY
		stdoutSupportsTMUX = prevStdout
	}()

	spawnCalls := 0
	enableCalls := 0
	passthroughCalls := 0
	detectTerminal = func(sessionName string) (terminaldetect.Info, error) {
		return terminaldetect.Info{
			Kind: terminaldetect.KindCMUX,
			CMUX: &terminaldetect.CMUXMetadata{WorkspaceID: "workspace:1"},
		}, nil
	}
	asmtmuxIsInsideTmux = func() bool { return true }
	enableTMUXPass = func() { enableCalls++ }
	sendTMUXPassthrough = func(title, body string) error {
		passthroughCalls++
		return nil
	}
	sendClientTTYNotice = func(ttyPath, title, body string) error {
		t.Fatal("client tty should not be called when passthrough succeeds")
		return nil
	}
	stdoutSupportsTMUX = func() bool { return true }
	spawnHelper = func(req Request, info terminaldetect.Info) error {
		spawnCalls++
		return nil
	}
	sendCMUXNotification = func(title, body, hook string, info terminaldetect.Info) error {
		t.Fatal("cmux backend should not be called when passthrough succeeds")
		return nil
	}
	sendOSNotification = func(title, body string, info terminaldetect.Info) error {
		t.Fatal("os backend should not be called when passthrough succeeds")
		return nil
	}

	SendRequest(Request{Title: "ASM", Body: "안녕하세요", Provider: "codex"})

	if spawnCalls != 0 {
		t.Fatalf("helper spawn calls = %d, want 0", spawnCalls)
	}
	if passthroughCalls != 1 {
		t.Fatalf("passthrough calls = %d, want 1", passthroughCalls)
	}
	if enableCalls != 1 {
		t.Fatalf("enable passthrough calls = %d, want 1", enableCalls)
	}
}

func TestSendRequestSkipsTMUXPassthroughWhenStdoutIsNotTTY(t *testing.T) {
	prevDetect := detectTerminal
	prevSpawn := spawnHelper
	prevCMUX := sendCMUXNotification
	prevOS := sendOSNotification
	prevInside := asmtmuxIsInsideTmux
	prevEnable := enableTMUXPass
	prevPassthrough := sendTMUXPassthrough
	prevTTY := sendClientTTYNotice
	prevStdout := stdoutSupportsTMUX
	defer func() {
		detectTerminal = prevDetect
		spawnHelper = prevSpawn
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
		sendClientTTYNotice = prevTTY
		stdoutSupportsTMUX = prevStdout
	}()

	passthroughCalls := 0
	enableCalls := 0
	ttyCalls := 0
	detectTerminal = func(sessionName string) (terminaldetect.Info, error) {
		return terminaldetect.Info{
			Kind:      terminaldetect.KindCMUX,
			CMUX:      &terminaldetect.CMUXMetadata{WorkspaceID: "workspace:1"},
			ClientTTY: "/dev/ttys011",
		}, nil
	}
	asmtmuxIsInsideTmux = func() bool { return true }
	stdoutSupportsTMUX = func() bool { return false }
	enableTMUXPass = func() { enableCalls++ }
	sendTMUXPassthrough = func(title, body string) error {
		passthroughCalls++
		return nil
	}
	sendClientTTYNotice = func(ttyPath, title, body string) error {
		ttyCalls++
		if ttyPath != "/dev/ttys011" {
			t.Fatalf("ttyPath = %q, want %q", ttyPath, "/dev/ttys011")
		}
		return nil
	}
	spawnHelper = func(req Request, info terminaldetect.Info) error {
		t.Fatal("helper spawn should not be called when client tty succeeds")
		return nil
	}
	sendOSNotification = func(title, body string, info terminaldetect.Info) error {
		t.Fatal("os backend should not be called when client tty succeeds")
		return nil
	}

	SendRequest(Request{Title: "ASM", Body: "done", Provider: "codex"})

	if passthroughCalls != 0 {
		t.Fatalf("passthrough calls = %d, want 0", passthroughCalls)
	}
	if enableCalls != 0 {
		t.Fatalf("enable passthrough calls = %d, want 0", enableCalls)
	}
	if ttyCalls != 1 {
		t.Fatalf("client tty calls = %d, want 1", ttyCalls)
	}
}

func TestRunHelperDeliversResolvedPayload(t *testing.T) {
	prevCMUX := sendCMUXNotification
	prevOS := sendOSNotification
	defer func() {
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
	}()

	cmuxCalls := 0
	sendCMUXNotification = func(title, body, hook string, info terminaldetect.Info) error {
		cmuxCalls++
		if title != "ASM" || body != "done" || hook != "claude-hook" {
			t.Fatalf("payload = (%q, %q, %q), want (%q, %q, %q)", title, body, hook, "ASM", "done", "claude-hook")
		}
		return nil
	}
	sendOSNotification = func(title, body string, info terminaldetect.Info) error {
		t.Fatal("os backend should not be called")
		return nil
	}

	raw, err := json.Marshal(helperPayload{
		Request: Request{Title: "ASM", Body: "done", Provider: "claude", CMUXHook: "claude-hook", SessionName: "asm-dcm"},
		Info: terminaldetect.Info{
			Kind: terminaldetect.KindCMUX,
			CMUX: &terminaldetect.CMUXMetadata{WorkspaceID: "workspace:1"},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := RunHelper(base64.StdEncoding.EncodeToString(raw)); err != nil {
		t.Fatalf("RunHelper() error = %v", err)
	}
	if cmuxCalls != 1 {
		t.Fatalf("cmux calls = %d, want 1", cmuxCalls)
	}
}

func TestSanitizeTextStripsANSIAndControlChars(t *testing.T) {
	got := sanitizeText(" \x1b[31mASM\x1b[0m\tdone\x00\nnext ", 50)
	want := "ASM done next"
	if got != want {
		t.Fatalf("sanitizeText() = %q, want %q", got, want)
	}
}

func TestSanitizeTextDropsReplacementRunes(t *testing.T) {
	got := sanitizeText("브�치가 main이어� 바�", 50)
	want := "브치가 main이어 바"
	if got != want {
		t.Fatalf("sanitizeText() = %q, want %q", got, want)
	}
}

func TestSanitizeOSCTextRemovesSeparators(t *testing.T) {
	got := sanitizeOSCText("build;done")
	if got != "build,done" {
		t.Fatalf("sanitizeOSCText() = %q, want %q", got, "build,done")
	}
}

func TestBuildTMUXPassthroughNotificationUsesOSC777ForASCII(t *testing.T) {
	got := buildTMUXPassthroughNotification("ASM", "done")
	if !strings.Contains(got, "\x1b\x1b]777;notify;ASM;done\x07") {
		t.Fatalf("buildTMUXPassthroughNotification() = %q, want wrapped OSC 777 payload", got)
	}
}

func TestBuildDirectNotificationUsesOSC777ForASCII(t *testing.T) {
	got := buildDirectNotification("ASM", "done")
	if got != "\x1b]777;notify;ASM;done\x07" {
		t.Fatalf("buildDirectNotification() = %q, want raw OSC 777 payload", got)
	}
}

func TestBuildTMUXPassthroughNotificationUsesOSC99ForNonASCII(t *testing.T) {
	got := buildTMUXPassthroughNotification("ASM", "안녕하세요")
	wantBody := base64.StdEncoding.EncodeToString([]byte("안녕하세요"))
	if !strings.Contains(got, "\x1b\x1b]99;i=asm.") {
		t.Fatalf("buildTMUXPassthroughNotification() = %q, want wrapped OSC 99 title payload", got)
	}
	if !strings.Contains(got, ":d=0:e=1;QVN") {
		t.Fatalf("buildTMUXPassthroughNotification() = %q, want base64 title payload", got)
	}
	if !strings.Contains(got, wantBody) {
		t.Fatalf("buildTMUXPassthroughNotification() = %q, want base64 body %q", got, wantBody)
	}
	if strings.Contains(got, "안녕하세요") {
		t.Fatalf("buildTMUXPassthroughNotification() should not contain raw non-ascii body: %q", got)
	}
}
