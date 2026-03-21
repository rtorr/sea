package abi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ABIFingerprint captures the empirical ABI characteristics of a C++ toolchain.
// Two toolchains with the same fingerprint produce binary-compatible objects.
type ABIFingerprint struct {
	// OS and architecture — these are never cross-compatible
	OS   string `json:"os"`
	Arch string `json:"arch"`

	// ManglingScheme is the C++ name mangling: "itanium" (GCC/Clang) or "msvc"
	ManglingScheme string `json:"mangling_scheme"`

	// StdlibABI captures the layout of key stdlib types.
	// This is the hash that actually matters — if std::string is 32 bytes on
	// one toolchain and 8 on another, you cannot link them together.
	StdlibABI string `json:"stdlib_abi"`

	// ExceptionABI is the exception handling mechanism hash
	ExceptionABI string `json:"exception_abi"`

	// Full hash combining all of the above
	Hash string `json:"hash"`
}

// The probe source. This is a minimal C++ program that exercises the
// ABI-sensitive surfaces of the C++ runtime:
//
// 1. std::string layout (the classic GCC 5 ABI break: COW → SSO)
// 2. std::vector layout
// 3. std::function layout (size varies between implementations)
// 4. Virtual table pointer size and layout
// 5. RTTI availability
// 6. Exception object layout
// 7. Name mangling scheme (extracted from symbol names)
//
// It prints sizeof/alignof for each type plus key mangled symbol names.
// The output is deterministic for a given ABI — if two compilers produce
// the same output, they are ABI-compatible.
const probeSource = `
#include <cstddef>
#include <cstdio>
#include <cstdint>
#include <functional>
#include <memory>
#include <string>
#include <vector>
#include <typeinfo>

// Force these types to be instantiated so we can measure their layout
struct VTableProbe {
    virtual ~VTableProbe() = default;
    virtual int method() { return 42; }
};

struct DerivedProbe : VTableProbe {
    int method() override { return 43; }
};

// Exported symbol whose mangling we can inspect
extern "C++" {
    void __sea_abi_probe_fn(std::string*, std::vector<int>*) {}
}

int main() {
    // Type sizes — these define struct layout compatibility
    printf("string_size=%zu\n", sizeof(std::string));
    printf("string_align=%zu\n", alignof(std::string));
    printf("vector_int_size=%zu\n", sizeof(std::vector<int>));
    printf("vector_int_align=%zu\n", alignof(std::vector<int>));
    printf("shared_ptr_size=%zu\n", sizeof(std::shared_ptr<int>));
    printf("shared_ptr_align=%zu\n", alignof(std::shared_ptr<int>));
    printf("function_size=%zu\n", sizeof(std::function<void()>));
    printf("function_align=%zu\n", alignof(std::function<void()>));
    printf("unique_ptr_size=%zu\n", sizeof(std::unique_ptr<int>));
    printf("unique_ptr_align=%zu\n", alignof(std::unique_ptr<int>));

    // Pointer and vtable sizes
    printf("pointer_size=%zu\n", sizeof(void*));
    printf("vtable_ptr_size=%zu\n", sizeof(VTableProbe) - sizeof(int));

    // RTTI check
    VTableProbe base;
    DerivedProbe derived;
    VTableProbe* p = &derived;
    printf("rtti_available=%d\n", typeid(*p) == typeid(DerivedProbe) ? 1 : 0);

    // Exception object size (implementation-defined)
    try {
        throw std::runtime_error("probe");
    } catch (const std::exception& e) {
        printf("exception_what=%s\n", e.what());
    }

    // size_t width (affects ABI of containers)
    printf("size_t_size=%zu\n", sizeof(size_t));

    // wchar_t size (varies on Windows vs Unix)
    printf("wchar_size=%zu\n", sizeof(wchar_t));

    return 0;
}
`

// ProbeToolchain compiles and runs the ABI probe with the given compiler,
// returning an ABIFingerprint that captures the toolchain's ABI characteristics.
func ProbeToolchain(cxx string, os_, arch string) (*ABIFingerprint, error) {
	tmpDir, err := os.MkdirTemp("", "sea-abi-probe-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	srcPath := filepath.Join(tmpDir, "probe.cpp")
	binPath := filepath.Join(tmpDir, "probe")
	objPath := filepath.Join(tmpDir, "probe.o")

	if err := os.WriteFile(srcPath, []byte(probeSource), 0o644); err != nil {
		return nil, fmt.Errorf("writing probe source: %w", err)
	}

	// Compile the probe
	compileArgs := []string{"-std=c++17", "-frtti", "-fexceptions", "-o", binPath, srcPath}
	compileCmd := exec.Command(cxx, compileArgs...)
	compileCmd.Stderr = os.Stderr
	if err := compileCmd.Run(); err != nil {
		return nil, fmt.Errorf("compiling probe with %s: %w", cxx, err)
	}

	// Run the probe to get type sizes
	out, err := exec.Command(binPath).Output()
	if err != nil {
		return nil, fmt.Errorf("running probe: %w", err)
	}
	probeOutput := string(out)

	// Also compile to object file to extract mangled symbol names
	compileObjArgs := []string{"-std=c++17", "-frtti", "-fexceptions", "-c", "-o", objPath, srcPath}
	compileObjCmd := exec.Command(cxx, compileObjArgs...)
	compileObjCmd.Stderr = os.Stderr
	if err := compileObjCmd.Run(); err != nil {
		return nil, fmt.Errorf("compiling probe object: %w", err)
	}

	mangledSymbols := extractMangledSymbols(objPath)
	manglingScheme := detectManglingScheme(mangledSymbols)

	// Hash the stdlib type layout output
	stdlibHash := hashString(probeOutput)

	// Hash the exception-related output
	exceptionLines := filterLines(probeOutput, "exception_")
	exceptionHash := hashString(exceptionLines)

	// Combine everything into the final fingerprint hash
	combined := fmt.Sprintf("%s|%s|%s|%s|%s", os_, arch, manglingScheme, stdlibHash, exceptionHash)
	fullHash := hashString(combined)

	return &ABIFingerprint{
		OS:             os_,
		Arch:           arch,
		ManglingScheme: manglingScheme,
		StdlibABI:      stdlibHash,
		ExceptionABI:   exceptionHash,
		Hash:           fullHash,
	}, nil
}

// Compatible returns true if two fingerprints represent ABI-compatible toolchains.
// OS and arch must match exactly. Mangling scheme must match.
// StdlibABI is the key differentiator — if type layouts match, the ABI is compatible.
func (f *ABIFingerprint) Compatible(other *ABIFingerprint) bool {
	if f.OS != other.OS || f.Arch != other.Arch {
		return false
	}
	if f.ManglingScheme != other.ManglingScheme {
		return false
	}
	// The core check: do stdlib types have the same layout?
	return f.StdlibABI == other.StdlibABI
}

// extractMangledSymbols runs nm on an object file and returns symbol names
func extractMangledSymbols(objPath string) []string {
	// Try nm first (available on all Unix)
	out, err := exec.Command("nm", objPath).Output()
	if err != nil {
		return nil
	}
	var symbols []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			symbols = append(symbols, fields[2])
		} else if len(fields) == 2 {
			symbols = append(symbols, fields[1])
		}
	}
	return symbols
}

// detectManglingScheme determines if the compiler uses Itanium or MSVC mangling
func detectManglingScheme(symbols []string) string {
	for _, sym := range symbols {
		if strings.HasPrefix(sym, "_Z") || strings.HasPrefix(sym, "__Z") {
			return "itanium"
		}
		if strings.HasPrefix(sym, "?") {
			return "msvc"
		}
	}
	return "unknown"
}

func hashString(s string) string {
	// Normalize: sort lines to be order-independent, trim whitespace
	lines := strings.Split(strings.TrimSpace(s), "\n")
	sort.Strings(lines)
	normalized := strings.Join(lines, "\n")
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:8]) // 16 hex chars is enough for fingerprinting
}

func filterLines(output, prefix string) string {
	var filtered []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

// ParseProbeOutput parses the key=value output from the probe binary
func ParseProbeOutput(output string) map[string]string {
	result := make(map[string]string)
	re := regexp.MustCompile(`^(\w+)=(.+)$`)
	for _, line := range strings.Split(output, "\n") {
		if m := re.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			result[m[1]] = m[2]
		}
	}
	return result
}
