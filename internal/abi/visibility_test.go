package abi

import (
	"strings"
	"testing"
)

func TestCheckStaticLeaks(t *testing.T) {
	// Library exports these symbols
	libExports := []Symbol{
		{Name: "myapp_init"},
		{Name: "myapp_run"},
		{Name: "myapp_stop"},
		{Name: "cJSON_Parse"},          // leaked from cjson
		{Name: "cJSON_Delete"},         // leaked from cjson
		{Name: "LZ4_compress_default"}, // leaked from lz4
	}

	depSymbols := map[string][]string{
		"cjson": {"cJSON_Parse", "cJSON_Delete", "cJSON_Print"},
		"lz4":   {"LZ4_compress_default", "LZ4_decompress_safe"},
	}

	report := CheckStaticLeaks(libExports, depSymbols)

	if report.Clean {
		t.Fatal("expected leaks")
	}
	if report.TotalLeaked != 3 {
		t.Errorf("expected 3 leaked, got %d", report.TotalLeaked)
	}
	if len(report.Leaks["cjson"]) != 2 {
		t.Errorf("expected 2 cjson leaks, got %d", len(report.Leaks["cjson"]))
	}
	if len(report.Leaks["lz4"]) != 1 {
		t.Errorf("expected 1 lz4 leak, got %d", len(report.Leaks["lz4"]))
	}
}

func TestCheckStaticLeaksClean(t *testing.T) {
	libExports := []Symbol{
		{Name: "myapp_init"},
		{Name: "myapp_run"},
	}

	depSymbols := map[string][]string{
		"cjson": {"cJSON_Parse", "cJSON_Delete"},
	}

	report := CheckStaticLeaks(libExports, depSymbols)
	if !report.Clean {
		t.Fatal("expected clean — no dependency symbols in exports")
	}
}

func TestCheckStaticLeaksNoDeps(t *testing.T) {
	libExports := []Symbol{{Name: "foo"}}
	report := CheckStaticLeaks(libExports, nil)
	if !report.Clean {
		t.Fatal("expected clean when no dep symbols provided")
	}
}

func TestIdentifyOwnSymbols(t *testing.T) {
	libExports := []Symbol{
		{Name: "myapp_init"},
		{Name: "myapp_run"},
		{Name: "cJSON_Parse"},
		{Name: "LZ4_compress_default"},
	}

	depSymbols := map[string][]string{
		"cjson": {"cJSON_Parse", "cJSON_Delete"},
		"lz4":   {"LZ4_compress_default"},
	}

	own := IdentifyOwnSymbols(libExports, depSymbols)
	if len(own) != 2 {
		t.Errorf("expected 2 own symbols, got %d: %v", len(own), own)
	}
	if own[0] != "myapp_init" || own[1] != "myapp_run" {
		t.Errorf("expected [myapp_init myapp_run], got %v", own)
	}
}

func TestGenerateVersionScript(t *testing.T) {
	script := GenerateVersionScript([]string{"myapp_run", "myapp_init"})
	if !strings.Contains(script, "global:") {
		t.Error("should contain global section")
	}
	if !strings.Contains(script, "local:") {
		t.Error("should contain local section")
	}
	if !strings.Contains(script, "myapp_init;") {
		t.Error("should contain myapp_init")
	}
	if !strings.Contains(script, "myapp_run;") {
		t.Error("should contain myapp_run")
	}
	if !strings.Contains(script, "*;") {
		t.Error("should hide everything else with *")
	}
}

func TestGenerateExportList(t *testing.T) {
	list := GenerateExportList([]string{"myapp_run", "myapp_init"})
	lines := strings.Split(strings.TrimSpace(list), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	// Should be sorted and have underscore prefix
	if lines[0] != "_myapp_init" {
		t.Errorf("expected _myapp_init, got %s", lines[0])
	}
	if lines[1] != "_myapp_run" {
		t.Errorf("expected _myapp_run, got %s", lines[1])
	}
}

func TestFormatStaticLeakReport(t *testing.T) {
	report := &StaticLeakReport{
		Leaks: map[string][]string{
			"cjson": {"cJSON_Parse", "cJSON_Delete"},
		},
		TotalLeaked: 2,
		Clean:       false,
	}

	output := FormatStaticLeakReport(report)
	if !strings.Contains(output, "2 symbol(s)") {
		t.Error("should mention symbol count")
	}
	if !strings.Contains(output, "cjson") {
		t.Error("should mention dependency name")
	}
	if !strings.Contains(output, "cJSON_Parse") {
		t.Error("should list leaked symbols")
	}

	// Clean report should be empty
	clean := &StaticLeakReport{Clean: true}
	if FormatStaticLeakReport(clean) != "" {
		t.Error("clean report should produce empty string")
	}
}
