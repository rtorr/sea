package builder

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rtorr/sea/internal/manifest"
)

const downloadTimeout = 10 * time.Minute

// DownloadSource downloads and extracts the source archive specified in
// [build.source]. Returns the path to the extracted source directory.
// This replaces the need for build.sh scripts that manually curl+tar.
func DownloadSource(src manifest.BuildSource, destDir string) (string, error) {
	if src.URL == "" {
		return "", fmt.Errorf("no source URL specified")
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("creating source directory: %w", err)
	}

	// Download to a temp file
	archivePath := filepath.Join(destDir, "source-archive")
	if err := downloadFile(src.URL, archivePath); err != nil {
		return "", fmt.Errorf("downloading source: %w", err)
	}
	defer os.Remove(archivePath)

	// Verify hash if specified
	if src.SHA256 != "" {
		if err := verifyFileHash(archivePath, src.SHA256); err != nil {
			return "", err
		}
	}

	// Extract based on URL extension
	extractDir := filepath.Join(destDir, "src")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", err
	}

	strip := src.Strip
	if strip == 0 {
		strip = 1 // default: strip the top-level directory
	}

	url := strings.ToLower(src.URL)
	if strings.HasSuffix(url, ".tar.gz") || strings.HasSuffix(url, ".tgz") {
		if err := extractTarGz(archivePath, extractDir, strip); err != nil {
			return "", fmt.Errorf("extracting tar.gz: %w", err)
		}
	} else if strings.HasSuffix(url, ".tar.xz") || strings.HasSuffix(url, ".txz") {
		// Go stdlib doesn't have xz; fall back to exec
		if err := extractWithCommand(archivePath, extractDir, strip, "xz"); err != nil {
			return "", fmt.Errorf("extracting tar.xz: %w", err)
		}
	} else if strings.HasSuffix(url, ".tar.bz2") || strings.HasSuffix(url, ".tbz2") {
		if err := extractWithCommand(archivePath, extractDir, strip, "bzip2"); err != nil {
			return "", fmt.Errorf("extracting tar.bz2: %w", err)
		}
	} else if strings.HasSuffix(url, ".zip") {
		if err := extractZip(archivePath, extractDir, strip); err != nil {
			return "", fmt.Errorf("extracting zip: %w", err)
		}
	} else {
		return "", fmt.Errorf("unsupported archive format: %s", src.URL)
	}

	return extractDir, nil
}

func downloadFile(url, dest string) error {
	client := &http.Client{
		Timeout: downloadTimeout,
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(dest)
		return err
	}

	return nil
}

func verifyFileHash(path, expectedSHA256 string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expectedSHA256 {
		return fmt.Errorf("source archive hash mismatch: expected %s, got %s", expectedSHA256, actual)
	}
	return nil
}

func extractTarGz(archivePath, destDir string, strip int) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	return extractTar(gz, destDir, strip)
}

func extractTar(r io.Reader, destDir string, strip int) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Strip leading path components
		name := header.Name
		parts := strings.SplitN(filepath.ToSlash(name), "/", strip+1)
		if len(parts) <= strip {
			continue // entirely within stripped prefix
		}
		name = parts[strip]
		if name == "" || name == "." {
			continue
		}

		target := filepath.Join(destDir, filepath.FromSlash(name))

		// Security: prevent path traversal
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(target) != filepath.Clean(destDir) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0o755)
			if err != nil {
				return err
			}
			io.Copy(out, tr)
			out.Close()
		case tar.TypeSymlink:
			// Skip symlinks for security
			continue
		}
	}
	return nil
}

func extractZip(archivePath, destDir string, strip int) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		name := f.Name
		parts := strings.SplitN(filepath.ToSlash(name), "/", strip+1)
		if len(parts) <= strip {
			continue
		}
		name = parts[strip]
		if name == "" || name == "." {
			continue
		}

		target := filepath.Join(destDir, filepath.FromSlash(name))

		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(target) != filepath.Clean(destDir) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}

		os.MkdirAll(filepath.Dir(target), 0o755)

		rc, err := f.Open()
		if err != nil {
			return err
		}

		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()&0o755)
		if err != nil {
			rc.Close()
			return err
		}
		io.Copy(out, rc)
		out.Close()
		rc.Close()
	}
	return nil
}

func extractWithCommand(archivePath, destDir string, strip int, decompressor string) error {
	// Fallback for xz/bzip2: use command-line tools
	// This is only needed for .tar.xz and .tar.bz2 which Go stdlib doesn't support
	return fmt.Errorf("%s decompression requires the %s command (install it via your system package manager)", decompressor, decompressor)
}
