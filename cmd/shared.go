package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/runner"
	"github.com/mytmlt/wrk3/internal/shared"
)

var sharedFollow bool

var sharedCmd = &cobra.Command{
	Use:   "shared",
	Short: "manage shared services (up/down/status/logs)",
	Long: `Manage the shared-services compose project (databases, brokers).

Shared services run once per repo in a fixed project while each
worktree runs only its app services. Per-worktree up/down never
touch the shared project: up ensures it is running first, down
leaves it alone. Only 'shared down' stops it (containers and
networks removed, named volumes preserved).`,
	Args: cobra.NoArgs,
}

var sharedUpCmd = &cobra.Command{
	Use:   "up",
	Short: "start shared services (idempotent)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		if !r.cfg.HasShared() {
			return fmt.Errorf("no shared services configured (see docs/CONFIGURATION.md#shared-services)")
		}
		out := cmd.OutOrStdout()
		logf := func(format string, a ...any) {
			_, _ = fmt.Fprintf(out, format+"\n", a...)
		}
		r.logOpStart("shared-up", "shared up")
		err = ensureSharedUp(cmd.Context(), r, logf)
		r.logOpDone("shared-up", "shared up", err)
		return err
	},
}

var sharedDownCmd = &cobra.Command{
	Use:   "down",
	Short: "stop shared services (volumes preserved)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		if !r.cfg.HasShared() {
			return fmt.Errorf("no shared services configured (see docs/CONFIGURATION.md#shared-services)")
		}
		rn, err := r.sharedRunner()
		if err != nil {
			return err
		}
		project := r.cfg.SharedProject()
		out := cmd.OutOrStdout()
		r.logOpStart("shared-down", "shared down")
		_, _ = fmt.Fprintf(out, "[shared] compose down (%s)\n", project)
		err = rn.Down(cmd.Context(), r.cfg.RepoPath(), sharedBaseEnv(r.cfg))
		if err != nil {
			err = fmt.Errorf("shared down: %w", err)
		} else {
			_, _ = fmt.Fprintf(out, "[shared] down\n")
		}
		r.logOpDone("shared-down", "shared down", err)
		return err
	},
}

var sharedStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "show shared services status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		if !r.cfg.HasShared() {
			return fmt.Errorf("no shared services configured (see docs/CONFIGURATION.md#shared-services)")
		}
		state, detail := probeSharedStatus(cmd.Context(), r)
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), sharedStatusLine(r.cfg, state, detail))
		return nil
	},
}

var sharedLogsCmd = &cobra.Command{
	Use:   "logs [--follow]",
	Short: "show shared services logs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		if !r.cfg.HasShared() {
			return fmt.Errorf("no shared services configured (see docs/CONFIGURATION.md#shared-services)")
		}
		rn, err := r.sharedRunner()
		if err != nil {
			return err
		}
		out, err := rn.Logs(cmd.Context(), r.cfg.RepoPath(), sharedFollow)
		if err != nil {
			return fmt.Errorf("shared logs: %w", err)
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), out); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	},
}

// sharedRunner builds the Runner backend for the shared project and
// decorates it with system logging (branch/slug empty: shared ops
// belong to the repo, not a worktree).
func (r *resolved) sharedRunner() (runner.Runner, error) {
	raw, err := newSharedRunner(r.cfg)
	if err != nil {
		return nil, err
	}
	return &loggedRunner{
		inner:   raw,
		stateP:  r.stateP,
		origin:  r.logSource,
		project: r.cfg.SharedProject(),
	}, nil
}

// newSharedRunner builds the raw Runner backend for cfg's shared
// project via the registry (interface-only, like newRunner).
func newSharedRunner(cfg *config.Config) (runner.Runner, error) {
	f, err := runner.Resolve(cfg.Runner.Type)
	if err != nil {
		return nil, err
	}
	return f(cfg.SharedRunnerOptions()), nil
}

// sharedBaseEnv is the process env for shared compose operations:
// just the project marker (shared services take no per-worktree
// allocation; COMPOSE_PROJECT_NAME is forced by the runner).
func sharedBaseEnv(cfg *config.Config) map[string]string {
	return map[string]string{"WRK3_SHARED_PROJECT": cfg.SharedProject()}
}

// ensureSharedUp brings the shared compose project up idempotently
// for worktree up and `shared up`: preflight, shared overlay write,
// compose up on the shared service scope, then a TCP readiness wait
// on published host ports. Concurrent callers serialize on the
// overlay-dir compose lock (callers may hold a worktree lock: the
// global order is always worktree-then-shared).
func ensureSharedUp(ctx context.Context, r *resolved, logf func(string, ...any)) error {
	cfg := r.cfg
	project := cfg.SharedProject()
	overlayDir := shared.OverlayDir(cfg.AbsWorktreeBase())
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		return fmt.Errorf("shared services: create overlay dir: %w", err)
	}
	lock, err := runner.LockCompose(ctx, overlayDir)
	if err != nil {
		return fmt.Errorf("shared services: %w", err)
	}
	defer func() { _ = runner.UnlockCompose(lock) }()
	be := cfg.SharedBackend()
	if err := runner.CheckComposeFiles(cfg.RepoPath(), be.ComposeFiles); err != nil {
		return fmt.Errorf("shared services: %w", err)
	}
	raw, err := shared.SharedOverlay(cfg.SharedServicePorts())
	if err != nil {
		return fmt.Errorf("shared services: %w", err)
	}
	if err := shared.WriteFile(cfg.SharedOverlayPath(), raw); err != nil {
		return fmt.Errorf("shared services: %w", err)
	}
	rn, err := r.sharedRunner()
	if err != nil {
		return fmt.Errorf("shared services: %w", err)
	}
	env := sharedBaseEnv(cfg)
	logf("[shared] compose up (%s: %s)", project, strings.Join(cfg.SharedServiceNames(), ","))
	if err := rn.Up(ctx, cfg.RepoPath(), env); err != nil {
		return fmt.Errorf("shared services compose: %w", err)
	}
	if ports := cfg.SharedHostPorts(); len(ports) > 0 {
		logf("[shared] waiting for ports %v", ports)
		if err := shared.WaitReady(ports); err != nil {
			return fmt.Errorf("shared services: %w", err)
		}
	}
	logf("[shared] up")
	return nil
}

// writeWorktreeOverlay renders the per-slug worktree overlay
// (shared env overrides on the worktree service scope) so the
// per-worktree compose project reaches shared services without
// editing the base compose file.
func writeWorktreeOverlay(cfg *config.Config, slug string) error {
	raw, err := shared.WorktreeOverlay(cfg.Shared.WorktreeServices, cfg.ExpandSharedEnv(slug))
	if err != nil {
		return fmt.Errorf("worktree overlay: %w", err)
	}
	if err := shared.WriteFile(cfg.WorktreeOverlayPath(slug), raw); err != nil {
		return fmt.Errorf("worktree overlay: %w", err)
	}
	return nil
}

// probeSharedStatus reports the shared project's live state without
// failing: probe errors surface as unknown with the error as detail.
// The probe gets a 30s budget like worktree live probes so a wedged
// daemon degrades the status line instead of hanging the command.
func probeSharedStatus(ctx context.Context, r *resolved) (state, detail string) {
	rn, err := r.sharedRunner()
	if err != nil {
		return "unknown", err.Error()
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	st, err := rn.Status(timeoutCtx, r.cfg.RepoPath())
	if err != nil {
		if st.Detail != "" {
			return string(st.State), st.Detail
		}
		return string(st.State), err.Error()
	}
	return string(st.State), st.Detail
}

// sharedStatusLine renders the one-line shared summary for status
// output: `shared: <project> <state> (<detail>) [services: a,b]`.
func sharedStatusLine(cfg *config.Config, state, detail string) string {
	return fmt.Sprintf("shared: %s %s (%s) [services: %s]",
		cfg.SharedProject(), state, detail, strings.Join(cfg.SharedServiceNames(), ","))
}

func init() {
	sharedLogsCmd.Flags().BoolVar(&sharedFollow, "follow", false, "follow logs")
	sharedCmd.AddCommand(sharedUpCmd, sharedDownCmd, sharedStatusCmd, sharedLogsCmd)
	rootCmd.AddCommand(sharedCmd)
}
