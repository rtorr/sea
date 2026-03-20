package abi

import (
	"testing"
)

func TestDiffSymbols(t *testing.T) {
	old := []Symbol{
		{Name: "foo", Binding: BindGlobal},
		{Name: "bar", Binding: BindGlobal},
		{Name: "baz", Binding: BindGlobal},
	}
	new := []Symbol{
		{Name: "foo", Binding: BindGlobal},
		{Name: "baz", Binding: BindGlobal},
		{Name: "qux", Binding: BindGlobal},
	}

	diff := DiffSymbols(old, new)

	if len(diff.Added) != 1 || diff.Added[0] != "qux" {
		t.Errorf("expected added [qux], got %v", diff.Added)
	}
	if len(diff.Removed) != 1 || diff.Removed[0] != "bar" {
		t.Errorf("expected removed [bar], got %v", diff.Removed)
	}
	if !diff.IsBreaking() {
		t.Error("expected breaking change")
	}
}

func TestDiffSymbolsNoChange(t *testing.T) {
	syms := []Symbol{{Name: "foo"}, {Name: "bar"}}
	diff := DiffSymbols(syms, syms)
	if diff.IsBreaking() {
		t.Error("no changes should not be breaking")
	}
	if len(diff.Added) != 0 || len(diff.Removed) != 0 {
		t.Error("expected no changes")
	}
}

func TestCheckVisibility(t *testing.T) {
	symbols := []Symbol{
		{Name: "public_func"},
		{Name: "detail::internal_func"},
		{Name: "_Internal_helper"},
		{Name: "another_public"},
	}

	report := CheckVisibility(symbols, DefaultPolicy())
	if report.Clean {
		t.Error("expected unclean report")
	}
	if len(report.LeakedSymbols) != 2 {
		t.Errorf("expected 2 leaked symbols, got %d", len(report.LeakedSymbols))
	}
}

func TestCheckVisibilityClean(t *testing.T) {
	symbols := []Symbol{
		{Name: "public_func"},
		{Name: "another_public"},
	}

	report := CheckVisibility(symbols, DefaultPolicy())
	if !report.Clean {
		t.Error("expected clean report")
	}
}

func TestSymbolNames(t *testing.T) {
	symbols := []Symbol{
		{Name: "charlie"},
		{Name: "alpha"},
		{Name: "bravo"},
	}
	names := SymbolNames(symbols)
	if names[0] != "alpha" || names[1] != "bravo" || names[2] != "charlie" {
		t.Errorf("expected sorted names, got %v", names)
	}
}
