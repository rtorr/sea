package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rtorr/sea/internal/config"
	"github.com/rtorr/sea/internal/manifest"
	"github.com/rtorr/sea/internal/registry"
	"github.com/spf13/cobra"
)

// runPublishCI triggers CI builds for the current package and its dependencies.
// With --watch, it waits for each tier to complete before proceeding.
func runPublishCI(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	regName := m.Publish.Registry
	if r, _ := cmd.Flags().GetString("registry"); r != "" {
		regName = r
	}
	if regName == "" {
		return fmt.Errorf("no publish registry specified")
	}

	remote := cfg.FindRemote(regName)
	if remote == nil {
		return fmt.Errorf("registry %q not found", regName)
	}
	if remote.Type != "github-releases" {
		return fmt.Errorf("--ci only works with github-releases registries (got %q)", remote.Type)
	}

	repo := remote.URL
	token := resolveGitHubToken(remote)
	if token == "" {
		return fmt.Errorf("no GitHub token available (set GITHUB_TOKEN or run 'gh auth login')")
	}

	watchFlag, _ := cmd.Flags().GetBool("watch")

	// Build dependency graph and check what needs publishing
	multi, err := registry.NewMulti(cfg)
	if err != nil {
		return fmt.Errorf("initializing registries: %w", err)
	}

	// Target platforms we want published
	targetABIs := []string{"darwin-aarch64-libcxx", "linux-x86_64-libstdcxx", "windows-x86_64-msvc"}

	// Compute which deps need publishing
	pkg := fmt.Sprintf("%s/%s", m.Package.Name, m.Package.Version)
	depTiers := computeDepTiers(cmd, m, multi, targetABIs)

	if len(depTiers) > 0 {
		cmd.Printf("Dependencies need publishing first:\n")
		for i, tier := range depTiers {
			cmd.Printf("  Tier %d: %s\n", i, strings.Join(tier, ", "))
		}
		cmd.Println()

		if !watchFlag {
			cmd.Println("Use --watch to automatically build dependencies in order.")
			cmd.Println("Or publish them manually first.")
			return nil
		}

		// Trigger and wait for each dep tier
		for i, tier := range depTiers {
			cmd.Printf("=== Building dependency tier %d ===\n", i)
			for _, dep := range tier {
				if err := triggerWorkflow(repo, token, dep); err != nil {
					return fmt.Errorf("triggering %s: %w", dep, err)
				}
				cmd.Printf("  Triggered: %s\n", dep)
			}

			cmd.Println("  Waiting for tier to complete...")
			if err := waitForRuns(repo, len(tier)); err != nil {
				cmd.Printf("  WARNING: some jobs failed in tier %d\n", i)
			} else {
				cmd.Printf("  Tier %d complete\n", i)
			}
			cmd.Println()
		}
	}

	// Now trigger the package itself
	cmd.Printf("=== Building %s ===\n", pkg)
	if err := triggerWorkflow(repo, token, pkg); err != nil {
		return fmt.Errorf("triggering %s: %w", pkg, err)
	}
	cmd.Printf("Triggered CI build for %s on all platforms\n", pkg)

	if !watchFlag {
		cmd.Printf("View at: https://github.com/%s/actions\n", repo)
		return nil
	}

	cmd.Println("Waiting for build to complete...")
	if err := waitForRuns(repo, 1); err != nil {
		return fmt.Errorf("CI build failed: %w", err)
	}

	cmd.Printf("CI build complete for %s\n", pkg)
	return nil
}

// computeDepTiers determines which dependencies need CI builds and in what order.
// Returns tiers of package paths (e.g., [["gflags/2.2.0"], ["glog/0.3.5"]]).
func computeDepTiers(cmd *cobra.Command, m *manifest.Manifest, multi *registry.Multi, targetABIs []string) [][]string {
	if len(m.Dependencies) == 0 {
		return nil
	}

	// For each dependency, check if it's published for all target ABIs
	type depInfo struct {
		name    string
		version string
		path    string // name/version
		missing bool
		deps    []string // names of its dependencies
	}

	var allDeps []depInfo

	for name, dep := range m.Dependencies {
		// Resolve the best available version
		versions, err := multi.ListVersions(name)
		if err != nil || len(versions) == 0 {
			continue
		}

		// Find the best version that matches the constraint
		// For simplicity, use the latest version (the resolver handles constraints properly)
		bestVer := versions[len(versions)-1]
		pkgPath := fmt.Sprintf("%s/%s", name, bestVer)
		_ = dep

		// Check if published for all target ABIs
		tags, err := multi.ListABITagsFromAny(name, bestVer)
		if err != nil {
			allDeps = append(allDeps, depInfo{name: name, version: bestVer, path: pkgPath, missing: true})
			continue
		}

		tagSet := make(map[string]bool)
		for _, t := range tags {
			tagSet[t] = true
		}

		missing := false
		for _, target := range targetABIs {
			if !tagSet[target] && !tagSet["any"] {
				missing = true
				break
			}
		}

		if missing {
			// Also read this dep's dependencies for ordering
			var depDeps []string
			for _, reg := range multi.Registries() {
				for _, tag := range tags {
					meta, err := reg.FetchMeta(name, bestVer, tag)
					if err != nil {
						continue
					}
					for _, d := range meta.Dependencies {
						depDeps = append(depDeps, d.Name)
					}
					break
				}
				if len(depDeps) > 0 {
					break
				}
			}

			allDeps = append(allDeps, depInfo{
				name: name, version: bestVer, path: pkgPath,
				missing: true, deps: depDeps,
			})
		}
	}

	if len(allDeps) == 0 {
		return nil
	}

	// Topological sort into tiers
	tierOf := make(map[string]int)
	nameToPath := make(map[string]string)
	for _, d := range allDeps {
		tierOf[d.name] = 0
		nameToPath[d.name] = d.path
	}

	changed := true
	for changed {
		changed = false
		for _, d := range allDeps {
			for _, depName := range d.deps {
				if depTier, exists := tierOf[depName]; exists {
					needed := depTier + 1
					if needed > tierOf[d.name] {
						tierOf[d.name] = needed
						changed = true
					}
				}
			}
		}
	}

	// Group by tier
	maxTier := 0
	for _, t := range tierOf {
		if t > maxTier {
			maxTier = t
		}
	}

	var tiers [][]string
	for t := 0; t <= maxTier; t++ {
		var tier []string
		for _, d := range allDeps {
			if tierOf[d.name] == t {
				tier = append(tier, d.path)
			}
		}
		if len(tier) > 0 {
			tiers = append(tiers, tier)
		}
	}

	return tiers
}

func triggerWorkflow(repo, token, pkg string) error {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/build-packages.yml/dispatches", repo)
	body := fmt.Sprintf(`{"ref":"main","inputs":{"package":"%s"}}`, pkg)

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 {
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
}

func waitForRuns(repo string, expectedCount int) error {
	time.Sleep(5 * time.Second)

	// Use gh CLI to watch — it handles streaming and exit codes
	args := []string{"run", "list",
		"--repo", repo,
		"--event", "workflow_dispatch",
		"--limit", fmt.Sprintf("%d", expectedCount),
		"--json", "databaseId,status",
		"--jq", `.[] | select(.status != "completed") | .databaseId`,
	}
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		return fmt.Errorf("listing runs: %w", err)
	}

	runIDs := strings.Fields(strings.TrimSpace(string(out)))
	if len(runIDs) == 0 {
		return nil // all already completed
	}

	for _, runID := range runIDs {
		watchCmd := exec.Command("gh", "run", "watch", runID, "--repo", repo, "--exit-status")
		watchCmd.Stdout = os.Stdout
		watchCmd.Stderr = os.Stderr
		if err := watchCmd.Run(); err != nil {
			return fmt.Errorf("run %s failed: %w", runID, err)
		}
	}

	return nil
}

func resolveGitHubToken(remote *config.Remote) string {
	token := os.Getenv("GITHUB_TOKEN")
	if remote.TokenEnv != "" {
		if t := os.Getenv(remote.TokenEnv); t != "" {
			token = t
		}
	}
	if token == "" {
		if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
			token = strings.TrimSpace(string(out))
		}
	}
	return token
}
