# Installation

`wrk3` is a single static Go binary. You need `git` and `docker` on `PATH`
at runtime (docker only for the `docker` runner).

## Option 1 — install script (recommended)

Installs a prebuilt release asset with checksum verification:

```bash
curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash
wrk3 version
```

Pin a version or prefix:

```bash
curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash -s -- --version v0.1.0 --prefix ~/.local
```

Supported: `linux`/`darwin` × `amd64`/`arm64`, plus `windows/amd64`.
If no asset matches your platform, the script falls back to `go install`.

## Option 2 — go install

Requires Go ≥ 1.26:

```bash
go install github.com/mytmlt/wrk3@latest
wrk3 version
```

## Option 3 — from source

```bash
git clone https://github.com/mytmlt/wrk3.git && cd wrk3
make build          # ./bin/wrk3, version-stamped from git
make install        # to /usr/local/bin (override: make install PREFIX=~/.local)
wrk3 version
```

Uninstall: `make uninstall` (or `rm "$(command -v wrk3)"`).

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
