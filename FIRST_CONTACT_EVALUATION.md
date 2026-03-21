# Sea: First-Contact Evaluation

## Chronological "First Hour Experience" Narrative

### Minute 0-2: Orientation

I land in `/home/user/sea`. I see `go.mod`, `cmd/`, `internal/`, `examples/`, `README.md`. Good — standard Go project layout. I immediately know this is a Go CLI tool.

I read the README. It's excellent — clear positioning ("A C/C++ package manager where versions mean compatibility, not hashes"), a Quick Start, command reference, and a package format spec. Within 60 seconds I understand what this project is and why it exists. The README is one of the strongest parts of the project.

### Minute 2-5: Try to Build

I run `go build ./cmd/sea/`. This fails because `klauspost/compress` can't be downloaded (network issue in this environment). **First friction:** the project doesn't vendor its dependencies. A `go mod vendor` + committed vendor directory would make this buildable offline and in restricted environments.

I notice the project uses `go 1.23.3` in `go.mod` but CI pins `go-version: "1.22"`. This is a version mismatch that could cause subtle issues.

### Minute 5-10: Read the Entrypoint

`cmd/sea/main.go` is 15 lines. Clean. Delegates to `cli.Execute()`. I follow the thread to `internal/cli/root.go` which registers 17 commands via cobra. This is textbook Go CLI structure — no surprises, no indirection.

### Minute 10-20: Explore Commands

The CLI commands are well-organized, one file per command (mostly). I notice:

- `install.go` is 947 lines — the largest file by far. It handles resolution, downloading, building from source, linking, lockfile writing, CMake integration, and soname symlink creation. This is a **god file** that does too many things.
- `info.go` has a dangling `init()` function with a comment `// Register in root.go is needed — let me check if it's there` — leftover debugging thought committed to source.
- `clean.go:72` — `reinstallCmd` calls `cleanCmd.RunE(cmd, nil)` but **ignores the error**. If clean fails, install proceeds on dirty state.
- `update.go:131` — calls `os.Exit(1)` directly inside a cobra RunE handler, bypassing cobra's error handling and making the function untestable.

### Minute 20-30: Explore the Architecture

The `internal/` package layout is clean and well-separated:

| Package | Purpose | Quality |
|---------|---------|---------|
| abi | Symbol extraction + ABI diffing | Excellent — supports ELF, Mach-O, PE with DWARF type analysis |
| archive | Tar.zst packing with security controls | Very good — path traversal protection, size limits |
| builder | Build orchestration (CMake/Make/Meson) | Good — auto-detection, caching, 30min timeout |
| cache | Local package + build cache | Good — atomic operations, file locking |
| config | ~/.sea/config.toml management | Good — multi-registry routing |
| manifest | sea.toml parsing | Excellent — strict validation, clear errors |
| registry | 5 registry backends | Good — retry logic, auth chain |
| resolver | PubGrub dependency resolution | Good — proper algorithm, clear error types |
| integrate | CMake/pkg-config generation | Adequate — no tests |
| lockfile | sea.lock serialization | Good — schema versioning |
| profile | ABI tag detection | Good — compiler detection, compatibility ranking |
| pkgconfig | .pc file generation | Adequate — fragile library name extraction |

### Minute 30-40: Try the E2E Test

I look at `examples/run-e2e.sh`. This is the **real integration test** — it builds real C/C++ libraries (cjson, lz4, fmt), publishes them to a local filesystem registry, installs them, compiles against them, and verifies ABI gates work.

I can't run it (no network + no sea binary), but reading it reveals:

1. It expects the binary at `$PROJECT_ROOT/bin/sea` but `go build` puts it in the current directory. The README says `go install` which puts it in `$GOPATH/bin`. Neither matches the e2e script's expectation. **Three different binary locations, none documented together.**

2. The fmt symlink handling (lines 162-169) is macOS-specific — it looks for `.dylib` files. On Linux this entire block is a no-op, and fmt compilation may fail.

3. There's no prerequisite check. The script silently needs: Go, cc, c++, cmake, curl, make, nm, ar. If any is missing, you get a cryptic error mid-run.

### Minute 40-50: Inspect the Package Examples

The example packages (cjson, lz4, fmt) are well-structured and demonstrate real-world usage. The build scripts handle cross-platform (macOS/Linux) with try-and-fallback patterns. `cjson-bad-1.7.1` is clever — it intentionally removes a symbol to test the ABI gate.

But: all examples are `kind = "source"`. The README documents `kind = "prebuilt"` and `kind = "header-only"` but there are zero examples of either. A newcomer wanting to publish a header-only library has no reference.

### Minute 50-60: Assess the Big Picture

The project is surprisingly complete for its scope. The PubGrub resolver, multi-format ABI extraction, and ABI verification gate are genuinely novel features. The core thesis — "versions should mean compatibility, not hashes" — is well-implemented.

But the surface doesn't reflect the depth. The README is the only documentation. There's no `CONTRIBUTING.md`, no architecture doc, no `Makefile` for common tasks, and no way to discover the elegant internals without reading source.

---

## Prioritized List of Rough Spots

### Major Onboarding Hazards

**1. No Makefile or task runner**
- **What a newcomer tries:** `make build`, `make test`, or looks for a Makefile
- **What happens:** Nothing. You have to know to run `go build ./cmd/sea/` and `go test ./...` and `examples/run-e2e.sh`
- **Category:** DX / Tooling
- **Recommendation:** Add a Makefile with `build`, `test`, `e2e`, `lint`, `clean` targets
- **Severity:** Major onboarding hazard

**2. Dependencies not vendored**
- **What a newcomer tries:** Clone and build
- **What happens:** Fails in any environment without internet access to Go module proxy
- **Category:** Build flow
- **Recommendation:** Run `go mod vendor` and commit the vendor directory, or document that network access is required
- **Severity:** Major onboarding hazard

**3. Binary output location inconsistency**
- **What a newcomer tries:** Build the binary and run it
- **What happens:** `go build ./cmd/sea/` puts it in `./sea`. The e2e script expects `bin/sea`. `go install` puts it in `$GOPATH/bin/sea`. The README suggests `go install github.com/rtorr/sea/cmd/sea@latest`. Four different locations.
- **Category:** Build flow / Discoverability
- **Recommendation:** Add a Makefile that builds to `bin/sea` consistently, matching what e2e expects
- **Severity:** Major onboarding hazard

### Moderate Friction Points

**4. `install.go` is a 947-line god file**
- **What a newcomer tries:** Understand how install works
- **What happens:** They open one file that handles resolution, downloading, source-building, linking, lockfile writing, CMake integration, soname symlinks, and Windows fallback paths
- **Category:** Architecture
- **Recommendation:** Extract into `install_resolve.go`, `install_download.go`, `install_link.go`
- **Severity:** Moderate friction

**5. `os.Exit(1)` in update.go:131**
- **What a newcomer tries:** Test or extend the update --check command
- **What happens:** The function calls `os.Exit(1)` after printing results, bypassing cobra's error handling. Line 132 (`return nil`) is unreachable dead code.
- **Category:** Architecture / Tooling
- **Recommendation:** Return a sentinel error or use cobra's `SilenceErrors` with a non-zero exit code
- **Severity:** Moderate friction

**6. `reinstallCmd` silently swallows clean errors**
- **What a newcomer tries:** `sea reinstall` when a directory has permission issues
- **What happens:** Clean fails silently, install runs on dirty state, producing confusing results
- **Category:** DX / Error handling
- **Recommendation:** `if err := cleanCmd.RunE(cmd, nil); err != nil { return err }`
- **Severity:** Moderate friction

**7. E2E tests have no prerequisite check**
- **What a newcomer tries:** Run `examples/run-e2e.sh`
- **What happens:** Fails with cryptic error if cmake, curl, or C compiler is missing
- **Category:** DX / Tooling
- **Recommendation:** Add a prereq check at the top: `command -v cc cmake curl make >/dev/null || { echo "Missing: ..."; exit 1; }`
- **Severity:** Moderate friction

**8. E2E fmt symlink handling is macOS-only**
- **What a newcomer tries:** Run e2e on Linux
- **What happens:** Lines 162-169 look for `.dylib` files, find nothing on Linux, and the fmt C++ compilation may fail because no `libfmt.so` symlink exists
- **Category:** Tooling / Build flow
- **Recommendation:** Add `.so` handling alongside `.dylib` in the symlink block
- **Severity:** Moderate friction

**9. Magic directory names scattered as strings**
- **What a newcomer tries:** Search for where `sea_packages` is defined as a constant
- **What happens:** It's hardcoded as a string literal in 10+ places across the codebase
- **Category:** Architecture / Naming
- **Recommendation:** Define `const SeaPackagesDir = "sea_packages"` (etc.) in a shared constants package
- **Severity:** Moderate friction

**10. No examples for `prebuilt` or `header-only` package kinds**
- **What a newcomer tries:** Create a header-only library package
- **What happens:** README documents it but zero examples exist. Must read source to understand behavior.
- **Category:** Docs / Discoverability
- **Recommendation:** Add `examples/packages/doctest/` (header-only) and a prebuilt example
- **Severity:** Moderate friction

### Small Paper Cuts

**11. `info.go:158-160` has a leftover debugging comment in `init()`**
- Empty `init()` with comment: `// Register in root.go is needed — let me check if it's there`
- **Category:** DX
- **Recommendation:** Delete the empty init()
- **Severity:** Small paper cut

**12. CI Go version (1.22) doesn't match go.mod (1.23.3)**
- **Category:** Build flow
- **Recommendation:** Align CI to use `go-version-file: go.mod`
- **Severity:** Small paper cut

**13. No `.editorconfig` or `gofmt` enforcement in CI**
- **Category:** DX
- **Recommendation:** Add `gofmt -d .` check in CI
- **Severity:** Small paper cut

**14. `_fbuild` and `_build` in clean.go without explanation**
- **What a newcomer tries:** Understand what `_fbuild` is
- **What happens:** No documentation. It's apparently an intermediate build directory, but only referenced in `clean.go`
- **Category:** Naming / Discoverability
- **Recommendation:** Add a comment or remove if obsolete
- **Severity:** Small paper cut

**15. Test coverage is thin for integration paths**
- 23 test files, ~3800 lines of tests. Unit tests exist for core algorithms (semver, resolver, cache) but no integration tests for the full install/build/publish pipeline in Go (only the bash e2e script).
- **Category:** Tooling
- **Recommendation:** Add Go integration tests using `testscript` or similar
- **Severity:** Small paper cut

**16. `archive.Unpack()` silently skips symlinks**
- Symlinks in archives are silently ignored. A package that relies on symlinks (common in lib/ directories for soname versioning) will lose them.
- **Category:** Runtime flow
- **Recommendation:** At minimum, log a warning when symlinks are skipped
- **Severity:** Small paper cut

---

## Top 3 Changes to Most Improve First-Contact Experience

### 1. Add a Makefile

```makefile
.PHONY: build test e2e lint clean

build:
	go build -o bin/sea ./cmd/sea

test:
	go test -race ./... -count=1

e2e: build
	./examples/run-e2e.sh

lint:
	go vet ./...
	gofmt -d .

clean:
	rm -rf bin/ dist/
```

This single file resolves the binary location inconsistency, gives newcomers a discoverable entry point, and documents all the common workflows. Every Go project a newcomer has seen before has either a Makefile or a `justfile`. Its absence is the biggest false affordance — the project looks like a standard Go project but is missing the standard task runner.

### 2. Fix the error handling bugs (reinstallCmd + update os.Exit)

These are small code fixes with outsized impact. A newcomer who hits `sea reinstall` with a permission error or tries to test `sea update --check` will lose trust in the tool's reliability. Fix `clean.go:72` to propagate errors and replace `update.go:131` with a proper error return.

### 3. Add prerequisite checks and Linux support to E2E tests

The e2e script is the project's crown jewel demo — it proves the entire thesis works end-to-end. But it's fragile. Adding a 5-line prereq check at the top and fixing the macOS-only symlink block would make it runnable by any newcomer on any Unix system.

---

## False Affordances

### 1. "Standard Go project" suggests `go build` just works
The layout says "standard Go CLI" but without vendored deps or a Makefile, the obvious `clone && build` path fails in restricted environments. The e2e script expects `bin/sea` but nothing produces that without knowing to pass `-o bin/sea`.

### 2. README Quick Start suggests a working public registry
The Quick Start shows `sea remote add packages github-releases rtorr/sea-packages`, implying a functioning public package registry. But the `sea-packages` repo may or may not have published packages. A newcomer following the Quick Start has no way to verify this works without trying it. The e2e tests use a local filesystem registry instead, suggesting the public registry path may not be the primary workflow yet.

### 3. `sea build` suggests auto-detection is the primary path
The README says "auto-detects CMake/Make/Meson" but every example package uses an explicit `build.sh` script. Auto-detection is implemented in the code but never demonstrated, creating uncertainty about whether it actually works in practice.

### 4. The clean command's directory list implies more build systems than documented
`clean.go` removes `_fbuild`, `_build`, `_src` — directories never mentioned in docs or examples. This suggests either historical build approaches that were deprecated, or internal implementation details leaking through the CLI.

---

## Where the Project Is Actually Elegant

1. **ABI verification gate** — The publish-time symbol extraction, diffing against previous versions, and automatic major/minor/patch enforcement is genuinely novel and well-implemented. The DWARF type-level analysis goes beyond simple symbol name comparison.

2. **PubGrub resolver** — Using a proper dependency resolution algorithm (not just "latest wins") with clear conflict error messages and locked-version preference is sophisticated and correct.

3. **Archive security** — Path traversal protection, symlink bomb prevention, file count/size limits, and mode sanitization. This is production-grade archive handling that most tools skip.

4. **Profile compatibility ranking** — The ABI tag system with ranked compatibility (exact=100, any=90, compat=80) allows graceful fallback when exact platform matches aren't available.

5. **Static symbol leak detection** — Detecting when statically-linked dependency symbols leak into a shared library's export table, and generating a linker script to fix it, is a feature I've never seen in another package manager.

6. **The e2e test itself** — Despite the fragility issues, the fact that it builds real upstream libraries (cjson, lz4, fmt), publishes them, installs them, compiles against them, and runs the result is an impressive integration test that proves the entire system works end-to-end.

---

## Where Source Inspection Is Required to Understand Core Behavior

1. **How `sea install` falls back to source builds** — Not documented anywhere. If a prebuilt package isn't found for your ABI tag, sea silently downloads source and builds locally. You'd only discover this by reading `install.go:downloadOrBuild()`.

2. **How profile detection works** — The ABI tag format (`linux-x86_64-gcc13-libstdc++`) is shown in output but never explained. You'd need to read `profile/detect.go` to understand the compiler detection chain (gcc → clang → MSVC).

3. **How registry routing works** — Config supports per-package and per-platform registry routing with wildcard matching, but this is only documented in the README's brief config example. The actual matching logic (prefix wildcards, suffix wildcards, platform patterns) requires reading `config/config.go`.

4. **How the build cache works** — Build caching hashes all source files + build script to create a cache key. Cache hits skip rebuilds entirely. This is never mentioned in docs; you'd need to read `cache/buildcache.go`.

5. **How lockfile sync detection works** — `sea install` is a no-op when the lockfile is in sync. The sync check compares manifest hash + feature set. This optimization is invisible and undocumented.
