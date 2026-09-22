package cmd

import "testing"

func TestPortsCell(t *testing.T) {
	cases := []struct {
		name  string
		ports map[string]int
		want  string
	}{
		{"nil", nil, "-"},
		{"empty", map[string]int{}, "-"},
		{"single app", map[string]int{"app": 8000}, "app=8000"},
		{"multi sorted app first", map[string]int{"web": 3000, "app": 8000, "db": 5432}, "app=8000,db=5432,web=3000"},
		{"no app sorts plain", map[string]int{"web": 3000, "db": 5432}, "db=5432,web=3000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := portsCell(tc.ports); got != tc.want {
				t.Errorf("portsCell(%v) = %q, want %q", tc.ports, got, tc.want)
			}
		})
	}
}
