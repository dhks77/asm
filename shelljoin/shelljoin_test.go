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
