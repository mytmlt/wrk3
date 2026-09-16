package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellInitCmd = &cobra.Command{
	Use:       "shell-init <bash|zsh|fish|powershell>",
	Short:     "Print shell integration so `wrk3 checkout` cds the calling shell",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Long: `Print a shell wrapper that lets 'wrk3 checkout <branch>' change the
calling shell's directory (a child process cannot cd its parent, so the
wrapper applies the cd after wrk3 exits via the WRK3_DIRECTIVE_CD_FILE
protocol — raw path, never parsed as shell).

One-time setup (pick your shell):

  eval "$(wrk3 shell-init bash)"          # ~/.bashrc
  eval "$(wrk3 shell-init zsh)"           # ~/.zshrc
  wrk3 shell-init fish >> ~/.config/fish/config.fish
  wrk3 shell-init powershell >> $PROFILE

Without this, 'wrk3 checkout' prints the worktree path instead of
changing directories: cd "$(wrk3 checkout <branch>)".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		src, err := shellInitScript(args[0])
		if err != nil {
			return err
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), src); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	},
}

// shellInitScript returns the wrapper for shell, or an error listing the
// supported shells. The wrappers share one protocol: create a temp file,
// export WRK3_DIRECTIVE_CD_FILE at the real binary, and cd to the raw
// path wrk3 writes there (see cmd/checkout.go).
func shellInitScript(shell string) (string, error) {
	switch shell {
	case "bash", "zsh":
		return shellInitBashZsh, nil
	case "fish":
		return shellInitFish, nil
	case "powershell":
		return shellInitPowershell, nil
	default:
		return "", fmt.Errorf("unknown shell %q (want bash|zsh|fish|powershell)", shell)
	}
}

const shellInitBashZsh = `# wrk3 shell integration: lets 'wrk3 checkout <branch>' cd the calling shell.
# Setup: eval "$(wrk3 shell-init bash)" in ~/.bashrc (or shell-init zsh in ~/.zshrc).
wrk3() {
  local cd_file exit_code=0
  cd_file="$(mktemp)" || return 1
  WRK3_DIRECTIVE_CD_FILE="$cd_file" command wrk3 "$@" || exit_code=$?
  if [[ -s "$cd_file" ]]; then
    cd -- "$(<"$cd_file")" || exit_code=$?
  fi
  rm -f "$cd_file"
  return "$exit_code"
}
`

const shellInitFish = `# wrk3 shell integration: lets 'wrk3 checkout <branch>' cd the calling shell.
# Setup: wrk3 shell-init fish >> ~/.config/fish/config.fish
function wrk3
  set -l cd_file (mktemp); or return 1
  env WRK3_DIRECTIVE_CD_FILE="$cd_file" command wrk3 $argv; set -l exit_code $status
  if test -s "$cd_file"
    cd -- (cat "$cd_file"); set exit_code $status
  end
  rm -f "$cd_file"
  return $exit_code
end
`

const shellInitPowershell = `# wrk3 shell integration: lets 'wrk3 checkout <branch>' cd the calling shell.
# Setup: wrk3 shell-init powershell >> $PROFILE
function wrk3 {
  $cdFile = New-TemporaryFile
  try {
    $wtBin = (Get-Command wrk3 -CommandType Application | Select-Object -First 1 -ExpandProperty Source)
    $env:WRK3_DIRECTIVE_CD_FILE = $cdFile.FullName
    & $wtBin @args
    $exitCode = $LASTEXITCODE
    if ((Test-Path $cdFile.FullName) -and ((Get-Item $cdFile.FullName).Length -gt 0)) {
      Set-Location -LiteralPath ((Get-Content -Raw $cdFile.FullName).Trim())
      if (-not $?) { $exitCode = 1 }
    }
    $global:LASTEXITCODE = $exitCode
  } finally {
    Remove-Item $cdFile.FullName -Force -ErrorAction SilentlyContinue
    Remove-Item Env:WRK3_DIRECTIVE_CD_FILE -ErrorAction SilentlyContinue
  }
}
`

func init() {
	rootCmd.AddCommand(shellInitCmd)
}
