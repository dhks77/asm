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
			args: []string{"asm", "--picker", "--path", "/tmp/repo"},
			want: "'asm' '--picker' '--path' '/tmp/repo'",
		},
		{
			name: "spaces and quotes",
			args: []string{"asm", "--path", "/tmp/my repo/it's fine"},
			want: "'asm' '--path' '/tmp/my repo/it'\"'\"'s fine'",
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
