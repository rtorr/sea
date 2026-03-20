package profile

import (
	"os/exec"
	"runtime"
	"strings"
)

// DetectHost auto-detects a build profile for the current host.
func DetectHost() *Profile {
	p := &Profile{
		OS:        normalizeOS(runtime.GOOS),
		Arch:      normalizeArch(runtime.GOARCH),
		BuildType: "release",
		Env:       make(map[string]string),
	}

	detectCompiler(p)

	if p.CppStdlib == "" {
		switch p.OS {
		case "linux":
			p.CppStdlib = "libstdc++"
		case "darwin":
			p.CppStdlib = "libc++"
		case "windows":
			p.CppStdlib = "msvc"
		}
	}

	if p.Name == "" {
		p.Name = p.OS + "-" + p.Arch + "-host"
	}

	return p
}

func normalizeOS(goos string) string {
	switch goos {
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	default:
		return goos
	}
}

func normalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "x86"
	case "arm":
		return "armv7"
	case "riscv64":
		return "riscv64"
	case "mips":
		return "mips"
	case "mips64":
		return "mips64"
	case "mips64le":
		return "mips64el"
	case "mipsle":
		return "mipsel"
	case "ppc64":
		return "ppc64"
	case "ppc64le":
		return "ppc64le"
	case "s390x":
		return "s390x"
	case "loong64":
		return "loong64"
	default:
		return goarch
	}
}

func detectCompiler(p *Profile) {
	// Try gcc first, then clang, then MSVC
	if ver, ok := compilerVersion("gcc", "-dumpversion"); ok {
		p.Compiler = "gcc"
		p.CompilerVersion = majorVersion(ver)
		p.Env["CC"] = "gcc"
		p.Env["CXX"] = "g++"
		return
	}

	if ver, ok := compilerVersion("clang", "--version"); ok {
		p.Compiler = "clang"
		p.CompilerVersion = majorVersion(ver)
		p.CppStdlib = "libc++"
		p.Env["CC"] = "clang"
		p.Env["CXX"] = "clang++"
		return
	}

	if ver, ok := compilerVersion("cl.exe", ""); ok {
		p.Compiler = "msvc"
		p.CompilerVersion = msvcToolset(ver)
		p.CppStdlib = "msvc"
		return
	}

	// Fallback
	p.Compiler = "unknown"
	p.CompilerVersion = "0"
}

func compilerVersion(compiler, flag string) (string, bool) {
	if flag == "" {
		_, err := exec.LookPath(compiler)
		return "", err == nil
	}
	out, err := exec.Command(compiler, flag).Output()
	if err != nil {
		return "", false
	}
	ver := strings.TrimSpace(string(out))
	if compiler == "clang" {
		// clang --version outputs multi-line, extract version number
		for _, line := range strings.Split(ver, "\n") {
			for _, word := range strings.Fields(line) {
				if len(word) > 0 && word[0] >= '0' && word[0] <= '9' {
					return word, true
				}
			}
		}
	}
	return ver, ver != ""
}

func majorVersion(ver string) string {
	if idx := strings.Index(ver, "."); idx > 0 {
		return ver[:idx]
	}
	return ver
}

func msvcToolset(ver string) string {
	// Map MSVC compiler version to toolset: 19.3x-19.4x → v143
	if strings.HasPrefix(ver, "19.3") || strings.HasPrefix(ver, "19.4") {
		return "v143"
	}
	if strings.HasPrefix(ver, "19.2") {
		return "v142"
	}
	return "v" + ver
}
