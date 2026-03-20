package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/rtorr/sea/internal/archive"
)

const (
	httpTimeout     = 30 * time.Second
	downloadTimeout = 5 * time.Minute
)

// Artifactory implements Registry for Artifactory Generic repos.
type Artifactory struct {
	name       string
	baseURL    string
	repository string
	tokenEnv   string
	client     *http.Client
	dlClient   *http.Client // longer timeout for downloads
}

// NewArtifactory creates an Artifactory registry.
func NewArtifactory(name, baseURL, repository, tokenEnv string) (*Artifactory, error) {
	// Validate URL
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Artifactory URL %q: %w", baseURL, err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("Artifactory URL must use http or https scheme, got %q", parsed.Scheme)
	}

	return &Artifactory{
		name:       name,
		baseURL:    strings.TrimRight(baseURL, "/"),
		repository: repository,
		tokenEnv:   tokenEnv,
		client:     &http.Client{Timeout: httpTimeout},
		dlClient:   &http.Client{Timeout: downloadTimeout},
	}, nil
}

func (a *Artifactory) Name() string { return a.name }

func (a *Artifactory) token() string {
	if a.tokenEnv != "" {
		return os.Getenv(a.tokenEnv)
	}
	return ""
}

func (a *Artifactory) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	reqURL := fmt.Sprintf("%s/%s", a.baseURL, path)
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if tok := a.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.Header.Set("User-Agent", "sea/0.1")

	client := a.client
	if method == "GET" && strings.HasSuffix(path, ".tar.zst") {
		client = a.dlClient
	}

	resp, err := retryDo(client, req)
	if err != nil {
		return nil, fmt.Errorf("request to %s: %w", reqURL, err)
	}
	return resp, nil
}

func (a *Artifactory) packagePath(pkg, version, abiTag, filename string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", a.repository, pkg, version, abiTag, filename)
}

func (a *Artifactory) ListVersions(pkg string) ([]string, error) {
	path := fmt.Sprintf("api/storage/%s/%s/", a.repository, pkg)
	resp, err := a.doRequest("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("listing versions for %s: %w", pkg, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing versions for %s: HTTP %d", pkg, resp.StatusCode)
	}

	return a.parseChildren(resp.Body)
}

func (a *Artifactory) ListABITags(pkg, version string) ([]string, error) {
	path := fmt.Sprintf("api/storage/%s/%s/%s/", a.repository, pkg, version)
	resp, err := a.doRequest("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("listing ABI tags for %s@%s: %w", pkg, version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing ABI tags for %s@%s: HTTP %d", pkg, version, resp.StatusCode)
	}

	return a.parseChildren(resp.Body)
}

// parseChildren extracts folder names from Artifactory storage API response.
func (a *Artifactory) parseChildren(body io.Reader) ([]string, error) {
	var result struct {
		Children []struct {
			URI    string `json:"uri"`
			Folder bool   `json:"folder"`
		} `json:"children"`
	}
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parsing Artifactory response: %w", err)
	}

	var names []string
	for _, child := range result.Children {
		if child.Folder {
			names = append(names, strings.TrimPrefix(child.URI, "/"))
		}
	}
	return names, nil
}

func (a *Artifactory) FetchMeta(pkg, version, abiTag string) (*archive.PackageMeta, error) {
	path := a.packagePath(pkg, version, abiTag, archive.PackageMetaFile)
	resp, err := a.doRequest("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching metadata for %s@%s: %w", pkg, version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching metadata for %s@%s: HTTP %d", pkg, version, resp.StatusCode)
	}

	var meta archive.PackageMeta
	if _, err := toml.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("parsing metadata for %s@%s: %w", pkg, version, err)
	}
	return &meta, nil
}

func (a *Artifactory) Download(pkg, version, abiTag string) (io.ReadCloser, error) {
	archiveName := fmt.Sprintf("%s-%s-%s.tar.zst", pkg, version, abiTag)
	path := a.packagePath(pkg, version, abiTag, archiveName)
	resp, err := a.doRequest("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("downloading %s@%s: %w", pkg, version, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("downloading %s@%s: HTTP %d", pkg, version, resp.StatusCode)
	}
	return resp.Body, nil
}

func (a *Artifactory) Upload(pkg, version, abiTag string, data io.Reader, meta *archive.PackageMeta) error {
	// Upload archive
	archiveName := fmt.Sprintf("%s-%s-%s.tar.zst", pkg, version, abiTag)
	archivePath := a.packagePath(pkg, version, abiTag, archiveName)
	resp, err := a.doRequest("PUT", archivePath, data)
	if err != nil {
		return fmt.Errorf("uploading archive for %s@%s: %w", pkg, version, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("uploading archive for %s@%s: HTTP %d", pkg, version, resp.StatusCode)
	}

	// Upload metadata sidecar
	metaBytes, err := MetaToTOML(meta)
	if err != nil {
		return fmt.Errorf("encoding metadata for %s@%s: %w", pkg, version, err)
	}
	metaPath := a.packagePath(pkg, version, abiTag, archive.PackageMetaFile)
	resp, err = a.doRequest("PUT", metaPath, strings.NewReader(string(metaBytes)))
	if err != nil {
		return fmt.Errorf("uploading metadata for %s@%s: %w", pkg, version, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("uploading metadata for %s@%s: HTTP %d", pkg, version, resp.StatusCode)
	}

	return nil
}

func (a *Artifactory) Search(query string) ([]SearchResult, error) {
	escapedQuery := url.QueryEscape(query)
	path := fmt.Sprintf("api/search/prop?sea.name=%s&repos=%s", escapedQuery, url.QueryEscape(a.repository))
	resp, err := a.doRequest("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("searching %s: %w", a.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var result struct {
		Results []struct {
			URI string `json:"uri"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil
	}

	seen := make(map[string]bool)
	var results []SearchResult
	for _, r := range result.Results {
		parts := strings.Split(r.URI, "/")
		for i, p := range parts {
			if p == a.repository && i+1 < len(parts) {
				name := parts[i+1]
				if !seen[name] {
					seen[name] = true
					versions, _ := a.ListVersions(name)
					results = append(results, SearchResult{
						Name:     name,
						Versions: versions,
						Registry: a.name,
					})
				}
			}
		}
	}

	return results, nil
}

func (a *Artifactory) FetchVersionManifest(pkg, version string) (*archive.VersionManifest, error) {
	path := fmt.Sprintf("%s/%s/%s/%s", a.repository, pkg, version, archive.VersionManifestFile)
	resp, err := a.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching version manifest: HTTP %d", resp.StatusCode)
	}
	var vm archive.VersionManifest
	if _, err := toml.NewDecoder(resp.Body).Decode(&vm); err != nil {
		return nil, fmt.Errorf("parsing version manifest: %w", err)
	}
	return &vm, nil
}

func (a *Artifactory) UploadVersionManifest(pkg, version string, vm *archive.VersionManifest) error {
	// Read-modify-write
	existing, _ := a.FetchVersionManifest(pkg, version)
	if existing != nil {
		existing.Merge(vm)
		vm = existing
	}
	data, err := MetaToTOML2(vm)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("%s/%s/%s/%s", a.repository, pkg, version, archive.VersionManifestFile)
	resp, err := a.doRequest("PUT", path, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
