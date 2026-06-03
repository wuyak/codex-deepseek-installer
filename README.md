# Codex TokenFlux Model Repair for macOS

macOS-only repair tool for making Codex App show these required model picker entries:

```text
gpt-5.3-codex-spark
deepseek-v4-flash
deepseek-v4-pro
human-llm
```

The tool repairs the local Codex-side visibility chain. It does not configure TokenFlux, change upstream model routing, deploy CPA services, store credentials, or modify `/Applications/Codex.app`.

## What It Repairs

Only these local files and caches are touched:

```text
~/.codex/config.toml
  verify model_provider = "tokenflux"
  ensure model_catalog_json points to ~/.codex/models_catalog.json

~/.codex/models_catalog.json
  verify the official gpt-5.3-codex-spark catalog object exists
  upsert full catalog objects for deepseek-v4-flash, deepseek-v4-pro, and human-llm

~/Library/Application Support/Codex/Default/Local Storage/leveldb
  add required picker slugs to Statsig dynamic config 107580212
  pin the local Statsig evaluation cache so app startup is less likely to overwrite it
```

The Statsig patch is repairable, not permanent. Codex App may refresh local frontend cache after startup, app updates, reboots, or network refreshes. When a required model disappears, rerun repair.

## Requirements

- macOS.
- Codex App has been opened at least once.
- `~/.codex/config.toml` exists and already has top-level `model_provider = "tokenflux"`.
- TokenFlux credentials and upstream routes are already configured outside this tool.
- `~/.codex/models_catalog.json` exists and already contains the official `gpt-5.3-codex-spark` object.
- Codex App must fully quit before a write repair can patch LevelDB. Closing the window is not enough; use `Cmd+Q`.

If the active Codex profile is not the default profile, pass the active LevelDB path explicitly:

```bash
./codex-deepseek-installer-darwin-arm64 status \
  --statsig-path "$HOME/Library/Application Support/Codex/Default/Local Storage/leveldb"
```

## Safe Commands

Download the macOS release matching your Mac:

```text
codex-deepseek-installer-<version>-macos-arm64.tar.gz
codex-deepseek-installer-<version>-macos-amd64.tar.gz
```

Run the bundled macOS entrypoint:

```bash
tar -xzf codex-deepseek-installer-<version>-macos-arm64.tar.gz
./install-macos.sh
```

The macOS entrypoint:

- chooses the matching local binary for Apple Silicon or Intel Macs;
- patches config, catalog, and Statsig after Codex App releases the LevelDB lock;
- backs up files and LevelDB before writing;
- verifies the final state;
- leaves Codex App closed by default.

Useful direct commands:

```bash
./codex-deepseek-installer-darwin-arm64 status
./codex-deepseek-installer-darwin-arm64 doctor
./codex-deepseek-installer-darwin-arm64 plan
./codex-deepseek-installer-darwin-arm64 repair --wait-for-app-exit
```

`status` and `doctor` can inspect a temporary read-only LevelDB snapshot while Codex App is running. `repair` writes only after the real LevelDB lock is released.

`--open-after` is opt-in. When it is set, the tool first verifies that all required picker models are present and the Statsig cache is pinned. If verification fails, it refuses to open Codex App.

## Safety Model

The tool is intentionally narrow:

- It refuses to fabricate a missing Statsig schema.
- It refuses to create a fresh Codex setup.
- It refuses non-`tokenflux` top-level provider configs.
- It does not kill Codex App processes.
- It backs up edited files and LevelDB under `~/.codex/tokenflux-model-installer-backups/`.
- It does not copy private LevelDB caches between machines.

Expected failure examples:

```text
missing ~/.codex/config.toml          -> fail
model_provider is not tokenflux       -> fail
missing ~/.codex/models_catalog.json  -> fail
missing official spark catalog object -> fail
Codex App still holds LevelDB LOCK    -> wait only with --wait-for-app-exit
```

## Other Operating Systems

This repository no longer ships Linux or Windows entrypoint scripts, release packages, or one-command installers.

The general repair idea is probably the same on any future platform, but none of the non-macOS details should be treated as known until verified on a real machine:

1. Point Codex config at a structured model catalog.
2. Add complete catalog objects for routed TokenFlux models.
3. Patch the Codex frontend model-picker allowlist in that platform's real local app cache.
4. Back up before writes and verify against the real UI cache.

Do not publish Linux or Windows scripts until the full chain below has been proven end to end on that operating system.

### Linux Investigation Chain

Start by proving where Codex stores its Linux-side state. Do not assume the macOS paths translate directly. Identify the real Codex home directory, the real app data directory, and whether the app uses an Electron/Chromium profile layout with Local Storage LevelDB. Candidate locations may follow XDG conventions, but the active path must be discovered from the running app, filesystem evidence, or app logs rather than guessed.

Once the active locations are known, inspect the same three layers independently:

- Config layer: confirm the active Codex config file exists, has top-level `model_provider = "tokenflux"`, and points `model_catalog_json` at the catalog file actually loaded by Codex.
- Catalog layer: confirm the catalog schema is identical or compatible with macOS, especially `base_instructions`, `model_messages`, tool metadata, reasoning fields, and the existing official `gpt-5.3-codex-spark` object.
- Frontend cache layer: find the real model-picker cache, confirm it is LevelDB or another storage format, and confirm whether Statsig dynamic config `107580212` and `available_models` are still the controlling schema.

Before any write path exists, define a Linux-specific safety model:

- Detect whether the app holds a storage lock and whether read-only snapshot inspection is safe.
- Back up the exact cache directory before modifying it.
- Refuse to write if the schema is missing, ambiguous, or different from macOS.
- Verify by reopening Codex and observing the real model picker, not only by inspecting files.

Linux support should remain documentation-only until those checks are repeatable on a clean Linux Codex install and on an already-used profile.

### Windows Investigation Chain

Windows needs its own discovery pass. Do not assume `%APPDATA%`, `%LOCALAPPDATA%`, profile names, lock behavior, or LevelDB value encoding without checking a real Codex App install. The first task is to map where Codex stores config, catalog, app profile data, Local Storage, and any Statsig cache on Windows.

After the storage map is known, validate the same three layers:

- Config layer: identify the real Codex config file, confirm `tokenflux` is the top-level provider, and confirm the catalog path is the one Codex loads on Windows.
- Catalog layer: verify path escaping, line endings, permissions, and JSON compatibility. The model objects should match the macOS catalog structure unless real Windows evidence shows otherwise.
- Frontend cache layer: determine whether Codex uses Chromium LevelDB, a different profile directory, or a different cache key/value encoding on Windows. Confirm the controlling dynamic config and the exact `available_models` schema before designing any patch.

Windows-specific safety questions must be answered before implementation:

- How does Codex hold file locks on the active cache?
- Can the cache be snapshotted safely while Codex is open?
- What is the correct backup and restore path when a write fails?
- Does reopening the app preserve patched models or immediately refresh them away?
- Are there multiple profiles or partitions, and which one controls the visible model picker?

Windows support should not ship until it has real verification for lock handling, backup/restore, schema matching, and UI picker visibility.

### Publication Gate

A non-macOS implementation is only safe to publish after all of these are true:

- The active config, catalog, and frontend cache paths are known from real installations.
- The frontend cache schema has been compared with macOS and documented.
- Read-only status can distinguish "models missing" from "models present but cache not pinned."
- Repair creates a backup before every write.
- Repair refuses unknown schemas instead of creating guessed records.
- The app can be reopened and the required models are visible in the real picker.
- The behavior has been checked after app restart, machine reboot, and app update where practical.

Until then, Linux and Windows remain conceptual ports, not supported targets.

## Build macOS Release Assets

From a maintainer checkout:

```bash
./build-release.sh
```

This writes only macOS release assets into `dist/`:

```text
codex-deepseek-installer-<version>-macos-arm64.tar.gz
codex-deepseek-installer-<version>-macos-amd64.tar.gz
SHA256SUMS
```

## Maintainer Release Flow

Releases are built by GitHub Actions when a version tag is pushed:

```bash
git tag v0.1.3
git push origin v0.1.3
```

The release workflow runs tests, builds the two macOS archives, generates `SHA256SUMS`, creates the GitHub Release, and uploads only those macOS assets.
