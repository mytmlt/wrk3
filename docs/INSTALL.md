# Installation

`wrk3` is a single static binary — **no Go toolchain required**. You need
`git` and `docker` on `PATH` at runtime (docker only for the `docker`
runner).

## Option 1 — install script (recommended)

Installs a prebuilt release asset with sha256 verification. No sudo needed —
defaults to `~/.local/bin` (available in every new terminal for the current
user once that dir is on `PATH`):

```bash
curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash
wrk3 version
```

If `~/.local/bin` is not on `PATH` yet, the script prints the one-liner for
your shell:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc   # zsh (macOS default)
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc   # bash
fish_add_path $HOME/.local/bin                             # fish
```

Pin a version, pick another dir, or install machine-wide (all users):

```bash
curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash -s -- --version v0.2.0
curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash -s -- --bindir ~/.local/bin --no-verify
curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | sudo bash -s -- --system   # /usr/local/bin
```

Supported: `linux`/`darwin` × `amd64`/`arm64`, plus `windows/amd64`
(no `windows/arm64` asset is built). If no asset matches your platform,
the script errors with a link to the releases page.

## Option 2 — download the asset manually

Pick `wrk3_<version>_<os>_<arch>.{tar.gz,zip}` (no `v` in the filename)
+ `checksums.txt` from
[releases](https://github.com/mytmlt/wrk3/releases), verify, and put
`wrk3` on `PATH`:

```bash
# linux example (tag v0.3.0 -> file wrk3_0.3.0_...)
curl -fsSLO https://github.com/mytmlt/wrk3/releases/download/v0.3.0/wrk3_0.3.0_linux_amd64.tar.gz
curl -fsSLO https://github.com/mytmlt/wrk3/releases/download/v0.3.0/checksums.txt
sha256sum -c <(grep wrk3_0.3.0_linux_amd64.tar.gz checksums.txt)
tar -xzf wrk3_0.3.0_linux_amd64.tar.gz wrk3
install -m 0755 wrk3 ~/.local/bin/wrk3
```

## Option 3 — from source (developers only)

Requires Go ≥ 1.26:

```bash
git clone https://github.com/mytmlt/wrk3.git && cd wrk3
make build          # ./bin/wrk3, version-stamped from git
make install        # to ~/.local/bin, no sudo needed (ensure it is on PATH)
sudo make install PREFIX=/usr/local   # machine-wide instead (all users)
wrk3 version
```

Uninstall: `make uninstall` (or `rm "$(command -v wrk3)"`).

## Updates

`wrk3` checks GitHub for a new release at most once per 24h (cached in
`~/.cache/wrk3/latest-check.json`) and prints to stderr:

```text
A new version of wrk3 is available: v0.3.0 (you have v0.2.0). Run "wrk3 update" to update.
```

The check never blocks commands (2s timeout, silent when offline) and is
skipped for `dev` builds, `version`, `completion`, `update`, and `--help`.
Opt out with `WRK3_NO_UPDATE_CHECK=1`. To update in place (checksum
verified, no Go needed):

```bash
wrk3 update --check        # show latest without installing
wrk3 update                # install latest over the current binary
wrk3 update --version v0.3.0
```

## Shell completions

```bash
make completion
# bash
source completions/wrk3.bash
# zsh
source completions/wrk3.zsh
# fish
cp completions/wrk3.fish ~/.config/fish/completions/
```

## Verify

```bash
wrk3 --help        # lists all commands
wrk3 version
go version             # ≥ 1.26 if building from source
git --version
docker --version
```
