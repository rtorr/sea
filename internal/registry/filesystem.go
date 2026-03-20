package registry

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/rtorr/sea/internal/archive"
)

// semverDirRe is a basic check that a directory name looks like a version.
var semverDirRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// Filesystem implements Registry for local filesystem paths.
type Filesystem struct {
	name string
	root string
}

// NewFilesystem creates a filesystem registry at the given path.
func NewFilesystem(name, root string) (*Filesystem, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving path for %q: %w", name, err)
	}
	return &Filesystem{name: name, root: abs}, nil
}

func (f *Filesystem) Name() string { return f.name }

func (f *Filesystem) ListVersions(pkg string) ([]string, error) {
	pkgDir := filepath.Join(f.root, pkg)
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing versions for %s: %w", pkg, err)
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() && semverDirRe.MatchString(e.Name()) {
			versions = append(versions, e.Name())
		}
	}
	return versions, nil
}

func (f *Filesystem) ListABITags(pkg, version string) ([]string, error) {
	versionDir := filepath.Join(f.root, pkg, version)
	entries, err := os.ReadDir(versionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing ABI tags for %s@%s: %w", pkg, version, err)
	}
	var tags []string
	for _, e := range entries {
		if e.IsDir() {
			// Verify there's actually an archive or metadata in this directory
			metaPath := filepath.Join(versionDir, e.Name(), archive.PackageMetaFile)
			if _, err := os.Stat(metaPath); err == nil {
				tags = append(tags, e.Name())
			}
		}
	}
	return tags, nil
}

func (f *Filesystem) FetchMeta(pkg, version, abiTag string) (*archive.PackageMeta, error) {
	metaPath := filepath.Join(f.root, pkg, version, abiTag, archive.PackageMetaFile)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("reading metadata for %s@%s [%s]: %w", pkg, version, abiTag, err)
	}
	var meta archive.PackageMeta
	if err := toml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parsing metadata for %s@%s [%s]: %w", pkg, version, abiTag, err)
	}
	return &meta, nil
}

func (f *Filesystem) Download(pkg, version, abiTag string) (io.ReadCloser, error) {
	archiveName := fmt.Sprintf("%s-%s-%s.tar.zst", pkg, version, abiTag)
	archivePath := filepath.Join(f.root, pkg, version, abiTag, archiveName)
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("opening archive for %s@%s [%s]: %w", pkg, version, abiTag, err)
	}
	return file, nil
}

func (f *Filesystem) Upload(pkg, version, abiTag string, data io.Reader, meta *archive.PackageMeta) error {
	dir := filepath.Join(f.root, pkg, version, abiTag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating package directory: %w", err)
	}

	// Write archive to temp file first, then rename
	archiveName := fmt.Sprintf("%s-%s-%s.tar.zst", pkg, version, abiTag)
	archivePath := filepath.Join(dir, archiveName)
	tmpFile, err := os.CreateTemp(dir, ".sea-upload-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing archive: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, archivePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("finalizing archive: %w", err)
	}

	// Write metadata sidecar
	metaPath := filepath.Join(dir, archive.PackageMetaFile)
	metaBytes, err := MetaToTOML(meta)
	if err != nil {
		return fmt.Errorf("encoding metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, metaBytes, 0o644); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}

	return nil
}

func (f *Filesystem) Search(query string) ([]SearchResult, error) {
	entries, err := os.ReadDir(f.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("searching %s: %w", f.name, err)
	}

	query = strings.ToLower(query)
	var results []SearchResult
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.Contains(strings.ToLower(e.Name()), query) {
			versions, _ := f.ListVersions(e.Name())
			results = append(results, SearchResult{
				Name:     e.Name(),
				Versions: versions,
				Registry: f.name,
			})
		}
	}
	return results, nil
}

func (f *Filesystem) FetchVersionManifest(pkg, version string) (*archive.VersionManifest, error) {
	manifestPath := filepath.Join(f.root, pkg, version, archive.VersionManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading version manifest: %w", err)
	}
	var vm archive.VersionManifest
	if err := toml.Unmarshal(data, &vm); err != nil {
		return nil, fmt.Errorf("parsing version manifest: %w", err)
	}
	return &vm, nil
}

func (f *Filesystem) UploadVersionManifest(pkg, version string, vm *archive.VersionManifest) error {
	dir := filepath.Join(f.root, pkg, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating version directory: %w", err)
	}

	manifestPath := filepath.Join(dir, archive.VersionManifestFile)

	// Read-modify-write: merge with existing manifest
	existing, _ := f.FetchVersionManifest(pkg, version)
	if existing != nil {
		existing.Merge(vm)
		vm = existing
	}

	data, err := MetaToTOML2(vm)
	if err != nil {
		return fmt.Errorf("encoding version manifest: %w", err)
	}

	// Atomic write
	tmpFile, err := os.CreateTemp(dir, ".sea-manifest-*")
	if err != nil {
		return fmt.Errorf("creating temp manifest file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp manifest: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, manifestPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("finalizing version manifest: %w", err)
	}
	return nil
}

// MetaToTOML2 serializes any TOML-tagged struct to bytes.
func MetaToTOML2(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MetaToTOML serializes PackageMeta to TOML bytes.
func MetaToTOML(meta *archive.PackageMeta) ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(meta); err != nil {
		return nil, fmt.Errorf("encoding metadata: %w", err)
	}
	return buf.Bytes(), nil
}
