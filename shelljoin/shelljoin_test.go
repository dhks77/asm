package shelljoin

import "testing"

func TestJoin(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "simple args",
			args: []string{"asm", "--picker", "--restore-last"},
			want: "'asm' '--picker' '--restore-last'",
		},
		{
			name: "env args with spaces and quotes",
			args: []string{"env", "ASM_CONTEXT_PATH=/tmp/my repo/it's fine", "asm", "--launcher"},
			want: "'env' 'ASM_CONTEXT_PATH=/tmp/my repo/it'\"'\"'s fine' 'asm' '--launcher'",
		},
		{
			name: "empty arg",
			args: []string{"asm", "--delete-task", ""},
			want: "'asm' '--delete-task' ''",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Join(tc.args...); got != tc.want {
				t.Fatalf("Join() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJoinCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    string
	}{
		{
			name:    "bare command stays unquoted for alias expansion",
			command: "claude",
			args:    []string{"--continue"},
			want:    "claude '--continue'",
		},
		{
			name:    "tilde command stays unquoted",
			command: "~/bin/claude",
			args:    []string{"--continue"},
			want:    "~/bin/claude '--continue'",
		},
		{
			name:    "command with spaces is quoted",
			command: "/Applications/Claude Code/bin/claude",
			args:    []string{"--continue"},
			want:    "'/Applications/Claude Code/bin/claude' '--continue'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := JoinCommand(tc.command, tc.args...); got != tc.want {
				t.Fatalf("JoinCommand() = %q, want %q", got, tc.want)
			}
		})
	}
}
