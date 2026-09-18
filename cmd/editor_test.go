package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withEnv(t *testing.T, key, val string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if val == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, val)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestResolveEditor_VisualWinsOverEditor(t *testing.T) {
	withEnv(t, "VISUAL", "my-vis --wait")
	withEnv(t, "EDITOR", "my-ed")
	old := editorLookPath
	editorLookPath = func(name string) (string, error) { return "/bin/" + name, nil }
	defer func() { editorLookPath = old }()

	got, err := resolveEditor()
	if err != nil {
		t.Fatalf("resolveEditor = %v", err)
	}
	if len(got) != 2 || got[0] != "my-vis" || got[1] != "--wait" {
		t.Errorf("got %v, want [my-vis --wait]", got)
	}
}

func TestResolveEditor_EditorWithArgs(t *testing.T) {
	withEnv(t, "VISUAL", "")
	withEnv(t, "EDITOR", "code --wait --new-window")
	old := editorLookPath
	editorLookPath = func(name string) (string, error) { return "/bin/" + name, nil }
	defer func() { editorLookPath = old }()

	got, err := resolveEditor()
	if err != nil {
		t.Fatalf("resolveEditor = %v", err)
	}
	if len(got) != 3 || got[0] != "code" {
		t.Errorf("got %v, want code + 2 args", got)
	}
}

func TestResolveEditor_FallbackAndMissing(t *testing.T) {
	withEnv(t, "VISUAL", "")
	withEnv(t, "EDITOR", "")
	old := editorLookPath
	defer func() { editorLookPath = old }()

	editorLookPath = func(name string) (string, error) {
		if name == "nano" {
			return "/usr/bin/nano", nil
		}
		return "", os.ErrNotExist
	}
	got, err := resolveEditor()
	if err != nil {
		t.Fatalf("resolveEditor = %v", err)
	}
	if len(got) != 1 || got[0] != "nano" {
		t.Errorf("got %v, want [nano]", got)
	}

	editorLookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	if _, err := resolveEditor(); err == nil || !strings.Contains(err.Error(), "$VISUAL") {
		t.Errorf("missing editor err = %v, want $VISUAL hint", err)
	}
}

func TestEnvFilePath_JoinsDotEnv(t *testing.T) {
	got := envFilePath(filepath.Join("a", "b"))
	if !strings.HasSuffix(got, string(filepath.Separator)+".env") {
		t.Errorf("got %q, want trailing .env", got)
	}
}
