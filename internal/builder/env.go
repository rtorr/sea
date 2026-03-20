package builder

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/profile"
)

// BuildEnv computes the environment variables for a build.
func BuildEnv(m *manifest.Manifest, prof *profile.Profile, projectDir, installDir string) []string {
	env := os.Environ()

	// Profile env vars
	for k, v := range prof.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Manifest build env vars
	for k, v := range m.Build.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Sea-specific env vars
	env = append(env,
		fmt.Sprintf("SEA_PACKAGE_NAME=%s", m.Package.Name),
		fmt.Sprintf("SEA_PACKAGE_VERSION=%s", m.Package.Version),
		fmt.Sprintf("SEA_BUILD_TYPE=%s", prof.BuildType),
		fmt.Sprintf("SEA_OS=%s", prof.OS),
		fmt.Sprintf("SEA_ARCH=%s", prof.Arch),
		fmt.Sprintf("SEA_COMPILER=%s", prof.Compiler),
		fmt.Sprintf("SEA_COMPILER_VERSION=%s", prof.CompilerVersion),
		fmt.Sprintf("SEA_ABI_TAG=%s", prof.ABITag()),
		fmt.Sprintf("SEA_PROJECT_DIR=%s", projectDir),
		fmt.Sprintf("SEA_INSTALL_DIR=%s", installDir),
		fmt.Sprintf("SEA_PACKAGES_DIR=%s", filepath.Join(projectDir, "sea_packages")),
	)

	if prof.Sysroot != "" {
		env = append(env, fmt.Sprintf("SEA_SYSROOT=%s", prof.Sysroot))
	}
	if prof.ToolchainPrefix != "" {
		env = append(env, fmt.Sprintf("SEA_TOOLCHAIN_PREFIX=%s", prof.ToolchainPrefix))
	}
	if prof.CFlags != "" {
		env = append(env, fmt.Sprintf("SEA_CFLAGS=%s", prof.CFlags))
	}
	if prof.CXXFlags != "" {
		env = append(env, fmt.Sprintf("SEA_CXXFLAGS=%s", prof.CXXFlags))
	}
	if prof.LDFlags != "" {
		env = append(env, fmt.Sprintf("SEA_LDFLAGS=%s", prof.LDFlags))
	}

	return env
}
