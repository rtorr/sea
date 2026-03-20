package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/rtorr/sea/internal/archive"
)

// GitHub implements Registry for GitHub Releases.
type GitHub struct {
	name     string
	owner    string
	tokenEnv string
	client   *http.Client
	dlClient *http.Client
}

// NewGitHub creates a GitHub Releases registry.
// url should be "github.com/owner" or just "owner".
func NewGitHub(name, rawURL, tokenEnv string) (*GitHub, error) {
	owner := rawURL
	owner = strings.TrimPrefix(owner, "https://")
	owner = strings.TrimPrefix(owner, "http://")
	owner = strings.TrimPrefix(owner, "github.com/")
	owner = strings.TrimRight(owner, "/")
	if owner == "" {
		return nil, fmt.Errorf("github remote %q: owner cannot be empty", name)
	}

	return &GitHub{
		name:     name,
		owner:    owner,
		tokenEnv: tokenEnv,
		client:   &http.Client{Timeout: httpTimeout},
		dlClient: &http.Client{Timeout: downloadTimeout},
	}, nil
}

func (g *GitHub) Name() string { return g.name }

func (g *GitHub) token() string {
	if g.tokenEnv != "" {
		return os.Getenv(g.tokenEnv)
	}
	return ""
}

func (g *GitHub) doAPI(path string) (*http.Response, error) {
	apiURL := "https://api.github.com" + path
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sea/0.1")
	if tok := g.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := retryDo(g.client, req)
	if err != nil {
		return nil, fmt.Errorf("request to GitHub API: %w", err)
	}
	return resp, nil
}

func (g *GitHub) ListVersions(pkg string) ([]string, error) {
	resp, err := g.doAPI(fmt.Sprintf("/repos/%s/%s/releases?per_page=100", g.owner, pkg))
	if err != nil {
		return nil, fmt.Errorf("listing releases for %s: %w", pkg, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing releases for %s: HTTP %d", pkg, resp.StatusCode)
	}

	var releases []struct {
		TagName string `json:"tag_name"`
		Draft   bool   `json:"draft"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("parsing releases for %s: %w", pkg, err)
	}

	var versions []string
	for _, r := range releases {
		if r.Draft {
			continue
		}
		v := strings.TrimPrefix(r.TagName, "v")
		versions = append(versions, v)
	}
	return versions, nil
}

func (g *GitHub) ListABITags(pkg, version string) ([]string, error) {
	// Try both with and without v prefix
	for _, tag := range []string{"v" + version, version} {
		resp, err := g.doAPI(fmt.Sprintf("/repos/%s/%s/releases/tags/%s", g.owner, pkg, tag))
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var release struct {
			Assets []struct {
				Name string `json:"name"`
			} `json:"assets"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return nil, nil
		}

		prefix := fmt.Sprintf("%s-%s-", pkg, version)
		suffix := ".tar.zst"
		var tags []string
		for _, a := range release.Assets {
			if strings.HasPrefix(a.Name, prefix) && strings.HasSuffix(a.Name, suffix) {
				t := strings.TrimPrefix(a.Name, prefix)
				t = strings.TrimSuffix(t, suffix)
				if t != "" {
					tags = append(tags, t)
				}
			}
		}
		return tags, nil
	}
	return nil, nil
}

func (g *GitHub) FetchMeta(pkg, version, abiTag string) (*archive.PackageMeta, error) {
	// The meta file is stored as: sea-package-{abi_tag}.toml
	assetName := fmt.Sprintf("sea-package-%s.toml", abiTag)
	rc, err := g.downloadAsset(pkg, version, assetName)
	if err != nil {
		return nil, fmt.Errorf("fetching metadata for %s@%s: %w", pkg, version, err)
	}
	defer rc.Close()

	var meta archive.PackageMeta
	if _, err := toml.NewDecoder(rc).Decode(&meta); err != nil {
		return nil, fmt.Errorf("parsing metadata for %s@%s: %w", pkg, version, err)
	}
	return &meta, nil
}

func (g *GitHub) Download(pkg, version, abiTag string) (io.ReadCloser, error) {
	assetName := fmt.Sprintf("%s-%s-%s.tar.zst", pkg, version, abiTag)
	return g.downloadAsset(pkg, version, assetName)
}

func (g *GitHub) downloadAsset(pkg, version, assetName string) (io.ReadCloser, error) {
	// Try both tag formats
	for _, tag := range []string{"v" + version, version} {
		resp, err := g.doAPI(fmt.Sprintf("/repos/%s/%s/releases/tags/%s", g.owner, pkg, tag))
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var release struct {
			Assets []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			} `json:"assets"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		for _, a := range release.Assets {
			if a.Name == assetName {
				dlReq, err := http.NewRequest("GET", a.BrowserDownloadURL, nil)
				if err != nil {
					return nil, fmt.Errorf("creating download request: %w", err)
				}
				dlReq.Header.Set("User-Agent", "sea/0.1")
				if tok := g.token(); tok != "" {
					dlReq.Header.Set("Authorization", "Bearer "+tok)
				}
				dlResp, err := g.dlClient.Do(dlReq)
				if err != nil {
					return nil, fmt.Errorf("downloading asset %s: %w", assetName, err)
				}
				if dlResp.StatusCode != http.StatusOK {
					dlResp.Body.Close()
					return nil, fmt.Errorf("downloading asset %s: HTTP %d", assetName, dlResp.StatusCode)
				}
				return dlResp.Body, nil
			}
		}
	}

	return nil, fmt.Errorf("asset %q not found for %s@%s in %s", assetName, pkg, version, g.name)
}

func (g *GitHub) Upload(pkg, version, abiTag string, data io.Reader, meta *archive.PackageMeta) error {
	return fmt.Errorf("GitHub Releases upload is not supported via this tool — use 'gh release upload' or GitHub Actions")
}

func (g *GitHub) Search(query string) ([]SearchResult, error) {
	resp, err := g.doAPI(fmt.Sprintf("/search/repositories?q=%s+user:%s+topic:sea-package&per_page=20", query, g.owner))
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var result struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil
	}

	var results []SearchResult
	for _, item := range result.Items {
		versions, _ := g.ListVersions(item.Name)
		results = append(results, SearchResult{
			Name:     item.Name,
			Versions: versions,
			Registry: g.name,
		})
	}
	return results, nil
}

func (g *GitHub) FetchVersionManifest(pkg, version string) (*archive.VersionManifest, error) {
	rc, err := g.downloadAsset(pkg, version, archive.VersionManifestFile)
	if err != nil {
		return nil, nil // not found is fine
	}
	defer rc.Close()
	var vm archive.VersionManifest
	if _, err := toml.NewDecoder(rc).Decode(&vm); err != nil {
		return nil, nil
	}
	return &vm, nil
}

func (g *GitHub) UploadVersionManifest(pkg, version string, vm *archive.VersionManifest) error {
	return fmt.Errorf("GitHub version manifest upload not supported — use GitHub Actions to manage release assets")
}
