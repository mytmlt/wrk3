package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerNameServices(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		want  []string
		wantE bool
	}{
		{
			name: "clean",
			raw: `services:
  db:
    image: postgres:16-alpine
  app:
    image: alpine:3.19
`,
			want: nil,
		},
		{
			name: "single offender",
			raw: `services:
  db:
    image: postgres:16-alpine
    container_name: testapp-db
  app:
    image: alpine:3.19
`,
			want: []string{"db"},
		},
		{
			name: "multiple sorted",
			raw: `services:
  frontend:
    container_name: testapp-frontend
  db:
    container_name: testapp-db
  backend:
    image: x
`,
			want: []string{"db", "frontend"},
		},
		{
			name: "empty string ignored",
			raw: `services:
  db:
    container_name: ""
`,
			want: nil,
		},
		{
			name:  "no services",
			raw:   `version: "3"`,
			want:  nil,
			wantE: false,
		},
		{
			name:  "invalid yaml",
			raw:   "services: [unclosed",
			wantE: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := containerNameServices([]byte(c.raw))
			if c.wantE && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !c.wantE && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestCheckComposeFiles(t *testing.T) {
	dir := t.TempDir()
	clean := `services:
  db:
    image: postgres:16-alpine
`
	bad := `services:
  db:
    image: postgres:16-alpine
    container_name: testapp-db
`
	if err := os.WriteFile(filepath.Join(dir, "clean.yml"), []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.yml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CheckComposeFiles(dir, []string{"clean.yml"}); err != nil {
		t.Errorf("clean file should pass: %v", err)
	}
	if err := CheckComposeFiles(dir, []string{"missing.yml"}); err != nil {
		t.Errorf("missing file should be skipped: %v", err)
	}
	if err := CheckComposeFiles(dir, []string{""}); err != nil {
		t.Errorf("empty entry should be skipped: %v", err)
	}
	err := CheckComposeFiles(dir, []string{"clean.yml", "bad.yml"})
	if err == nil {
		t.Fatal("expected error for bad file, got nil")
	}
	for _, want := range []string{"bad.yml", "db", "container_name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}
