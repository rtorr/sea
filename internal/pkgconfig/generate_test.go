package pkgconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateBasic(t *testing.T) {
	pc := &PCFile{
		Name:        "zlib",
		Description: "Installed by sea",
		Version:     "1.3.1",
		Prefix:      "/home/user/project/sea_packages/zlib",
		Libs:        []string{"-lz"},
	}

	got := Generate(pc)

	// Check prefix variable
	if !strings.Contains(got, "prefix=/home/user/project/sea_packages/zlib\n") {
		t.Errorf("expected prefix line, got:\n%s", got)
	}
	// Check variable substitution in includedir
	if !strings.Contains(got, "includedir=${prefix}/include\n") {
		t.Errorf("expected includedir with ${prefix}, got:\n%s", got)
	}
	// Check variable substitution in libdir
	if !strings.Contains(got, "libdir=${prefix}/lib\n") {
		t.Errorf("expected libdir with ${prefix}, got:\n%s", got)
	}
	// Check Name field
	if !strings.Contains(got, "Name: zlib\n") {
		t.Errorf("expected Name: zlib, got:\n%s", got)
	}
	// Check Version field
	if !strings.Contains(got, "Version: 1.3.1\n") {
		t.Errorf("expected Version: 1.3.1, got:\n%s", got)
	}
	// Check Cflags uses variable
	if !strings.Contains(got, "Cflags: -I${includedir}") {
		t.Errorf("expected Cflags with ${includedir}, got:\n%s", got)
	}
	// Check Libs uses variable and -lz
	if !strings.Contains(got, "Libs: -L${libdir} -lz") {
		t.Errorf("expected Libs with ${libdir} -lz, got:\n%s", got)
	}
}

func TestGenerateMultipleLibs(t *testing.T) {
	pc := &PCFile{
		Name:        "openssl",
		Description: "OpenSSL",
		Version:     "3.2.0",
		Prefix:      "/opt/sea_packages/openssl",
		Libs:        []string{"-lssl", "-lcrypto"},
	}

	got := Generate(pc)

	if !strings.Contains(got, "-lssl -lcrypto") {
		t.Errorf("expected both -lssl and -lcrypto, got:\n%s", got)
	}
}

func TestGeneratePrefixSubstitution(t *testing.T) {
	pc := &PCFile{
		Name:        "foo",
		Description: "Test",
		Version:     "1.0.0",
		Prefix:      "/a/b/c",
	}

	got := Generate(pc)

	// The actual prefix should appear only once (in the variable definition)
	count := strings.Count(got, "/a/b/c")
	if count != 1 {
		t.Errorf("expected prefix path to appear exactly once (in variable), appeared %d times:\n%s", count, got)
	}

	// ${prefix} should be used in includedir and libdir
	if strings.Count(got, "${prefix}") < 2 {
		t.Errorf("expected ${prefix} to appear at least twice (includedir and libdir), got:\n%s", got)
	}
}

func TestWriteForPackage(t *testing.T) {
	pkgDir := t.TempDir()

	// Create include and lib directories with a library
	os.MkdirAll(filepath.Join(pkgDir, "include"), 0o755)
	os.MkdirAll(filepath.Join(pkgDir, "lib"), 0o755)
	os.WriteFile(filepath.Join(pkgDir, "lib", "libfoo.a"), []byte("fake"), 0o644)

	err := WriteForPackage(pkgDir, "foo", "2.1.0")
	if err != nil {
		t.Fatalf("WriteForPackage: %v", err)
	}

	// Check the .pc file was created
	pcPath := filepath.Join(pkgDir, "lib", "pkgconfig", "foo.pc")
	data, err := os.ReadFile(pcPath)
	if err != nil {
		t.Fatalf("reading generated .pc file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Name: foo") {
		t.Errorf("expected Name: foo in .pc file")
	}
	if !strings.Contains(content, "Version: 2.1.0") {
		t.Errorf("expected Version: 2.1.0 in .pc file")
	}
	if !strings.Contains(content, "-lfoo") {
		t.Errorf("expected -lfoo in .pc file")
	}
	if !strings.Contains(content, "prefix="+pkgDir) {
		t.Errorf("expected prefix=%s in .pc file", pkgDir)
	}
}

func TestWriteForPackageNoLibOrInclude(t *testing.T) {
	pkgDir := t.TempDir()
	// No include or lib directories — should be a no-op
	err := WriteForPackage(pkgDir, "empty", "1.0.0")
	if err != nil {
		t.Fatalf("WriteForPackage should not error for packages without lib/include: %v", err)
	}

	pcPath := filepath.Join(pkgDir, "lib", "pkgconfig", "empty.pc")
	if _, err := os.Stat(pcPath); err == nil {
		t.Errorf("expected no .pc file to be generated for package without lib/include")
	}
}
