# sea CLI — C++ Package Manager

Go-based CLI tool at `/Users/rtorr/personal/seapkg`.

## Architecture

- `cmd/sea/main.go` — entry point
- `internal/abi/` — ABI symbol extraction, diff, and **probe fingerprinting**
- `internal/archive/` — package archive format (.tar.zst), metadata (sea-package.toml)
- `internal/builder/` — build system detection (CMake, Meson, etc.), source download, env setup
- `internal/cache/` — local package cache (~/.sea/cache/)
- `internal/cli/` — all CLI commands (install, build, publish, etc.)
- `internal/config/` — ~/.sea/config.toml parsing
- `internal/integrate/` — cmake integration (sea-cmake.cmake, Find modules, cmake config relocation)
- `internal/lockfile/` — sea.lock format
- `internal/manifest/` — sea.toml format
- `internal/pkgconfig/` — pkg-config generation
- `internal/profile/` — build profiles, ABI tags, compatibility checking
- `internal/registry/` — registry backends (github-releases, filesystem, artifactory, local)
- `internal/resolver/` — PubGrub dependency resolver

## Key Design Decisions

### ABI Fingerprinting (v2.0.0)
ABI compatibility is determined by an empirical probe, not compiler version strings.
`abi/probe.go` compiles a C++ program that measures sizeof(std::string), sizeof(std::vector),
name mangling scheme, and exception ABI. The resulting hash is the fingerprint. Two toolchains
with the same fingerprint produce link-compatible binaries.

### ABI Tags
Format: `{os}-{arch}-{stdlib}` (e.g., `darwin-aarch64-libcxx`, `linux-x86_64-libstdcxx`).
Compiler name/version deliberately excluded — the fingerprint handles compatibility.
Pure C packages use `{os}-{arch}`.

### cmake Config Relocation
`integrate/relocate.go` rewrites absolute paths in cmake config files at publish time.
Uses both exact prefix matching and regex fallback for external paths (SONAME).

### Build Dependencies
`sea build` installs BOTH runtime and build-only dependencies before building.
Runtime deps go to `sea_packages/`, build-only deps to `sea_build_packages/`.

## Scripts

- `scripts/release.sh <version>` — test, tag, push, watch CI, verify release

## Testing

```bash
go test ./...           # all tests
go test ./internal/abi/ # just the ABI probe tests
```

## Registry

Published packages live at https://github.com/rtorr/sea-packages (GitHub Releases).
See that repo's CLAUDE.md for package-specific documentation.
