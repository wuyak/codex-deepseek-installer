# Codex DeepSeek Installer

One-command installer for making Codex App show the two DeepSeek models already routed by a `tokenflux` provider. macOS gets the full App picker patch; Linux and Windows currently patch config/catalog only.

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

## Commands

Clone the repo and run the platform entrypoint:

```bash
git clone git@github.com:wuyak/codex-deepseek-installer.git
cd codex-deepseek-installer
./install-macos.sh
```

The installer will patch config/catalog, wait for Codex App to be fully quit, patch the App picker allowlist, verify the result, and reopen Codex.

Linux and Windows entrypoints exist for config/catalog patching only:

```bash
./install-linux.sh
```

```powershell
powershell -ExecutionPolicy Bypass -File .\install-windows.ps1
```

Those two entrypoints run with `--skip-statsig` until the Codex App Local Storage paths are verified on real Linux/Windows machines.

If macOS blocks the downloaded binary, remove quarantine from the cloned directory:

```bash
xattr -dr com.apple.quarantine .
```

Advanced/debug commands:

```bash
go run . install
go run . plan
go run . apply
go run . verify
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
- Codex App must be fully quit before Statsig patching. Use `Cmd+Q`; closing the window is not enough.

The installer intentionally stops instead of creating a new Codex setup:

```text
missing ~/.codex/config.toml       -> fail
model_provider is not tokenflux    -> fail
missing ~/.codex/models_catalog.json -> fail
Codex App still holds LevelDB LOCK  -> fail unless --wait-for-app-exit is set
```

`model_catalog_json` is treated as a safe config pointer. If it is missing or points elsewhere, `apply` rewrites only that one top-level line.

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

```bash
# One-shot install. Quit Codex App with Cmd+Q when prompted.
./install-macos.sh
```

If the installer reports that LevelDB is locked, fully quit Codex App with `Cmd+Q`. Closing the window is not enough. The installer waits by default and does not kill Codex App.

## Build Release Binaries

```bash
./build-release.sh
```

This writes platform binaries and entry scripts into `dist/`.
