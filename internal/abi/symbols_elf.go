package abi

import (
	"debug/elf"
	"fmt"
)

// ExtractSONAME reads the DT_SONAME entry from an ELF .dynamic section.
// Returns the SONAME string, or empty string if not present or not ELF.
func ExtractSONAME(path string) (string, error) {
	f, err := elf.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening ELF: %w", err)
	}
	defer f.Close()

	sonames, err := f.DynString(elf.DT_SONAME)
	if err != nil {
		return "", nil // no .dynamic section or no DT_SONAME — not an error
	}
	if len(sonames) > 0 {
		return sonames[0], nil
	}
	return "", nil
}

// ExtractELFSymbols extracts exported symbols from an ELF binary.
func ExtractELFSymbols(path string) ([]Symbol, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening ELF: %w", err)
	}
	defer f.Close()

	dynSyms, err := f.DynamicSymbols()
	if err != nil {
		// Fall back to regular symbol table
		dynSyms = nil
	}

	// Also try regular symtab
	regularSyms, _ := f.Symbols()

	allSyms := append(dynSyms, regularSyms...)

	var symbols []Symbol
	seen := make(map[string]bool)

	for _, s := range allSyms {
		if s.Name == "" {
			continue
		}
		// Skip undefined symbols
		if s.Section == elf.SHN_UNDEF {
			continue
		}
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true

		binding := elfBinding(s.Info)
		vis := elfVisibility(s.Other)

		// Only include exported symbols (global or weak with default/protected visibility)
		if binding == BindLocal {
			continue
		}
		if vis == VisHidden || vis == VisInternal {
			continue
		}

		symbols = append(symbols, Symbol{
			Name:       s.Name,
			Binding:    binding,
			Visibility: vis,
			Type:       elfType(s.Info),
		})
	}

	return symbols, nil
}

func elfBinding(info byte) SymbolBinding {
	switch elf.ST_BIND(info) {
	case elf.STB_LOCAL:
		return BindLocal
	case elf.STB_GLOBAL:
		return BindGlobal
	case elf.STB_WEAK:
		return BindWeak
	default:
		return BindLocal
	}
}

func elfVisibility(other byte) SymbolVisibility {
	switch elf.SymVis(other) {
	case elf.STV_DEFAULT:
		return VisDefault
	case elf.STV_INTERNAL:
		return VisInternal
	case elf.STV_HIDDEN:
		return VisHidden
	case elf.STV_PROTECTED:
		return VisProtected
	default:
		return VisDefault
	}
}

func elfType(info byte) SymbolType {
	switch elf.ST_TYPE(info) {
	case elf.STT_FUNC:
		return TypeFunc
	case elf.STT_OBJECT:
		return TypeObject
	default:
		return TypeOther
	}
}
