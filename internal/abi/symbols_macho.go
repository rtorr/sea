package abi

import (
	"debug/macho"
	"fmt"
	"strings"
)

// ExtractMachOSymbols extracts exported symbols from a Mach-O binary.
func ExtractMachOSymbols(path string) ([]Symbol, error) {
	f, err := macho.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening Mach-O: %w", err)
	}
	defer f.Close()

	if f.Symtab == nil {
		return nil, nil
	}

	var symbols []Symbol
	for _, s := range f.Symtab.Syms {
		if s.Name == "" {
			continue
		}

		// Check if external (N_EXT bit set)
		nType := s.Type
		isExternal := (nType & 0x01) != 0 // N_EXT
		isPrivateExternal := (nType & 0x10) != 0 // N_PEXT

		if !isExternal || isPrivateExternal {
			continue
		}

		// Check if defined (N_TYPE bits indicate section)
		nTypeMask := nType & 0x0E
		if nTypeMask == 0x00 { // N_UNDF
			// Undefined unless it's a common symbol (value != 0)
			if s.Value == 0 {
				continue
			}
		}

		name := s.Name
		// Strip leading underscore (C convention on macOS)
		if strings.HasPrefix(name, "_") {
			name = name[1:]
		}

		symbols = append(symbols, Symbol{
			Name:       name,
			Binding:    BindGlobal,
			Visibility: VisDefault,
			Type:       TypeOther, // Mach-O doesn't have type info in nlist
		})
	}

	return symbols, nil
}
