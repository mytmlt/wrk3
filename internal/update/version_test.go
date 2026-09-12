package update

import (
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.2.0", "v0.1.0", false},
		{"0.2.0", "v0.2.1", true},
		{"v0.2.0", "v0.10.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"dev", "v9.9.9", false},
		{"", "v1.0.0", false},
		{"v0.2.0", "dev", false},
		{"v1.0.0-rc1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-rc1", false},
		{"garbage", "v1.0.0", false},
		{"v1.0.0", "garbage", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"0.2.0":  "v0.2.0",
		"v0.2.0": "v0.2.0",
		"latest": "latest",
		"":       "",
	}
	for in, want := range cases {
		if got := NormalizeVersion(in); got != want {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
