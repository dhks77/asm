package notification

import (
	"encoding/base64"
	"encoding/json"
	"errors"
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
	defer func() {
		detectTerminal = prevDetect
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
	}()

	cmuxCalls := 0
	osCalls := 0
	asmtmuxIsInsideTmux = func() bool { return false }
	enableTMUXPass = func() {}
	sendTMUXPassthrough = func(title, body string) error { return nil }
	detectTerminal = func(sessionName string) (terminaldetect.Info, error) {
		if sessionName != "asm-default" {
			t.Fatalf("Detect() session = %q, want %q", sessionName, "asm-default")
		}
		return terminaldetect.Info{
			Kind: terminaldetect.KindCMUX,
			CMUX: &terminaldetect.CMUXMetadata{WorkspaceID: "workspace:1"},
		}, nil
	}
	sendCMUXNotification = func(title, body, provider string, info terminaldetect.Info) error {
		cmuxCalls++
		if provider != "claude" {
			t.Fatalf("provider = %q, want %q", provider, "claude")
		}
		return nil
	}
	sendOSNotification = func(title, body string, info terminaldetect.Info) error {
		osCalls++
		return nil
	}

	SendRequest(Request{Title: "ASM", Body: "done", Provider: "claude", SessionName: "asm-default"})

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
	defer func() {
		detectTerminal = prevDetect
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
	}()

	cmuxCalls := 0
	osCalls := 0
	asmtmuxIsInsideTmux = func() bool { return false }
	enableTMUXPass = func() {}
	sendTMUXPassthrough = func(title, body string) error { return nil }
	detectTerminal = func(sessionName string) (terminaldetect.Info, error) {
		return terminaldetect.Info{
			Kind: terminaldetect.KindCMUX,
			CMUX: &terminaldetect.CMUXMetadata{WorkspaceID: "workspace:1"},
		}, nil
	}
	sendCMUXNotification = func(title, body, provider string, info terminaldetect.Info) error {
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
	defer func() {
		detectTerminal = prevDetect
		spawnHelper = prevSpawn
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
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
	spawnHelper = func(req Request, info terminaldetect.Info) error {
		spawnCalls++
		return nil
	}
	sendCMUXNotification = func(title, body, provider string, info terminaldetect.Info) error {
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
	defer func() {
		detectTerminal = prevDetect
		spawnHelper = prevSpawn
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
		asmtmuxIsInsideTmux = prevInside
		enableTMUXPass = prevEnable
		sendTMUXPassthrough = prevPassthrough
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
	spawnHelper = func(req Request, info terminaldetect.Info) error {
		t.Fatal("helper spawn should not be called when passthrough succeeds")
		return nil
	}
	sendCMUXNotification = func(title, body, provider string, info terminaldetect.Info) error {
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

func TestRunHelperDeliversResolvedPayload(t *testing.T) {
	prevCMUX := sendCMUXNotification
	prevOS := sendOSNotification
	defer func() {
		sendCMUXNotification = prevCMUX
		sendOSNotification = prevOS
	}()

	cmuxCalls := 0
	sendCMUXNotification = func(title, body, provider string, info terminaldetect.Info) error {
		cmuxCalls++
		if title != "ASM" || body != "done" || provider != "claude" {
			t.Fatalf("payload = (%q, %q, %q), want (%q, %q, %q)", title, body, provider, "ASM", "done", "claude")
		}
		return nil
	}
	sendOSNotification = func(title, body string, info terminaldetect.Info) error {
		t.Fatal("os backend should not be called")
		return nil
	}

	raw, err := json.Marshal(helperPayload{
		Request: Request{Title: "ASM", Body: "done", Provider: "claude", SessionName: "asm-dcm"},
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

func TestSanitizeOSCTextRemovesSeparators(t *testing.T) {
	got := sanitizeOSCText("build;done")
	if got != "build,done" {
		t.Fatalf("sanitizeOSCText() = %q, want %q", got, "build,done")
	}
}
