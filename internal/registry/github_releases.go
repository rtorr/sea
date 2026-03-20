package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/rtorr/sea/internal/archive"
)

// GitHubReleases implements Registry using a single GitHub repo where each
// package version is a release tagged "{pkg}/v{version}". Assets are the
// .tar.zst archives and sea-package.toml metadata files. No git clone needed —
// everything is fetched via the GitHub Releases API.
type GitHubReleases struct {
	name     string
	owner    string // "owner/repo" e.g. "rtorr/sea-packages"
	repo     string
	tokenEnv string
	client   *http.Client
	dlClient *http.Client
}

// NewGitHubReleases creates a registry backed by GitHub Releases on a single repo.
// url should be "github.com/owner/repo" or "owner/repo".
func NewGitHubReleases(name, rawURL, tokenEnv string) (*GitHubReleases, error) {
	u := rawURL
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "github.com/")
	u = strings.TrimRight(u, "/")

	parts := strings.SplitN(u, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("github-releases remote %q: URL must be owner/repo (e.g. rtorr/sea-packages)", name)
	}

	return &GitHubReleases{
		name:     name,
		owner:    parts[0],
		repo:     parts[1],
		tokenEnv: tokenEnv,
		client:   &http.Client{Timeout: httpTimeout},
		dlClient: &http.Client{Timeout: downloadTimeout},
	}, nil
}

func (g *GitHubReleases) Name() string { return g.name }

func (g *GitHubReleases) token() string {
	// 1. Explicit env var from config
	if g.tokenEnv != "" {
		if tok := os.Getenv(g.tokenEnv); tok != "" {
			return tok
		}
	}
	// 2. Standard GITHUB_TOKEN env var
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		return tok
	}
	// 3. Auto-detect from gh CLI (if installed)
	if tok := ghAuthToken(); tok != "" {
		return tok
	}
	return ""
}

func (g *GitHubReleases) doAPI(method, path string, body io.Reader) (*http.Response, error) {
	apiURL := "https://api.github.com" + path
	req, err := http.NewRequest(method, apiURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sea/0.1")
	if tok := g.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if body != nil && method != "GET" {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	return retryDo(g.client, req)
}

// releaseTag returns the git tag for a package version: "{pkg}/v{version}"
func (g *GitHubReleases) releaseTag(pkg, version string) string {
	return fmt.Sprintf("%s/v%s", pkg, version)
}

func (g *GitHubReleases) ListVersions(pkg string) ([]string, error) {
	// List all releases and filter by tag prefix
	resp, err := g.doAPI("GET", fmt.Sprintf("/repos/%s/%s/releases?per_page=100", g.owner, g.repo), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var releases []struct {
		TagName string `json:"tag_name"`
		Draft   bool   `json:"draft"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	prefix := pkg + "/v"
	var versions []string
	for _, r := range releases {
		if r.Draft {
			continue
		}
		if strings.HasPrefix(r.TagName, prefix) {
			versions = append(versions, strings.TrimPrefix(r.TagName, prefix))
		}
	}
	return versions, nil
}

func (g *GitHubReleases) ListABITags(pkg, version string) ([]string, error) {
	assets, err := g.listAssets(pkg, version)
	if err != nil || len(assets) == 0 {
		return nil, err
	}

	// Extract ABI tags from asset names: {pkg}-{version}-{abi_tag}.tar.zst
	prefix := fmt.Sprintf("%s-%s-", pkg, version)
	suffix := ".tar.zst"
	var tags []string
	for _, a := range assets {
		if strings.HasPrefix(a.Name, prefix) && strings.HasSuffix(a.Name, suffix) {
			tag := strings.TrimPrefix(a.Name, prefix)
			tag = strings.TrimSuffix(tag, suffix)
			if tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	return tags, nil
}

func (g *GitHubReleases) FetchMeta(pkg, version, abiTag string) (*archive.PackageMeta, error) {
	// Meta file is named: sea-package-{abi_tag}.toml
	assetName := fmt.Sprintf("sea-package-%s.toml", abiTag)
	rc, err := g.downloadAsset(pkg, version, assetName)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var meta archive.PackageMeta
	if _, err := toml.NewDecoder(rc).Decode(&meta); err != nil {
		return nil, fmt.Errorf("parsing metadata: %w", err)
	}
	return &meta, nil
}

func (g *GitHubReleases) Download(pkg, version, abiTag string) (io.ReadCloser, error) {
	assetName := fmt.Sprintf("%s-%s-%s.tar.zst", pkg, version, abiTag)
	return g.downloadAsset(pkg, version, assetName)
}

func (g *GitHubReleases) Upload(pkg, version, abiTag string, data io.Reader, meta *archive.PackageMeta) error {
	tag := g.releaseTag(pkg, version)

	// Get or create the release
	releaseID, err := g.getOrCreateRelease(tag, fmt.Sprintf("%s %s", pkg, version))
	if err != nil {
		return fmt.Errorf("creating release: %w", err)
	}

	// Upload the archive
	archiveName := fmt.Sprintf("%s-%s-%s.tar.zst", pkg, version, abiTag)
	archiveData, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("reading archive data: %w", err)
	}
	if err := g.uploadAsset(releaseID, archiveName, archiveData); err != nil {
		return fmt.Errorf("uploading archive: %w", err)
	}

	// Upload metadata
	metaName := fmt.Sprintf("sea-package-%s.toml", abiTag)
	metaBytes, err := MetaToTOML(meta)
	if err != nil {
		return fmt.Errorf("encoding metadata: %w", err)
	}
	if err := g.uploadAsset(releaseID, metaName, metaBytes); err != nil {
		return fmt.Errorf("uploading metadata: %w", err)
	}

	return nil
}

func (g *GitHubReleases) Search(query string) ([]SearchResult, error) {
	// List all releases and extract unique package names
	resp, err := g.doAPI("GET", fmt.Sprintf("/repos/%s/%s/releases?per_page=100", g.owner, g.repo), nil)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var releases []struct {
		TagName string `json:"tag_name"`
		Draft   bool   `json:"draft"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, nil
	}

	query = strings.ToLower(query)
	pkgVersions := make(map[string][]string)
	for _, r := range releases {
		if r.Draft {
			continue
		}
		parts := strings.SplitN(r.TagName, "/v", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[0]
		ver := parts[1]
		if strings.Contains(strings.ToLower(name), query) {
			pkgVersions[name] = append(pkgVersions[name], ver)
		}
	}

	var results []SearchResult
	for name, versions := range pkgVersions {
		results = append(results, SearchResult{
			Name:     name,
			Versions: versions,
			Registry: g.name,
		})
	}
	return results, nil
}

func (g *GitHubReleases) FetchVersionManifest(pkg, version string) (*archive.VersionManifest, error) {
	rc, err := g.downloadAsset(pkg, version, archive.VersionManifestFile)
	if err != nil {
		return nil, nil
	}
	defer rc.Close()

	var vm archive.VersionManifest
	if _, err := toml.NewDecoder(rc).Decode(&vm); err != nil {
		return nil, nil
	}
	return &vm, nil
}

func (g *GitHubReleases) UploadVersionManifest(pkg, version string, vm *archive.VersionManifest) error {
	tag := g.releaseTag(pkg, version)
	releaseID, err := g.getOrCreateRelease(tag, fmt.Sprintf("%s %s", pkg, version))
	if err != nil {
		return err
	}

	// Merge with existing manifest if present
	existing, _ := g.FetchVersionManifest(pkg, version)
	if existing != nil {
		existing.Merge(vm)
		vm = existing
		// Delete old asset before uploading new one
		g.deleteAsset(releaseID, archive.VersionManifestFile)
	}

	data, err := MetaToTOML2(vm)
	if err != nil {
		return err
	}
	return g.uploadAsset(releaseID, archive.VersionManifestFile, data)
}

// ── internal helpers ──

type ghAsset struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func (g *GitHubReleases) listAssets(pkg, version string) ([]ghAsset, error) {
	tag := g.releaseTag(pkg, version)
	resp, err := g.doAPI("GET", fmt.Sprintf("/repos/%s/%s/releases/tags/%s", g.owner, g.repo, tag), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var release struct {
		Assets []ghAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, nil
	}
	return release.Assets, nil
}

func (g *GitHubReleases) downloadAsset(pkg, version, assetName string) (io.ReadCloser, error) {
	assets, err := g.listAssets(pkg, version)
	if err != nil {
		return nil, err
	}

	for _, a := range assets {
		if a.Name == assetName {
			req, err := http.NewRequest("GET", a.BrowserDownloadURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "sea/0.1")
			if tok := g.token(); tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			resp, err := g.dlClient.Do(req)
			if err != nil {
				return nil, err
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return nil, fmt.Errorf("downloading %s: HTTP %d", assetName, resp.StatusCode)
			}
			return resp.Body, nil
		}
	}

	return nil, fmt.Errorf("asset %q not found in release %s/v%s", assetName, pkg, version)
}

func (g *GitHubReleases) getOrCreateRelease(tag, title string) (int, error) {
	// Try to get existing release
	resp, err := g.doAPI("GET", fmt.Sprintf("/repos/%s/%s/releases/tags/%s", g.owner, g.repo, tag), nil)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode == http.StatusOK {
		var release struct {
			ID int `json:"id"`
		}
		json.NewDecoder(resp.Body).Decode(&release)
		resp.Body.Close()
		return release.ID, nil
	}
	resp.Body.Close()

	// Create new release
	body := map[string]interface{}{
		"tag_name": tag,
		"name":     title,
		"draft":    false,
	}
	bodyBytes, _ := json.Marshal(body)
	resp, err = g.doAPI("POST", fmt.Sprintf("/repos/%s/%s/releases", g.owner, g.repo), bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("creating release: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var release struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return 0, err
	}
	return release.ID, nil
}

func (g *GitHubReleases) uploadAsset(releaseID int, name string, data []byte) error {
	resp, err := g.doUploadAsset(releaseID, name, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// If asset already exists (422), delete it and retry
	if resp.StatusCode == 422 {
		resp.Body.Close()
		g.deleteAsset(releaseID, name)
		resp, err = g.doUploadAsset(releaseID, name, data)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("uploading asset %s: HTTP %d: %s", name, resp.StatusCode, string(respBody))
	}
	return nil
}

func (g *GitHubReleases) doUploadAsset(releaseID int, name string, data []byte) (*http.Response, error) {
	uploadURL := fmt.Sprintf("https://uploads.github.com/repos/%s/%s/releases/%d/assets?name=%s",
		g.owner, g.repo, releaseID, name)
	req, err := http.NewRequest("POST", uploadURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", "sea/0.1")
	if tok := g.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.ContentLength = int64(len(data))
	return g.client.Do(req)
}

func (g *GitHubReleases) deleteAsset(releaseID int, name string) {
	// List assets on the release
	resp, err := g.doAPI("GET", fmt.Sprintf("/repos/%s/%s/releases/%d/assets", g.owner, g.repo, releaseID), nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var assets []ghAsset
	json.NewDecoder(resp.Body).Decode(&assets)
	for _, a := range assets {
		if a.Name == name {
			delResp, err := g.doAPI("DELETE", fmt.Sprintf("/repos/%s/%s/releases/assets/%d", g.owner, g.repo, a.ID), nil)
			if err == nil {
				delResp.Body.Close()
			}
			return
		}
	}
}

// ghAuthToken runs `gh auth token` to get a GitHub token from the gh CLI.
// Returns empty string if gh is not installed or not authenticated.
func ghAuthToken() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
