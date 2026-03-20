package abi

import (
	"debug/pe"
	"encoding/binary"
	"fmt"
	"io"
)

// ExtractPESymbols extracts exported symbols from a PE binary.
func ExtractPESymbols(path string) ([]Symbol, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening PE: %w", err)
	}
	defer f.Close()

	// Find the export directory from DataDirectory[0]
	var exportDir pe.DataDirectory

	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		if len(oh.DataDirectory) > 0 {
			exportDir = oh.DataDirectory[0]
		}
	case *pe.OptionalHeader64:
		if len(oh.DataDirectory) > 0 {
			exportDir = oh.DataDirectory[0]
		}
	default:
		return nil, fmt.Errorf("unsupported PE optional header type")
	}

	if exportDir.VirtualAddress == 0 || exportDir.Size == 0 {
		return nil, nil // no exports
	}

	// Find the section containing the export directory
	var exportSection *pe.Section
	for _, s := range f.Sections {
		if exportDir.VirtualAddress >= s.VirtualAddress &&
			exportDir.VirtualAddress < s.VirtualAddress+s.VirtualSize {
			exportSection = s
			break
		}
	}
	if exportSection == nil {
		return nil, fmt.Errorf("cannot find section containing export directory")
	}

	sectionData, err := exportSection.Data()
	if err != nil {
		return nil, fmt.Errorf("reading export section: %w", err)
	}

	// Offset within section data
	base := exportDir.VirtualAddress - exportSection.VirtualAddress

	if int(base)+40 > len(sectionData) {
		return nil, fmt.Errorf("export directory truncated")
	}

	// Parse IMAGE_EXPORT_DIRECTORY
	data := sectionData[base:]
	numberOfNames := binary.LittleEndian.Uint32(data[24:28])
	addressOfNames := binary.LittleEndian.Uint32(data[32:36])

	namesBase := addressOfNames - exportSection.VirtualAddress

	var symbols []Symbol
	for i := uint32(0); i < numberOfNames; i++ {
		nameRVAOff := namesBase + i*4
		if int(nameRVAOff)+4 > len(sectionData) {
			break
		}
		nameRVA := binary.LittleEndian.Uint32(sectionData[nameRVAOff : nameRVAOff+4])
		nameOff := nameRVA - exportSection.VirtualAddress

		name, err := readCString(sectionData, nameOff)
		if err != nil {
			continue
		}

		symbols = append(symbols, Symbol{
			Name:       name,
			Binding:    BindGlobal,
			Visibility: VisDefault,
			Type:       TypeOther,
		})
	}

	return symbols, nil
}

func readCString(data []byte, offset uint32) (string, error) {
	if int(offset) >= len(data) {
		return "", io.ErrUnexpectedEOF
	}
	end := offset
	for int(end) < len(data) && data[end] != 0 {
		end++
	}
	return string(data[offset:end]), nil
}
