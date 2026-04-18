package notification

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
	spawnHelper          = defaultSpawnHelper
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
		enableTMUXPass()
		if err := sendTMUXPassthrough(req.Title, req.Body); err == nil {
			asmlog.Debugf("notification: tmux-passthrough sent session=%q", req.SessionName)
			return
		} else {
			asmlog.Debugf("notification: tmux-passthrough failed session=%q err=%v", req.SessionName, err)
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
		if err := sendCMUXNotification(req.Title, req.Body, req.Provider, info); err == nil {
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
	title = sanitizeOSCText(title)
	body = sanitizeOSCText(body)
	if title == "" {
		title = "ASM"
	}
	if body == "" {
		body = "done"
	}
	_, err := os.Stdout.WriteString("\x1bPtmux;\x1b\x1b]777;notify;" + title + ";" + body + "\x07\x1b\\")
	return err
}

func sanitizeText(s string, maxRunes int) string {
	s = stripANSIEscapes(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		case !utf8.ValidRune(r):
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
