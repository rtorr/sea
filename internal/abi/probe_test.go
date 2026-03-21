package abi

import (
	"runtime"
	"testing"
)

func TestProbeToolchain(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("probe test requires Unix-like system with g++")
	}

	fp, err := ProbeToolchain("g++", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("ProbeToolchain failed: %v", err)
	}

	t.Logf("OS:           %s", fp.OS)
	t.Logf("Arch:         %s", fp.Arch)
	t.Logf("Mangling:     %s", fp.ManglingScheme)
	t.Logf("StdlibABI:    %s", fp.StdlibABI)
	t.Logf("ExceptionABI: %s", fp.ExceptionABI)
	t.Logf("Hash:         %s", fp.Hash)

	if fp.Hash == "" {
		t.Error("fingerprint hash should not be empty")
	}
	if fp.ManglingScheme != "itanium" {
		t.Errorf("expected itanium mangling on %s, got %s", runtime.GOOS, fp.ManglingScheme)
	}
	if fp.StdlibABI == "" {
		t.Error("stdlib ABI hash should not be empty")
	}

	// Probing the same compiler twice should produce the same fingerprint
	fp2, err := ProbeToolchain("g++", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("second probe failed: %v", err)
	}
	if fp.Hash != fp2.Hash {
		t.Errorf("fingerprint should be deterministic: %s != %s", fp.Hash, fp2.Hash)
	}
}
