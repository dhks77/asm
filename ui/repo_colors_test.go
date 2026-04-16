package ui

import "testing"

func TestGeneratedRepoColorValueStable(t *testing.T) {
	first := generatedRepoColorValue("billing")
	second := generatedRepoColorValue("billing")
	other := generatedRepoColorValue("accounts")

	if first != second {
		t.Fatalf("expected stable generated color, got %q and %q", first, second)
	}
	if len(first) != 7 || first[0] != '#' {
		t.Fatalf("expected generated color to be hex, got %q", first)
	}
	if first == other {
		t.Fatalf("expected different repos to usually get different colors, both were %q", first)
	}
}

func TestNormalizeRepoColorValue(t *testing.T) {
	tests := []struct {
		name  string
		repo  string
		input string
		want  string
		valid bool
	}{
		{name: "auto", repo: "billing", input: "auto", want: generatedRepoColorValue("billing"), valid: true},
		{name: "preset", repo: "billing", input: "sky", want: "#7BC7FF", valid: true},
		{name: "hex", repo: "billing", input: "#7cc8ff", want: "#7CC8FF", valid: true},
		{name: "short hex", repo: "billing", input: "#7cf", want: "#77CCFF", valid: true},
		{name: "rgb", repo: "billing", input: "rgb(124, 199, 255)", want: "#7CC7FF", valid: true},
		{name: "comma triplet", repo: "billing", input: "124,199,255", want: "#7CC7FF", valid: true},
		{name: "ansi", repo: "billing", input: "117", want: "117", valid: true},
		{name: "invalid", repo: "billing", input: "nope", want: "", valid: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeRepoColorValue(tc.repo, tc.input)
			if ok != tc.valid {
				t.Fatalf("expected valid=%v, got %v (%q)", tc.valid, ok, got)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
