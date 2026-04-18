package terminaldetect

import "testing"

func TestDetectCMUXUsesMostRecentClient(t *testing.T) {
	d := detector{
		listClients: func(sessionName string) ([]client, error) {
			if sessionName != "asm-default" {
				t.Fatalf("session = %q, want %q", sessionName, "asm-default")
			}
			return []client{
				{PID: 100, Activity: 10},
				{PID: 200, Activity: 20},
			}, nil
		},
		inspectCommand: func(pid int) (string, error) {
			if pid != 200 {
				t.Fatalf("pid = %d, want %d", pid, 200)
			}
			return "tmux attach-session CMUX_WORKSPACE_ID=workspace:2 CMUX_SURFACE_ID=surface:7 CMUX_BUNDLED_CLI_PATH=/tmp/cmux CMUX_BUNDLE_ID=dev.cmux", nil
		},
	}

	info, err := d.detect("asm-default")
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if info.Kind != KindCMUX {
		t.Fatalf("kind = %q, want %q", info.Kind, KindCMUX)
	}
	if info.ClientPID != 200 {
		t.Fatalf("client pid = %d, want %d", info.ClientPID, 200)
	}
	if info.CMUX == nil || info.CMUX.WorkspaceID != "workspace:2" || info.CMUX.SurfaceID != "surface:7" {
		t.Fatalf("cmux metadata = %#v, want workspace/surface populated", info.CMUX)
	}
	if info.App.BundleID != "dev.cmux" {
		t.Fatalf("bundle id = %q, want %q", info.App.BundleID, "dev.cmux")
	}
}

func TestDetectITermFromEnvironment(t *testing.T) {
	d := detector{
		listClients: func(sessionName string) ([]client, error) {
			return []client{{PID: 321, Activity: 1}}, nil
		},
		inspectCommand: func(pid int) (string, error) {
			return "tmux attach TERM_PROGRAM=iTerm.app ITERM_SESSION_ID=w0t0p0 __CFBundleIdentifier=com.googlecode.iterm2", nil
		},
	}

	info, err := d.detect("asm-default")
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if info.Kind != KindITerm {
		t.Fatalf("kind = %q, want %q", info.Kind, KindITerm)
	}
	if info.App.BundleID != "com.googlecode.iterm2" {
		t.Fatalf("bundle id = %q, want %q", info.App.BundleID, "com.googlecode.iterm2")
	}
}

func TestDetectITermWinsOverInheritedCMUXEnvironment(t *testing.T) {
	d := detector{
		listClients: func(sessionName string) ([]client, error) {
			return []client{{PID: 321, Activity: 1}}, nil
		},
		inspectCommand: func(pid int) (string, error) {
			return "tmux attach TERM_PROGRAM=iTerm.app ITERM_SESSION_ID=w0t0p0 __CFBundleIdentifier=com.googlecode.iterm2 CMUX_WORKSPACE_ID=workspace:1 CMUX_BUNDLE_ID=com.cmuxterm.app", nil
		},
	}

	info, err := d.detect("asm-default")
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if info.Kind != KindITerm {
		t.Fatalf("kind = %q, want %q", info.Kind, KindITerm)
	}
	if info.CMUX != nil {
		t.Fatalf("cmux metadata = %#v, want nil for iTerm client", info.CMUX)
	}
	if info.App.BundleID != "com.googlecode.iterm2" {
		t.Fatalf("bundle id = %q, want %q", info.App.BundleID, "com.googlecode.iterm2")
	}
}

func TestFindEnvValueRequiresVariableBoundary(t *testing.T) {
	command := "FOO_CMUX_WORKSPACE_ID=nope CMUX_WORKSPACE_ID=workspace:3"
	got, ok := findEnvValue(command, "CMUX_WORKSPACE_ID")
	if !ok {
		t.Fatalf("findEnvValue() did not find variable")
	}
	if got != "workspace:3" {
		t.Fatalf("findEnvValue() = %q, want %q", got, "workspace:3")
	}
}

func TestFindEnvValuePreservesSpacesInValue(t *testing.T) {
	command := "tmux attach-session CMUX_SOCKET_PATH=/Users/nhn/Library/Application Support/cmux/cmux.sock DISPLAY=/tmp/display"
	got, ok := findEnvValue(command, "CMUX_SOCKET_PATH")
	if !ok {
		t.Fatal("findEnvValue() did not find variable")
	}
	want := "/Users/nhn/Library/Application Support/cmux/cmux.sock"
	if got != want {
		t.Fatalf("findEnvValue() = %q, want %q", got, want)
	}
}
