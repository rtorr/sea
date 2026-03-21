package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rtorr/sea/internal/abi"
	"github.com/rtorr/sea/internal/archive"
	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/dirs"
	"github.com/rtorr/sea/internal/integrate"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/registry"
	"github.com/spf13/cobra"
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Package and publish to a registry",
	Long: `Package the current project and publish to a registry.

Before uploading, sea automatically:
  1. Extracts exported symbols from your libraries
  2. Fetches the previous version's symbols from the registry
  3. Verifies your version number matches the actual ABI changes:
     - Symbols removed → must bump major
     - Symbols added   → must bump at least minor
     - No changes      → patch is fine

Use --skip-verify to bypass the ABI verification check.`,
	RunE: runPublish,
}

func runPublish(cmd *cobra.Command, args []string) error {
	ciFlag, _ := cmd.Flags().GetBool("ci")
	if ciFlag {
		return runPublishCI(cmd, args)
	}

	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	if err := manifest.Validate(m); err != nil {
		return fmt.Errorf("manifest not valid for publishing: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	prof := getProfile(cfg)
	abiTag := prof.ABITag()
	if m.EffectiveKind() == "header-only" {
		abiTag = "any"
	}

	// Determine registry
	regName := m.Publish.Registry
	if r, _ := cmd.Flags().GetString("registry"); r != "" {
		regName = r
	}
	if regName == "" {
		return fmt.Errorf("no publish registry specified — set [publish].registry in sea.toml or use --registry")
	}

	remote := cfg.FindRemote(regName)
	if remote == nil {
		return fmt.Errorf("registry %q not found in config — use 'sea remote add' first", regName)
	}

	reg, err := registry.FromConfig(remote)
	if err != nil {
		return fmt.Errorf("initializing registry %q: %w", regName, err)
	}

	// Set up multi for previous version lookup
	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return fmt.Errorf("initializing registries: %w", err)
	}
	if err := prof.EnsureFingerprint(); err != nil {
		cmd.Printf("Warning: ABI probe failed: %v\n", err)
	}
	multi.SetLocalFingerprint(prof.ABIFingerprintHash)

	// Determine source directory: built output or project root.
	// For header-only packages, the build output uses the host ABI tag
	// (not "any") since that's what the builder creates. The publish
	// ABI tag is "any" but the build directory uses the host tag.
	buildABI := abiTag
	if m.EffectiveKind() == "header-only" {
		buildABI = prof.ABITag()
	}
	srcDir := filepath.Join(dir, dirs.SeaBuild, buildABI)
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		// Also try the publish ABI tag (in case someone manually built for "any")
		srcDir = filepath.Join(dir, dirs.SeaBuild, abiTag)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			srcDir = dir
		}
	}

	// Create archive in temp directory
	archiveName := fmt.Sprintf("%s-%s-%s.tar.zst", m.Package.Name, m.Package.Version, abiTag)
	tmpDir, err := os.MkdirTemp("", "sea-publish-*")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, archiveName)

	includes := m.Publish.Include
	if len(includes) == 0 {
		includes = []string{"include/**", "lib/**", "bin/**", "share/**", "LICENSE", "COPYING"}
	}

	// Relocate cmake configs BEFORE archiving — the archive should contain
	// relative paths so packages work regardless of where they're installed.
	integrate.RelocateCMakeConfigs(srcDir)

	cmd.Printf("Packaging %s@%s for %s...\n", m.Package.Name, m.Package.Version, abiTag)
	if err := archive.Pack(srcDir, includes, archivePath); err != nil {
		return fmt.Errorf("creating archive: %w", err)
	}

	fi, err := os.Stat(archivePath)
	if err != nil || fi.Size() == 0 {
		return fmt.Errorf("archive is empty — no files matched include patterns %v", includes)
	}

	// Validate: source packages must have library files in lib/
	if m.EffectiveKind() == "source" {
		hasLib := false
		libDir := filepath.Join(srcDir, "lib")
		if entries, scanErr := os.ReadDir(libDir); scanErr == nil {
			for _, e := range entries {
				name := e.Name()
				if strings.HasSuffix(name, ".a") || strings.HasSuffix(name, ".so") ||
					strings.HasSuffix(name, ".dylib") || strings.HasSuffix(name, ".lib") ||
					strings.HasSuffix(name, ".dll") || strings.Contains(name, ".so.") {
					hasLib = true
					break
				}
			}
		}
		if !hasLib {
			return fmt.Errorf("source package has no library files in lib/ — build may have failed silently.\nCheck that your build.sh copies libraries to $SEA_INSTALL_DIR/lib/")
		}
	}

	// Extract symbols from libraries — track shared and static separately
	var libs []archive.MetaLib
	var allSymbols []abi.Symbol    // all symbols (for metadata)
	var sharedSymbols []abi.Symbol // symbols from shared libraries only
	libDir := filepath.Join(srcDir, "lib")
	if entries, err := os.ReadDir(libDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !isLibrary(name) {
				continue
			}

			libPath := filepath.Join(libDir, name)
			syms, err := abi.ExtractSymbols(libPath)
			if err != nil {
				if verbose {
					cmd.Printf("  Warning: could not extract symbols from %s: %v\n", name, err)
				}
				continue
			}
			allSymbols = append(allSymbols, syms...)

			libType := "shared"
			if strings.HasSuffix(name, ".a") {
				libType = "static"
			} else {
				sharedSymbols = append(sharedSymbols, syms...)
			}

			// Extract SONAME for shared ELF libraries
			var soname string
			if libType == "shared" {
				soname, _ = abi.ExtractSONAME(libPath)
			}

			libs = append(libs, archive.MetaLib{
				Path:                filepath.Join("lib", name),
				Type:                libType,
				Soname:              soname,
				ExportedSymbolCount: len(syms),
			})
		}
	}

	// ── ABI verification against previous version ──
	skipVerify, _ := cmd.Flags().GetBool("skip-verify")
	if !skipVerify && len(allSymbols) > 0 {
		if err := verifyABIBump(cmd, multi, m, abiTag, allSymbols); err != nil {
			return err
		}
	}

	// ── Static dependency symbol leak detection ──
	// If this package has shared libraries AND any dependencies are statically
	// linked, check whether dependency symbols leaked into the shared lib exports.
	if len(sharedSymbols) > 0 {
		if err := checkStaticLeaks(cmd, multi, m, abiTag, sharedSymbols, srcDir); err != nil {
			return err
		}
	}

	// Check visibility policy
	visClean := true
	if m.Build.Visibility == "hidden" && len(allSymbols) > 0 {
		report := abi.CheckVisibility(allSymbols, abi.DefaultPolicy())
		visClean = report.Clean
		if !report.Clean {
			cmd.Printf("Warning: %d symbol(s) leaked (visibility policy: hidden)\n", len(report.LeakedSymbols))
			for _, s := range report.LeakedSymbols {
				cmd.Printf("  ! %s\n", s.Name)
			}
		}
	}

	// Compute symbols hash and list
	symbolNames := abi.SymbolNames(allSymbols)
	symbolList := abi.FormatSymbolList(allSymbols)
	symHash := sha256.Sum256([]byte(symbolList))

	// Build dependency list for metadata
	var metaDeps []archive.MetaDependency
	for name, dep := range m.AllDependencies(false) {
		metaDeps = append(metaDeps, archive.MetaDependency{
			Name:    name,
			Version: dep.Version,
		})
	}

	meta := &archive.PackageMeta{
		Package: archive.MetaPackage{
			Name:    m.Package.Name,
			Version: m.Package.Version,
			Channel: m.EffectiveChannel(),
			Kind:    m.EffectiveKind(),
		},
		ABI: archive.MetaABI{
			Tag:             abiTag,
			OS:              prof.OS,
			Arch:            prof.Arch,
			Compiler:        prof.Compiler,
			CompilerVersion: prof.CompilerVersion,
			CppStdlib:       prof.CppStdlib,
			BuildType:       prof.BuildType,
			Fingerprint:     prof.ABIFingerprintHash,
		},
		Contents: archive.MetaContents{
			IncludeDirs: []string{"include"},
			LibDirs:     []string{"lib"},
		},
		Libs: libs,
		Symbols: archive.MetaSymbols{
			SymbolsHash:     hex.EncodeToString(symHash[:]),
			VisibilityClean: visClean,
			Exported:        symbolNames,
		},
		Dependencies: metaDeps,
	}

	// Dry-run check
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		cmd.Printf("Dry run summary:\n")
		cmd.Printf("  Name:         %s\n", m.Package.Name)
		cmd.Printf("  Version:      %s\n", m.Package.Version)
		cmd.Printf("  ABI tag:      %s\n", abiTag)
		cmd.Printf("  Archive size: %d bytes\n", fi.Size())
		cmd.Printf("  Symbols:      %d exported\n", len(symbolNames))
		cmd.Printf("Dry run — nothing published\n")
		return nil
	}

	// Compute archive hash
	archiveHash, err := hashFile(archivePath)
	if err != nil {
		return fmt.Errorf("hashing archive: %w", err)
	}

	// Upload the archive
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()

	cmd.Printf("Publishing to %s...\n", regName)
	if err := reg.Upload(m.Package.Name, m.Package.Version, abiTag, f, meta); err != nil {
		return fmt.Errorf("publishing: %w", err)
	}

	// Update the version manifest — tracks all channel × platform artifacts
	channel := m.EffectiveChannel()
	vm := &archive.VersionManifest{
		Package: archive.VersionManifestPackage{
			Name:    m.Package.Name,
			Version: m.Package.Version,
			Kind:    m.EffectiveKind(),
		},
		Artifacts: []archive.ArtifactEntry{
			archive.NewArtifactEntry(channel, abiTag, archiveHash, ""),
		},
	}
	if err := reg.UploadVersionManifest(m.Package.Name, m.Package.Version, vm); err != nil {
		// Non-fatal: the archive was already uploaded successfully
		cmd.Printf("Warning: could not update version manifest: %v\n", err)
	}

	cmd.Printf("Published %s@%s [%s] (%s) to %s\n", m.Package.Name, m.Package.Version, channel, abiTag, regName)
	return nil
}

// hashFile computes the SHA256 of a file.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyABIBump fetches the previous version's symbols from the registry and checks
// whether the proposed version number correctly reflects the ABI changes.
func verifyABIBump(cmd *cobra.Command, multi *registry.Multi, m *manifest.Manifest, abiTag string, newSymbols []abi.Symbol) error {
	prevSymNames, prevVer, err := multi.FetchPreviousSymbols(m.Package.Name, m.Package.Version, abiTag)
	if err != nil {
		if verbose {
			cmd.Printf("  Note: could not fetch previous version for ABI check: %v\n", err)
		}
		return nil // can't verify — don't block
	}
	if prevVer == "" {
		cmd.Println("  First version — no previous ABI to compare against.")
		return nil
	}

	// Convert previous symbol names to Symbol structs for the diff
	var prevSyms []abi.Symbol
	for _, name := range prevSymNames {
		prevSyms = append(prevSyms, abi.Symbol{Name: name})
	}

	// Parse versions
	var oldMaj, oldMin, oldPat int
	fmt.Sscanf(prevVer, "%d.%d.%d", &oldMaj, &oldMin, &oldPat)
	var newMaj, newMin, newPat int
	fmt.Sscanf(m.Package.Version, "%d.%d.%d", &newMaj, &newMin, &newPat)

	// Run verification
	bump, diff := abi.RequiredBump(prevSyms, newSymbols)

	cmd.Printf("  ABI check against %s@%s:\n", m.Package.Name, prevVer)
	if len(diff.Added) > 0 {
		cmd.Printf("    + %d symbol(s) added\n", len(diff.Added))
	}
	if len(diff.Removed) > 0 {
		cmd.Printf("    - %d symbol(s) removed\n", len(diff.Removed))
	}
	if len(diff.Added) == 0 && len(diff.Removed) == 0 {
		cmd.Println("    No symbol changes.")
	}
	cmd.Printf("    Required bump: %s\n", bump)

	if err := abi.VerifyVersion(oldMaj, oldMin, oldPat, newMaj, newMin, newPat, prevSyms, newSymbols); err != nil {
		return fmt.Errorf("ABI verification failed: %w\n  Use --skip-verify to bypass this check", err)
	}

	cmd.Printf("    Version %s → %s: OK\n", prevVer, m.Package.Version)
	return nil
}

// checkStaticLeaks detects when symbols from statically-linked dependencies
// leak into the shared library's export table. This is a common and dangerous
// mistake: if consumer A links your libfoo.so (which statically includes libbar),
// and consumer B also links libbar, the dynamic linker sees duplicate symbols.
func checkStaticLeaks(cmd *cobra.Command, multi *registry.Multi, m *manifest.Manifest, abiTag string, sharedSymbols []abi.Symbol, srcDir string) error {
	// Collect symbol lists from deps that are linked statically
	depSymbols := make(map[string][]string)

	for name, dep := range m.Dependencies {
		if dep.Linking != "static" {
			continue
		}

		// Try to get the dep's symbols from the registry metadata
		symNames, _, err := multi.FetchPreviousSymbols(name, "999999.0.0", abiTag)
		if err == nil && len(symNames) > 0 {
			depSymbols[name] = symNames
			continue
		}

		// Fallback: try to read symbols from the installed dep in sea_packages
		depLib := findDepSharedLib(srcDir, name)
		if depLib != "" {
			syms, err := abi.ExtractSymbols(depLib)
			if err == nil {
				names := make([]string, len(syms))
				for i, s := range syms {
					names[i] = s.Name
				}
				depSymbols[name] = names
			}
		}
	}

	if len(depSymbols) == 0 {
		return nil // no static deps with known symbols
	}

	report := abi.CheckStaticLeaks(sharedSymbols, depSymbols)
	if report.Clean {
		cmd.Println("  Static leak check: clean")
		return nil
	}

	// Leaks found — report and generate fix
	cmd.Print(abi.FormatStaticLeakReport(report))

	// Generate fix guidance
	ownSymbols := abi.IdentifyOwnSymbols(sharedSymbols, depSymbols)
	cmd.Println("\nTo fix, export only your own symbols. Options:")
	cmd.Println("  1. Build with -fvisibility=hidden and mark exports with __attribute__((visibility(\"default\")))")
	cmd.Println("  2. Use a linker version script (Linux) or -exported_symbols_list (macOS)")
	cmd.Println("")

	// Write the scripts to the build directory for convenience
	vsPath := filepath.Join(srcDir, "sea-exports.map")
	if err := os.WriteFile(vsPath, []byte(abi.GenerateVersionScript(ownSymbols)), 0o644); err == nil {
		cmd.Printf("  Generated: %s (Linux: -Wl,--version-script=%s)\n", vsPath, vsPath)
	}
	elPath := filepath.Join(srcDir, "sea-exports.txt")
	if err := os.WriteFile(elPath, []byte(abi.GenerateExportList(ownSymbols)), 0o644); err == nil {
		cmd.Printf("  Generated: %s (macOS: -exported_symbols_list %s)\n", elPath, elPath)
	}

	return fmt.Errorf("static dependency symbols leaked into shared library exports — %d symbol(s) from %d dep(s)\n  Use --skip-verify to bypass, or apply the generated export scripts",
		report.TotalLeaked, len(report.Leaks))
}

// findDepSharedLib looks for a dependency's shared library in sea_packages or
// the build directory, for symbol extraction.
func findDepSharedLib(baseDir string, depName string) string {
	for _, searchDir := range []string{
		filepath.Join(baseDir, "..", dirs.SeaPackages, depName, "lib"),
		filepath.Join(baseDir, "..", dirs.SeaBuildPackages, depName, "lib"),
	} {
		entries, err := os.ReadDir(searchDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if (strings.HasSuffix(name, ".so") || strings.HasSuffix(name, ".dylib")) &&
				!strings.Contains(name, ".so.") {
				return filepath.Join(searchDir, name)
			}
		}
	}
	return ""
}

func init() {
	publishCmd.Flags().String("registry", "", "override publish registry")
	publishCmd.Flags().Bool("skip-verify", false, "skip ABI version verification and static leak checking")
	publishCmd.Flags().Bool("dry-run", false, "show what would be published without uploading")
	publishCmd.Flags().Bool("ci", false, "trigger CI to build and publish for all platforms")
	publishCmd.Flags().Bool("watch", false, "wait for CI to complete (requires gh CLI, use with --ci)")
}

func isLibrary(name string) bool {
	if strings.HasSuffix(name, ".so") || strings.HasSuffix(name, ".dylib") ||
		strings.HasSuffix(name, ".a") || strings.HasSuffix(name, ".dll") ||
		strings.HasSuffix(name, ".lib") {
		return true
	}
	if strings.Contains(name, ".so.") {
		return true
	}
	return false
}

