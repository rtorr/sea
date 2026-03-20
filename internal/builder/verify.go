package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rtorr/sea/internal/abi"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
)

const verifyTimeout = 2 * time.Minute

// VerifyBuildOutput validates the build output automatically.
//
// Automatic checks (always run, no config needed):
//  1. include/ directory exists and has at least one file
//  2. lib/ directory exists and has at least one library
//  3. At least one shared library has extractable symbols
//
// Optional: if [build].test is set, also compile+link+run that test program.
func VerifyBuildOutput(m *manifest.Manifest, prof *profile.Profile, projectDir, installDir string) error {
	isHeaderOnly := m.EffectiveKind() == "header-only"
	includeDir := filepath.Join(installDir, "include")
	libDir := filepath.Join(installDir, "lib")

	// 1. Check headers exist
	headerCount := countFiles(includeDir)
	if headerCount == 0 {
		return fmt.Errorf("build produced no headers in %s/include/", installDir)
	}

	// Header-only packages don't need libraries
	if isHeaderOnly {
		if m.Build.Test != "" {
			return runTestProgram(m, prof, projectDir, installDir)
		}
		return nil
	}

	// 2. Check libraries exist
	libCount := 0
	sharedCount := 0
	if entries, err := os.ReadDir(libDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if isLib(name) {
				libCount++
				if isSharedLib(name) {
					sharedCount++
				}
			}
		}
	}
	if libCount == 0 {
		return fmt.Errorf("build produced no libraries in %s/lib/", installDir)
	}

	// 3. Check symbol extraction works on at least one shared library
	if sharedCount > 0 {
		symbolsFound := false
		entries, _ := os.ReadDir(libDir)
		for _, e := range entries {
			if !isSharedLib(e.Name()) {
				continue
			}
			syms, err := abi.ExtractSymbols(filepath.Join(libDir, e.Name()))
			if err == nil && len(syms) > 0 {
				symbolsFound = true
				break
			}
		}
		if !symbolsFound {
			return fmt.Errorf("build produced shared libraries but no symbols could be extracted — the library may be empty or corrupted")
		}
	}

	// 4. Optional: run test program if configured
	if m.Build.Test != "" {
		if err := runTestProgram(m, prof, projectDir, installDir); err != nil {
			return err
		}
	}

	return nil
}

func countFiles(dir string) int {
	count := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	return count
}

func isLib(name string) bool {
	return strings.HasSuffix(name, ".a") ||
		strings.HasSuffix(name, ".so") ||
		strings.HasSuffix(name, ".dylib") ||
		strings.HasSuffix(name, ".dll") ||
		strings.HasSuffix(name, ".lib") ||
		strings.Contains(name, ".so.")
}

func isSharedLib(name string) bool {
	return strings.HasSuffix(name, ".so") ||
		strings.HasSuffix(name, ".dylib") ||
		strings.HasSuffix(name, ".dll") ||
		strings.Contains(name, ".so.")
}

// runTestProgram compiles and runs the test program specified in [build].test.
func runTestProgram(m *manifest.Manifest, prof *profile.Profile, projectDir, installDir string) error {
	testSrc := m.Build.Test
	if !filepath.IsAbs(testSrc) {
		testSrc = filepath.Join(projectDir, testSrc)
	}
	if _, err := os.Stat(testSrc); err != nil {
		return fmt.Errorf("build test source not found: %s", m.Build.Test)
	}

	cc := "cc"
	cxx := "c++"
	if prof.Env != nil {
		if v, ok := prof.Env["CC"]; ok {
			cc = v
		}
		if v, ok := prof.Env["CXX"]; ok {
			cxx = v
		}
	}
	compiler := cc
	ext := filepath.Ext(testSrc)
	if ext == ".cpp" || ext == ".cxx" || ext == ".cc" {
		compiler = cxx
	}

	includeDir := filepath.Join(installDir, "include")
	libDir := filepath.Join(installDir, "lib")

	// Discover -l flags
	var lFlags []string
	if entries, err := os.ReadDir(libDir); err == nil {
		seen := make(map[string]bool)
		for _, e := range entries {
			name := extractTestLibName(e.Name())
			if name != "" && !seen[name] {
				seen[name] = true
				lFlags = append(lFlags, "-l"+name)
			}
		}
	}

	tmpDir, err := os.MkdirTemp("", "sea-verify-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	testBin := filepath.Join(tmpDir, "sea_verify")
	if runtime.GOOS == "windows" {
		testBin += ".exe"
	}

	args := []string{"-I" + includeDir, testSrc, "-L" + libDir}
	args = append(args, lFlags...)
	if runtime.GOOS != "windows" {
		args = append(args, "-Wl,-rpath,"+libDir)
	}
	args = append(args, "-o", testBin)

	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer cancel()

	compileCmd := exec.CommandContext(ctx, compiler, args...)
	compileCmd.Dir = projectDir
	compileCmd.Stdout = os.Stdout
	compileCmd.Stderr = os.Stderr
	if err := compileCmd.Run(); err != nil {
		return fmt.Errorf("build test failed to compile: %w\n  command: %s %s", err, compiler, strings.Join(args, " "))
	}

	runCmd := exec.CommandContext(ctx, testBin)
	runCmd.Dir = projectDir
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("build test timed out")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("build test exited with code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("build test failed: %w", err)
	}

	return nil
}

func extractTestLibName(name string) string {
	if strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".dylib") {
		base := name[3 : len(name)-6]
		if !strings.Contains(base, ".") {
			return base
		}
		return ""
	}
	if strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".so") {
		return name[3 : len(name)-3]
	}
	if strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".a") {
		return name[3 : len(name)-2]
	}
	if strings.HasSuffix(name, ".lib") {
		return name[:len(name)-4]
	}
	return ""
}
