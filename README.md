# Codex DeepSeek Installer

One-command setup and repeatable repair tool for making Codex App show the two DeepSeek models already routed by a `tokenflux` provider.

macOS patches config/catalog and performs a one-time local App picker injection. Linux and Windows currently patch config/catalog only.

It only patches three local states:

```text
~/.codex/config.toml
  ensure model_catalog_json points to ~/.codex/models_catalog.json

~/.codex/models_catalog.json
  upsert full DeepSeek model objects

~/Library/Application Support/Codex/Local Storage/leveldb
  upsert DeepSeek slugs into Statsig available_models
```

It does not configure upstream routing, `tokenflux`, provider credentials, or Codex auth. It also does not create a new Codex setup: the machine must already have a working Codex config whose top-level provider is `tokenflux`.

## Important: Picker Injection Is Repairable, Not Permanent

Codex App can refresh its local Statsig cache from the network during app startup, after reboot, or after app updates. When that happens, the local dynamic config can be rewritten back to the official `available_models` list, and the two DeepSeek models can disappear from the App picker even though `~/.codex/config.toml` and `~/.codex/models_catalog.json` are still correct.

This tool treats that as a normal repair case:

```bash
# Read-only diagnosis. This can inspect a temporary LevelDB snapshot while Codex App is open.
./codex-deepseek-installer status

# Strict diagnosis for scripts/CI. Exits non-zero if repair is needed.
./codex-deepseek-installer doctor

# Re-inject config/catalog/Statsig. Quit Codex App with Cmd+Q when prompted.
./codex-deepseek-installer repair --wait-for-app-exit --open-after
```

The public repo does not ship or require a private golden LevelDB cache. It patches the current local Statsig `available_models` entry in place after backing it up.

## Commands

Download the latest release for your platform:

- macOS Apple Silicon: `codex-deepseek-installer-*-macos-arm64.tar.gz`
- macOS Intel: `codex-deepseek-installer-*-macos-amd64.tar.gz`
- Linux ARM64: `codex-deepseek-installer-*-linux-arm64.tar.gz`
- Linux AMD64: `codex-deepseek-installer-*-linux-amd64.tar.gz`
- Windows ARM64: `codex-deepseek-installer-*-windows-arm64.zip`
- Windows AMD64: `codex-deepseek-installer-*-windows-amd64.zip`

macOS example:

```bash
VERSION=v0.1.2
curl -L -o codex-deepseek-installer-macos-arm64.tar.gz \
  "https://github.com/wuyak/codex-deepseek-installer/releases/download/${VERSION}/codex-deepseek-installer-${VERSION}-macos-arm64.tar.gz"
tar -xzf codex-deepseek-installer-macos-arm64.tar.gz
./install-macos.sh
```

The installer patches config/catalog, waits for Codex App to be fully quit, performs the one-time App picker injection, verifies the result, and reopens Codex.

Linux and Windows entrypoints exist for config/catalog patching only:

```bash
./install-linux.sh
```

```powershell
powershell -ExecutionPolicy Bypass -File .\install-windows.ps1
```

Those two entrypoints run with `--skip-statsig` until the Codex App Local Storage paths are verified on real Linux/Windows machines.

If macOS blocks the downloaded binary, remove quarantine from the extracted directory:

```bash
xattr -dr com.apple.quarantine .
```

Developer clone:

```bash
git clone https://github.com/wuyak/codex-deepseek-installer.git
cd codex-deepseek-installer
go run . plan --skip-statsig
```

SSH clone for maintainers:

```bash
git clone git@github.com:wuyak/codex-deepseek-installer.git
```

Advanced/debug commands from a source checkout:

```bash
go run . install
go run . plan
go run . apply
go run . verify
go run . status
go run . doctor
go run . repair --wait-for-app-exit --open-after
```

Useful flags:

```text
--codex-home <path>
--catalog-path <path>
--statsig-path <path>
--wait-for-app-exit
--skip-statsig
--open-after
--timeout 5m
```

## Requirements

- macOS for the full chain.
- Linux/Windows currently patch config/catalog only.
- Existing `~/.codex/config.toml`.
- Top-level `model_provider = "tokenflux"`.
- Existing `~/.codex/models_catalog.json`.
- Codex App must be fully quit before Statsig patching or repair. Use `Cmd+Q`; closing the window is not enough.
- The installer does not create or configure `tokenflux`; it only verifies that `tokenflux` is already the active top-level provider.

The installer intentionally stops instead of creating a new Codex setup:

```text
missing ~/.codex/config.toml       -> fail
model_provider is not tokenflux    -> fail
missing ~/.codex/models_catalog.json -> fail
Codex App still holds LevelDB LOCK  -> fail unless --wait-for-app-exit is set
```

`model_catalog_json` is treated as a safe config pointer. If it is missing or points elsewhere, `apply`/`repair` rewrites only that one top-level line.

## What Gets Added

The model slugs must match across catalog and Statsig:

```text
deepseek-v4-flash(deepseek)
deepseek-v4-pro(deepseek)
```

The catalog entries are not bare model shells. They keep the Codex prompt/tool metadata shape required by the App and CLI, including:

```text
base_instructions
model_messages.instructions_template
model_messages.instructions_variables
reasoning levels
tool metadata
context window
```

## Safe Flow

Initial setup:

```bash
# Quit Codex App with Cmd+Q when prompted.
./install-macos.sh
```

Check after reboot/update/startup:

```bash
./codex-deepseek-installer status
```

If DeepSeek disappears from the Codex App picker:

```bash
./codex-deepseek-installer repair --wait-for-app-exit --open-after
```

If the installer reports that LevelDB is locked during repair, fully quit Codex App with `Cmd+Q`. Closing the window is not enough. The installer waits only when `--wait-for-app-exit` is set and never kills Codex App.

## Build Release Binaries

```bash
./build-release.sh
```

This writes platform binaries, platform archives, and `SHA256SUMS` into `dist/`. The source repository intentionally does not track `dist/`; release binaries should be uploaded as GitHub Release assets.

## Maintainer Release Flow

Releases are built by GitHub Actions when a version tag is pushed:

```bash
git tag v0.1.2
git push origin v0.1.2
```

The release workflow runs tests, builds all platform archives, generates `SHA256SUMS`, creates the GitHub Release, and uploads the assets automatically.
