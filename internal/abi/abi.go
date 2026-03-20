package abi

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// DetectFormat detects the binary format by reading magic bytes.
func DetectFormat(path string) (BinaryFormat, error) {
	f, err := os.Open(path)
	if err != nil {
		return FormatUnknown, err
	}
	defer f.Close()

	magic := make([]byte, 4)
	if _, err := f.Read(magic); err != nil {
		return FormatUnknown, err
	}

	// ELF: 0x7f 'E' 'L' 'F'
	if magic[0] == 0x7f && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return FormatELF, nil
	}

	// Mach-O: 0xFEEDFACE, 0xFEEDFACF, 0xCEFAEDFE, 0xCFFAEDFE
	if (magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && (magic[3] == 0xCE || magic[3] == 0xCF)) ||
		((magic[0] == 0xCE || magic[0] == 0xCF) && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE) {
		return FormatMachO, nil
	}

	// PE: 'M' 'Z'
	if magic[0] == 'M' && magic[1] == 'Z' {
		return FormatPE, nil
	}

	return FormatUnknown, nil
}

// ExtractSymbols extracts exported symbols from a binary, auto-detecting format.
func ExtractSymbols(path string) ([]Symbol, error) {
	format, err := DetectFormat(path)
	if err != nil {
		return nil, fmt.Errorf("detecting format: %w", err)
	}

	switch format {
	case FormatELF:
		return ExtractELFSymbols(path)
	case FormatMachO:
		return ExtractMachOSymbols(path)
	case FormatPE:
		return ExtractPESymbols(path)
	default:
		return nil, fmt.Errorf("unsupported binary format for %s", path)
	}
}

// SymbolNames returns a sorted list of exported symbol names.
func SymbolNames(symbols []Symbol) []string {
	names := make([]string, len(symbols))
	for i, s := range symbols {
		names[i] = s.Name
	}
	sort.Strings(names)
	return names
}

// FormatSymbolList formats symbols for symbols.txt output.
func FormatSymbolList(symbols []Symbol) string {
	names := SymbolNames(symbols)
	return strings.Join(names, "\n") + "\n"
}
