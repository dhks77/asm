package notification

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nhn/asm/asmlog"
	cmuxnotify "github.com/nhn/asm/notification/cmux"
	osnotify "github.com/nhn/asm/notification/os"
	"github.com/nhn/asm/terminaldetect"
	asmtmux "github.com/nhn/asm/tmux"
)

// Request describes a single notification delivery request.
type Request struct {
	Title       string
	Body        string
	Provider    string
	CMUXHook    string
	SessionName string
}

type helperPayload struct {
	Request Request             `json:"request"`
	Info    terminaldetect.Info `json:"info"`
}

var (
	detectTerminal       = terminaldetect.Detect
	sendCMUXNotification = cmuxnotify.Send
	sendOSNotification   = osnotify.Send
	asmtmuxIsInsideTmux  = asmtmux.IsInsideTmux
	enableTMUXPass       = asmtmux.EnablePassthrough
	sendTMUXPassthrough  = defaultSendTMUXPassthrough
	sendClientTTYNotice  = defaultSendClientTTYNotice
	spawnHelper          = defaultSpawnHelper
	stdoutSupportsTMUX   = defaultStdoutSupportsTMUX
)

// Send sends a desktop notification. Best-effort: delivery failures are
// logged and otherwise ignored.
func Send(title, body string) {
	SendRequest(Request{Title: title, Body: body})
}

// SendRequest sends a desktop notification using the best backend for the
// current terminal environment.
func SendRequest(req Request) {
	req.Title = sanitizeText(req.Title, 96)
	req.Body = sanitizeText(req.Body, 180)
	req.Provider = strings.TrimSpace(req.Provider)
	req.CMUXHook = strings.TrimSpace(req.CMUXHook)
	req.SessionName = strings.TrimSpace(req.SessionName)
	if req.Title == "" && req.Body == "" {
		return
	}
	if req.SessionName == "" {
		req.SessionName = asmtmux.SessionName
	}

	info, err := detectTerminal(req.SessionName)
	if err != nil {
		asmlog.Debugf("notification: terminal-detect failed session=%q err=%v", req.SessionName, err)
	}
	asmlog.Debugf("notification: send session=%q provider=%q kind=%q app=%q title=%q",
		req.SessionName, req.Provider, info.Kind, info.App.BundleID, req.Title)

	if info.Kind == terminaldetect.KindCMUX && asmtmuxIsInsideTmux() {
		if stdoutSupportsTMUX() {
			enableTMUXPass()
			if err := sendTMUXPassthrough(req.Title, req.Body); err == nil {
				asmlog.Debugf("notification: tmux-passthrough sent session=%q", req.SessionName)
				return
			} else {
				asmlog.Debugf("notification: tmux-passthrough failed session=%q err=%v", req.SessionName, err)
			}
		} else {
			asmlog.Debugf("notification: tmux-passthrough skipped session=%q reason=stdout-not-tty", req.SessionName)
		}
		if tty := strings.TrimSpace(info.ClientTTY); tty != "" {
			if err := sendClientTTYNotice(tty, req.Title, req.Body); err == nil {
				asmlog.Debugf("notification: client-tty sent session=%q tty=%q", req.SessionName, tty)
				return
			} else {
				asmlog.Debugf("notification: client-tty failed session=%q tty=%q err=%v", req.SessionName, tty, err)
			}
		}
		if err := spawnHelper(req, info); err == nil {
			asmlog.Debugf("notification: helper spawned session=%q", req.SessionName)
			return
		} else {
			asmlog.Debugf("notification: helper spawn failed session=%q err=%v", req.SessionName, err)
		}
	}

	deliver(req, info)
}

// RunHelper decodes an internal helper payload and delivers the notification
// without performing terminal detection again.
func RunHelper(encoded string) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return err
	}
	var payload helperPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	payload.Request.Title = sanitizeText(payload.Request.Title, 96)
	payload.Request.Body = sanitizeText(payload.Request.Body, 180)
	deliver(payload.Request, payload.Info)
	return nil
}

func deliver(req Request, info terminaldetect.Info) {
	if info.Kind == terminaldetect.KindCMUX {
		if err := sendCMUXNotification(req.Title, req.Body, req.CMUXHook, info); err == nil {
			return
		} else {
			asmlog.Debugf("notification: cmux send failed session=%q err=%v", req.SessionName, err)
		}
	}

	if err := sendOSNotification(req.Title, req.Body, info); err != nil {
		asmlog.Debugf("notification: os send failed session=%q err=%v", req.SessionName, err)
	}
}

func defaultSpawnHelper(req Request, info terminaldetect.Info) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(helperPayload{Request: req, Info: info})
	if err != nil {
		return err
	}
	payload := base64.StdEncoding.EncodeToString(raw)
	cmd := exec.Command("launchctl", "asuser", strconv.Itoa(os.Getuid()), exe, "--notify-helper", payload)
	return cmd.Run()
}

func defaultSendTMUXPassthrough(title, body string) error {
	title = sanitizeText(title, 180)
	body = sanitizeText(body, 180)
	if title == "" {
		title = "ASM"
	}
	if body == "" {
		body = "done"
	}
	_, err := os.Stdout.WriteString(buildTMUXPassthroughNotification(title, body))
	return err
}

func defaultSendClientTTYNotice(ttyPath, title, body string) error {
	ttyPath = strings.TrimSpace(ttyPath)
	if ttyPath == "" {
		return os.ErrInvalid
	}
	title = sanitizeText(title, 180)
	body = sanitizeText(body, 180)
	if title == "" {
		title = "ASM"
	}
	if body == "" {
		body = "done"
	}
	seq := buildDirectNotification(title, body)
	f, err := os.OpenFile(ttyPath, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(seq)
	return err
}

func defaultStdoutSupportsTMUX() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func buildTMUXPassthroughNotification(title, body string) string {
	return wrapTMUXPassthrough(buildDirectNotification(title, body))
}

func buildDirectNotification(title, body string) string {
	if containsNonASCII(title) || containsNonASCII(body) {
		return buildOSC99Notification(title, body)
	}
	return buildOSC777Notification(title, body)
}

func buildOSC777Notification(title, body string) string {
	return "\x1b]777;notify;" + sanitizeOSCText(title) + ";" + sanitizeOSCText(body) + "\x07"
}

func buildOSC99Notification(title, body string) string {
	id := "asm." + strconv.FormatInt(time.Now().UnixNano(), 36)
	titlePayload := base64.StdEncoding.EncodeToString([]byte(sanitizeText(title, 180)))
	bodyPayload := base64.StdEncoding.EncodeToString([]byte(sanitizeText(body, 180)))
	return "\x1b]99;i=" + id + ":d=0:e=1;" + titlePayload + "\x1b\\" +
		"\x1b]99;i=" + id + ":e=1:p=body;" + bodyPayload + "\x1b\\"
}

func wrapTMUXPassthrough(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

func sanitizeText(s string, maxRunes int) string {
	s = stripANSIEscapes(s)
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == utf8.RuneError:
			return -1
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, s)
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return s
}

func sanitizeOSCText(s string) string {
	s = sanitizeText(s, 180)
	s = strings.NewReplacer(";", ",", "\x07", "", "\x1b", "", "\x9c", "").Replace(s)
	return s
}

func containsNonASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}

func stripANSIEscapes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			break
		}
		switch s[i+1] {
		case '[':
			i += 2
			for i < len(s) {
				c := s[i]
				if c >= 0x40 && c <= 0x7e {
					break
				}
				i++
			}
		case ']':
			i += 2
			for i < len(s) {
				if s[i] == 0x07 {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return b.String()
}
