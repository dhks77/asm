# Notification Redesign Notes

Date: 2026-04-19

## Goal

Redo notification delivery from a clean state while preserving the findings from the current experiments.

## What Was Learned

- There are two separate concerns:
  - notification delivery
  - click-to-focus behavior
- Treat them separately in the redesign.
- In this environment, `cmux` native notifications can carry the user back to the correct workspace/tab.
- `osascript display notification ...` was not a reliable baseline here.
  - Sometimes no visible banner appeared.
  - When it did appear, click handling could end up at Script Editor instead of the target terminal.
- A screenshot alone was not enough to prove whether a visible banner came from `cmux` native delivery or an OS helper.
- `cmux list-notifications` was the reliable source of truth for whether a native `cmux` notification was created.

## How To Decide "Is This cmux?"

Current discovery approach that worked:

1. Find the most recently active attached tmux client.
   - `tmux list-clients -t <session> -F "#{client_tty}\t#{client_pid}\t#{client_activity}"`
   - Pick the row with the highest `client_activity`.
2. Inspect that client process environment.
   - `ps eww -p <client-pid> -o command=`
3. Parse `CMUX_*` variables from that command string.
4. If `CMUX_WORKSPACE_ID` exists, treat the terminal as `cmux`.

Relevant `CMUX_*` variables observed:

- `CMUX_WORKSPACE_ID`
- `CMUX_SURFACE_ID`
- `CMUX_TAB_ID`
- `CMUX_PANEL_ID`
- `CMUX_SOCKET_PATH`
- `CMUX_SOCKET`
- `CMUX_BUNDLED_CLI_PATH`
- `CMUX_BUNDLE_ID`

Practical rule:

- `CMUX_WORKSPACE_ID` present => `cmux`
- no `CMUX_WORKSPACE_ID` => not enough evidence to use the `cmux` backend

## Native cmux Notification Calls

Two command shapes were tested.

### 1. Claude-style native path

```bash
printf '%s' '{"message":"..."}' | \
  /Applications/cmux.app/Contents/Resources/bin/cmux \
  claude-hook notification \
  --workspace <workspace-id> \
  --surface <surface-id>
```

Observed behavior:

- This created entries visible in `cmux list-notifications`.
- In successful cases this matched the built-in `Claude Code / Attention / <message>` style.
- This path is the best candidate when the provider is Claude.

### 2. Generic native path

```bash
/Applications/cmux.app/Contents/Resources/bin/cmux \
  notify \
  --title "Claude Code" \
  --subtitle "Attention" \
  --body "..." \
  --workspace <workspace-id> \
  --surface <surface-id>
```

Observed behavior:

- This also created entries in `cmux list-notifications`.
- In this setup, native creation alone did not consistently prove that a visible macOS banner appeared.

## Environment Constraints For cmux Calls

This mattered a lot.

- Calling `cmux claude-hook notification` with a minimal environment worked.
- Calling the same thing from a picker process that inherited a larger tmux/cmux environment sometimes failed with:

```text
Broken pipe
```

Useful minimal environment for repro:

- `HOME`
- `PATH`
- `LANG`
- `TMPDIR`
- `USER`
- `LOGNAME`
- `SHELL`
- required `CMUX_*` values

Risky inherited state:

- `TMUX`
- tmux-specific terminal env
- extra inherited terminal/session state that was not required for notification creation

Design implication:

- If invoking `cmux` from asm, prefer a deliberately constructed environment instead of passing through the full picker environment.

## Observability That Helped

These were the most useful checks:

### Was a native cmux notification created?

```bash
/Applications/cmux.app/Contents/Resources/bin/cmux list-notifications
```

### Which terminal app/session is the active tmux client attached through?

```bash
tmux list-clients -t <session> -F "#{client_tty}\t#{client_pid}\t#{client_activity}"
ps eww -p <client-pid> -o command=
```

### What did asm think the focus target was?

Look for:

- `notification-focus-target` debug logs
- adapter
- parsed `CMUX_*` metadata

## Redesign Recommendations

Start simple:

1. Notification backend selection
   - if `cmux` is detected, use native `cmux`
   - otherwise use plain OS notification
2. Click-to-focus
   - do not mix this into the first pass unless necessary
   - native `cmux` may already be sufficient
3. Keep a strict separation between:
   - terminal-native delivery backends
   - OS fallback delivery
   - optional click/focus helpers

## Suggested Clean Architecture

- `notification`
  - high-level send request
  - backend selection
- `notification/cmux`
  - native `cmux` delivery only
  - no OS helper logic
- `notification/os`
  - generic OS fallback only
- `terminaldetect`
  - detect terminal type and capture terminal metadata

## Things To Re-verify After Rebuild

- Does `cmux` native delivery alone produce the desired banner in the user’s real workflow?
- Is `claude-hook notification` more reliable than `notify` for Claude sessions?
- Is `surface_id` required for the user-visible behavior, or is `workspace_id` alone enough?
- Can click-to-focus logic be removed entirely when running under `cmux`?
