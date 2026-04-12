package provider

// State represents the current state of an AI provider session.
type State int

const (
	StateUnknown    State = iota
	StateIdle             // Waiting for user input
	StateBusy             // Generic busy (can't determine detail)
	StateThinking         // AI is thinking/processing
	StateToolUse          // Running a tool
	StateResponding       // Streaming response text
)

// IsBusy returns true if the provider is doing any kind of work.
func (s State) IsBusy() bool {
	return s == StateBusy || s == StateThinking || s == StateToolUse || s == StateResponding
}

// Label returns a display label for the state.
func (s State) Label() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateThinking:
		return "thinking…"
	case StateToolUse:
		return "tool…"
	case StateResponding:
		return "responding…"
	case StateBusy:
		return "busy…"
	default:
		return ""
	}
}
