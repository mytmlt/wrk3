package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mytmlt/wrk3/internal/ports"
)

// editorLookPath is a var so tests can stub it without touching PATH.
var editorLookPath = exec.LookPath

// editorCandidates are tried in order when neither $VISUAL nor $EDITOR is set.
var editorCandidates = []string{"nvim", "vim", "nano", "vi"}

// splitEditor splits an editor spec like "code --wait" into binary + args.
func splitEditor(spec string) []string {
	return strings.Fields(strings.TrimSpace(spec))
}

// resolveEditor returns the editor command line (binary + args, without the
// target file): $VISUAL wins, then $EDITOR, then the first installed
// fallback (nvim/vim/nano/vi). It errors when nothing is found so callers
// can still surface the .env path for manual opening.
func resolveEditor() ([]string, error) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			parts := splitEditor(v)
			if len(parts) == 0 {
				continue
			}
			if _, err := editorLookPath(parts[0]); err != nil {
				return nil, fmt.Errorf("open .env with $%s=%q: %w", key, v, err)
			}
			return parts, nil
		}
	}
	for _, name := range editorCandidates {
		if _, err := editorLookPath(name); err == nil {
			return []string{name}, nil
		}
	}
	return nil, fmt.Errorf("open .env: set $VISUAL or $EDITOR (tried %s)", strings.Join(editorCandidates, "/"))
}

// envFilePath returns the absolute .env path for a worktree record.
func envFilePath(absPath string) string {
	return filepath.Join(absPath, ports.EnvFileName)
}

// editorCmdFor builds the *exec.Cmd that opens path in the resolved editor.
// Stdio wiring is left to the caller: the CLI attaches the terminal
// directly, the dashboard hands it to tea.ExecProcess.
func editorCmdFor(path string) (*exec.Cmd, error) {
	parts, err := resolveEditor()
	if err != nil {
		return nil, err
	}
	return exec.Command(parts[0], append(parts[1:], path)...), nil
}

// openEnvInEditor opens path in the user's editor, attached to the terminal.
func openEnvInEditor(path string) error {
	c, err := editorCmdFor(path)
	if err != nil {
		return err
	}
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("open .env %q in editor: %w", path, err)
	}
	return nil
}
