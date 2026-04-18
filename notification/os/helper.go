package osnotify

import (
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nhn/asm/config"
	"github.com/nhn/asm/terminaldetect"
)

const (
	helperVersion         = "3"
	helperBinaryName      = "asm-notifier"
	helperLifetimeSeconds = 180
	helperBuildTimeout    = 30 * time.Second
	helperQuitGraceMillis = 1500
)

var userConfigDir = config.UserConfigDir

type helperSpec struct {
	AppPath        string
	BuildRoot      string
	ExecutablePath string
	InfoPlistPath  string
	BundleID       string
	DisplayName    string
	TargetBundleID string
}

func sendHelperNotification(title, body string, app terminaldetect.App) error {
	spec, err := ensureHelperApp(app)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return runCommand(ctx, "open", []string{"-n", "-g", spec.AppPath, "--args", title, body})
}

func ensureHelperApp(app terminaldetect.App) (helperSpec, error) {
	spec := newHelperSpec(app)
	if helperIsCurrent(spec) {
		return spec, nil
	}
	if err := buildHelperApp(spec); err != nil {
		return helperSpec{}, err
	}
	return spec, nil
}

func newHelperSpec(app terminaldetect.App) helperSpec {
	targetBundleID := strings.TrimSpace(app.BundleID)
	hash := sha1.Sum([]byte(targetBundleID))
	slug := sanitizeSlug(targetBundleID)
	if slug == "" {
		slug = "app"
	}
	slug = fmt.Sprintf("%s-%x", slug, hash[:6])

	root := filepath.Join(userConfigDir(), "notification-helper", slug)
	appPath := filepath.Join(root, "ASMNotifier.app")
	displayName := helperDisplayName(app)
	return helperSpec{
		AppPath:        appPath,
		BuildRoot:      root,
		ExecutablePath: filepath.Join(appPath, "Contents", "MacOS", helperBinaryName),
		InfoPlistPath:  filepath.Join(appPath, "Contents", "Info.plist"),
		BundleID:       fmt.Sprintf("com.github.nhn.asm.notifier.%x", hash[:6]),
		DisplayName:    displayName,
		TargetBundleID: targetBundleID,
	}
}

func helperDisplayName(app terminaldetect.App) string {
	label := strings.TrimSpace(app.Name)
	if label == "" {
		parts := strings.Split(strings.TrimSpace(app.BundleID), ".")
		label = parts[len(parts)-1]
	}
	label = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '-':
			return r
		default:
			return -1
		}
	}, label)
	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		label = "App"
	}
	return "ASM " + label + " Notifier"
}

func sanitizeSlug(value string) string {
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, strings.TrimSpace(value))
	value = strings.Trim(value, "-")
	value = strings.Join(strings.FieldsFunc(value, func(r rune) bool { return r == '-' }), "-")
	return value
}

func helperIsCurrent(spec helperSpec) bool {
	if _, err := os.Stat(spec.ExecutablePath); err != nil {
		return false
	}
	data, err := os.ReadFile(spec.InfoPlistPath)
	if err != nil {
		return false
	}
	return string(data) == renderInfoPlist(spec)
}

func buildHelperApp(spec helperSpec) error {
	if err := os.MkdirAll(spec.BuildRoot, 0o755); err != nil {
		return err
	}

	buildDir, err := os.MkdirTemp(spec.BuildRoot, ".build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(buildDir)

	tmpAppPath := filepath.Join(buildDir, "ASMNotifier.app")
	tmpExecutable := filepath.Join(tmpAppPath, "Contents", "MacOS", helperBinaryName)
	tmpInfoPlist := filepath.Join(tmpAppPath, "Contents", "Info.plist")
	if err := os.MkdirAll(filepath.Dir(tmpExecutable), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmpInfoPlist, []byte(renderInfoPlist(spec)), 0o644); err != nil {
		return err
	}

	sourcePath := filepath.Join(buildDir, "main.swift")
	if err := os.WriteFile(sourcePath, []byte(renderSwiftSource(spec)), 0o644); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), helperBuildTimeout)
	defer cancel()
	if err := runCommand(ctx, "xcrun", []string{
		"swiftc",
		"-o", tmpExecutable,
		sourcePath,
		"-framework", "AppKit",
		"-framework", "UserNotifications",
	}); err != nil {
		return err
	}
	if err := runCommand(ctx, "codesign", []string{
		"--force",
		"--deep",
		"--sign", "-",
		"--identifier", spec.BundleID,
		tmpAppPath,
	}); err != nil {
		return err
	}

	if err := os.RemoveAll(spec.AppPath); err != nil {
		return err
	}
	return os.Rename(tmpAppPath, spec.AppPath)
}

func renderInfoPlist(spec helperSpec) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>ASMNotifierVersion</key>
	<string>%s</string>
	<key>ASMTargetBundleID</key>
	<string>%s</string>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleDisplayName</key>
	<string>%s</string>
	<key>CFBundleExecutable</key>
	<string>%s</string>
	<key>CFBundleIdentifier</key>
	<string>%s</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>%s</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>1.0</string>
	<key>CFBundleVersion</key>
	<string>1</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSPrincipalClass</key>
	<string>NSApplication</string>
</dict>
</plist>
`, plistEscape(helperVersion), plistEscape(spec.TargetBundleID), plistEscape(spec.DisplayName), plistEscape(helperBinaryName), plistEscape(spec.BundleID), plistEscape(spec.DisplayName))
}

func renderSwiftSource(spec helperSpec) string {
	return fmt.Sprintf(`import AppKit
import Foundation
import UserNotifications

final class AppDelegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    private let center = UNUserNotificationCenter.current()
    private let targetBundleID = %q
    private let lifetimeSeconds = %d.0
    private let quitGraceSeconds = %0.1f
    private var timeoutTimer: Timer?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        center.delegate = self

        let args = Array(CommandLine.arguments.dropFirst())
        if args.isEmpty {
            activateTarget()
            terminate(after: quitGraceSeconds)
            return
        }

        let title = args[0]
        let body = args.count > 1 ? args[1] : ""
        center.getNotificationSettings { settings in
            switch settings.authorizationStatus {
            case .authorized, .provisional, .ephemeral:
                self.deliver(title: title, body: body)
            case .notDetermined:
                self.center.requestAuthorization(options: [.alert, .sound]) { granted, _ in
                    if granted {
                        self.deliver(title: title, body: body)
                    } else {
                        self.terminate()
                    }
                }
            default:
                self.terminate()
            }
        }
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification, withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .list])
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler completionHandler: @escaping () -> Void) {
        activateTarget()
        terminate(after: quitGraceSeconds)
        completionHandler()
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        activateTarget()
        terminate(after: quitGraceSeconds)
        return false
    }

    private func deliver(title: String, body: String) {
        let content = UNMutableNotificationContent()
        content.title = title
        if !body.isEmpty {
            content.body = body
        }

        let request = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        center.add(request) { error in
            if error != nil {
                self.terminate()
                return
            }
            DispatchQueue.main.async {
                self.timeoutTimer?.invalidate()
                self.timeoutTimer = Timer.scheduledTimer(withTimeInterval: self.lifetimeSeconds, repeats: false) { _ in
                    self.terminate()
                }
            }
        }
    }

    private func activateTarget() {
        DispatchQueue.main.async {
            if let running = NSRunningApplication.runningApplications(withBundleIdentifier: self.targetBundleID).first {
                running.activate(options: [.activateIgnoringOtherApps])
                return
            }

            guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: self.targetBundleID) else {
                return
            }

            let configuration = NSWorkspace.OpenConfiguration()
            configuration.activates = true
            NSWorkspace.shared.openApplication(at: url, configuration: configuration) { _, _ in
            }
        }
    }

    private func terminate() {
        DispatchQueue.main.async {
            NSApp.terminate(nil)
        }
    }

    private func terminate(after delay: TimeInterval) {
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) {
            NSApp.terminate(nil)
        }
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
`, spec.TargetBundleID, helperLifetimeSeconds, float64(helperQuitGraceMillis)/1000.0)
}

func plistEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(strings.TrimSpace(value))
}
