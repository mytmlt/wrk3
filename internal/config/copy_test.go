package config

import "testing"

func TestValidateGitCopy(t *testing.T) {
	for _, p := range []string{".env.local", "certs/", "storage/*.sqlite", "data/**/*.txt"} {
		if err := validateGitCopy([]string{p}); err != nil {
			t.Errorf("pattern %q must validate: %v", p, err)
		}
	}
	for _, tc := range []struct {
		name string
		in   []string
	}{
		{"empty", []string{""}},
		{"blank", []string{"  "}},
		{"absolute", []string{"/abs"}},
		{"escape", []string{"../x"}},
		{"escapeNested", []string{"a/../../x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateGitCopy(tc.in); err == nil {
				t.Fatalf("patterns %v must fail validation", tc.in)
			}
		})
	}
}
