package abi

import (
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"fmt"
	"strings"
)

// FuncSignature represents a function's type signature extracted from DWARF.
type FuncSignature struct {
	Name       string
	ReturnType string
	Params     []ParamInfo
}

// ParamInfo describes a function parameter.
type ParamInfo struct {
	Name string
	Type string
}

func (f FuncSignature) String() string {
	params := make([]string, len(f.Params))
	for i, p := range f.Params {
		if p.Name != "" {
			params[i] = p.Type + " " + p.Name
		} else {
			params[i] = p.Type
		}
	}
	return fmt.Sprintf("%s %s(%s)", f.ReturnType, f.Name, strings.Join(params, ", "))
}

// StructLayout represents a struct/union extracted from DWARF.
type StructLayout struct {
	Name    string
	Size    int64
	Fields  []FieldInfo
}

// FieldInfo describes a struct field.
type FieldInfo struct {
	Name   string
	Type   string
	Offset int64
	Size   int64
}

// TypeInfo holds all DWARF-extracted type information for a library.
type TypeInfo struct {
	Functions map[string]FuncSignature // name -> signature
	Structs   map[string]StructLayout  // name -> layout
}

// ExtractTypeInfo reads DWARF debug info from a binary and extracts
// function signatures and struct layouts. Returns nil if no DWARF info
// is available (release builds without debug info).
func ExtractTypeInfo(path string) (*TypeInfo, error) {
	format, err := DetectFormat(path)
	if err != nil {
		return nil, err
	}

	var dwarfData *dwarf.Data

	switch format {
	case FormatELF:
		f, err := elf.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		dwarfData, err = f.DWARF()
		if err != nil {
			return nil, nil // no DWARF
		}
	case FormatMachO:
		f, err := macho.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		dwarfData, err = f.DWARF()
		if err != nil {
			return nil, nil // no DWARF
		}
	default:
		return nil, nil // PE DWARF not commonly available
	}

	return parseDWARF(dwarfData)
}

func parseDWARF(d *dwarf.Data) (*TypeInfo, error) {
	info := &TypeInfo{
		Functions: make(map[string]FuncSignature),
		Structs:   make(map[string]StructLayout),
	}

	reader := d.Reader()

	for {
		entry, err := reader.Next()
		if err != nil {
			break
		}
		if entry == nil {
			break
		}

		switch entry.Tag {
		case dwarf.TagSubprogram:
			sig := parseFuncEntry(d, reader, entry)
			if sig.Name != "" && isExportedName(sig.Name) {
				info.Functions[sig.Name] = sig
			}

		case dwarf.TagStructType, dwarf.TagUnionType:
			layout := parseStructEntry(d, reader, entry)
			if layout.Name != "" {
				info.Structs[layout.Name] = layout
			}

		default:
			// Skip children of entries we don't care about
			if entry.Children {
				reader.SkipChildren()
			}
		}
	}

	return info, nil
}

func parseFuncEntry(d *dwarf.Data, reader *dwarf.Reader, entry *dwarf.Entry) FuncSignature {
	sig := FuncSignature{}

	// Get function name
	if name, ok := entry.Val(dwarf.AttrName).(string); ok {
		sig.Name = name
	}
	// Skip compiler-generated or anonymous functions
	if sig.Name == "" || strings.HasPrefix(sig.Name, "__") {
		if entry.Children {
			reader.SkipChildren()
		}
		return sig
	}

	// Get return type
	if typeOff, ok := entry.Val(dwarf.AttrType).(dwarf.Offset); ok {
		sig.ReturnType = resolveTypeName(d, typeOff)
	} else {
		sig.ReturnType = "void"
	}

	// Parse parameters from children
	if entry.Children {
		for {
			child, err := reader.Next()
			if err != nil || child == nil || child.Tag == 0 {
				break
			}
			if child.Tag == dwarf.TagFormalParameter {
				param := ParamInfo{}
				if name, ok := child.Val(dwarf.AttrName).(string); ok {
					param.Name = name
				}
				if typeOff, ok := child.Val(dwarf.AttrType).(dwarf.Offset); ok {
					param.Type = resolveTypeName(d, typeOff)
				}
				sig.Params = append(sig.Params, param)
			}
			if child.Children {
				reader.SkipChildren()
			}
		}
	}

	return sig
}

func parseStructEntry(d *dwarf.Data, reader *dwarf.Reader, entry *dwarf.Entry) StructLayout {
	layout := StructLayout{}

	if name, ok := entry.Val(dwarf.AttrName).(string); ok {
		layout.Name = name
	}
	if size, ok := entry.Val(dwarf.AttrByteSize).(int64); ok {
		layout.Size = size
	}

	if entry.Children {
		for {
			child, err := reader.Next()
			if err != nil || child == nil || child.Tag == 0 {
				break
			}
			if child.Tag == dwarf.TagMember {
				field := FieldInfo{}
				if name, ok := child.Val(dwarf.AttrName).(string); ok {
					field.Name = name
				}
				if typeOff, ok := child.Val(dwarf.AttrType).(dwarf.Offset); ok {
					field.Type = resolveTypeName(d, typeOff)
				}
				if off, ok := child.Val(dwarf.AttrDataMemberLoc).(int64); ok {
					field.Offset = off
				}
				if size, ok := child.Val(dwarf.AttrByteSize).(int64); ok {
					field.Size = size
				}
				layout.Fields = append(layout.Fields, field)
			}
			if child.Children {
				reader.SkipChildren()
			}
		}
	}

	return layout
}

// resolveTypeName follows a DWARF type offset and returns a human-readable name.
func resolveTypeName(d *dwarf.Data, off dwarf.Offset) string {
	// Limit depth to avoid infinite loops on recursive types
	return resolveTypeNameDepth(d, off, 0)
}

func resolveTypeNameDepth(d *dwarf.Data, off dwarf.Offset, depth int) string {
	if depth > 10 {
		return "..."
	}

	reader := d.Reader()
	reader.Seek(off)
	entry, err := reader.Next()
	if err != nil || entry == nil {
		return "?"
	}

	switch entry.Tag {
	case dwarf.TagBaseType, dwarf.TagTypedef, dwarf.TagStructType,
		dwarf.TagUnionType, dwarf.TagEnumerationType:
		if name, ok := entry.Val(dwarf.AttrName).(string); ok {
			return name
		}
		return "<anon>"

	case dwarf.TagPointerType:
		if typeOff, ok := entry.Val(dwarf.AttrType).(dwarf.Offset); ok {
			return resolveTypeNameDepth(d, typeOff, depth+1) + "*"
		}
		return "void*"

	case dwarf.TagConstType:
		if typeOff, ok := entry.Val(dwarf.AttrType).(dwarf.Offset); ok {
			return "const " + resolveTypeNameDepth(d, typeOff, depth+1)
		}
		return "const void"

	case dwarf.TagVolatileType:
		if typeOff, ok := entry.Val(dwarf.AttrType).(dwarf.Offset); ok {
			return "volatile " + resolveTypeNameDepth(d, typeOff, depth+1)
		}
		return "volatile void"

	case dwarf.TagRestrictType:
		if typeOff, ok := entry.Val(dwarf.AttrType).(dwarf.Offset); ok {
			return resolveTypeNameDepth(d, typeOff, depth+1) + " restrict"
		}
		return "void restrict"

	case dwarf.TagArrayType:
		if typeOff, ok := entry.Val(dwarf.AttrType).(dwarf.Offset); ok {
			return resolveTypeNameDepth(d, typeOff, depth+1) + "[]"
		}
		return "?[]"

	case dwarf.TagSubroutineType:
		return "func(...)"

	case dwarf.TagReferenceType:
		if typeOff, ok := entry.Val(dwarf.AttrType).(dwarf.Offset); ok {
			return resolveTypeNameDepth(d, typeOff, depth+1) + "&"
		}
		return "void&"

	default:
		if name, ok := entry.Val(dwarf.AttrName).(string); ok {
			return name
		}
		return "?"
	}
}

// isExportedName checks if a function name looks like a public C API
// (not a compiler intrinsic, not an internal helper).
func isExportedName(name string) bool {
	if strings.HasPrefix(name, "__") {
		return false
	}
	if strings.HasPrefix(name, "_GLOBAL_") {
		return false
	}
	return true
}

// DiffTypeInfo compares two TypeInfos and returns type-level changes.
type TypeChange struct {
	Symbol  string
	Kind    string // "signature_changed", "struct_size_changed", "field_changed"
	Old     string
	New     string
}

// DiffTypes compares DWARF type info between two versions.
// Returns changes that represent ABI breaks even when symbol names are unchanged.
func DiffTypes(oldInfo, newInfo *TypeInfo) []TypeChange {
	if oldInfo == nil || newInfo == nil {
		return nil
	}

	var changes []TypeChange

	// Compare function signatures
	for name, oldSig := range oldInfo.Functions {
		newSig, ok := newInfo.Functions[name]
		if !ok {
			continue // symbol removal handled by symbol-level diff
		}
		if oldSig.ReturnType != newSig.ReturnType {
			changes = append(changes, TypeChange{
				Symbol: name,
				Kind:   "return_type_changed",
				Old:    oldSig.ReturnType,
				New:    newSig.ReturnType,
			})
		}
		if len(oldSig.Params) != len(newSig.Params) {
			changes = append(changes, TypeChange{
				Symbol: name,
				Kind:   "param_count_changed",
				Old:    fmt.Sprintf("%d params", len(oldSig.Params)),
				New:    fmt.Sprintf("%d params", len(newSig.Params)),
			})
		} else {
			for i := range oldSig.Params {
				if oldSig.Params[i].Type != newSig.Params[i].Type {
					changes = append(changes, TypeChange{
						Symbol: name,
						Kind:   "param_type_changed",
						Old:    fmt.Sprintf("param %d: %s", i, oldSig.Params[i].Type),
						New:    fmt.Sprintf("param %d: %s", i, newSig.Params[i].Type),
					})
				}
			}
		}
	}

	// Compare struct layouts
	for name, oldLayout := range oldInfo.Structs {
		newLayout, ok := newInfo.Structs[name]
		if !ok {
			continue
		}
		if oldLayout.Size != newLayout.Size {
			changes = append(changes, TypeChange{
				Symbol: "struct " + name,
				Kind:   "struct_size_changed",
				Old:    fmt.Sprintf("%d bytes", oldLayout.Size),
				New:    fmt.Sprintf("%d bytes", newLayout.Size),
			})
		}
		// Check field offsets
		oldFields := make(map[string]FieldInfo)
		for _, f := range oldLayout.Fields {
			if f.Name != "" {
				oldFields[f.Name] = f
			}
		}
		for _, newField := range newLayout.Fields {
			if newField.Name == "" {
				continue
			}
			if oldField, ok := oldFields[newField.Name]; ok {
				if oldField.Offset != newField.Offset {
					changes = append(changes, TypeChange{
						Symbol: fmt.Sprintf("struct %s.%s", name, newField.Name),
						Kind:   "field_offset_changed",
						Old:    fmt.Sprintf("offset %d", oldField.Offset),
						New:    fmt.Sprintf("offset %d", newField.Offset),
					})
				}
				if oldField.Type != newField.Type {
					changes = append(changes, TypeChange{
						Symbol: fmt.Sprintf("struct %s.%s", name, newField.Name),
						Kind:   "field_type_changed",
						Old:    oldField.Type,
						New:    newField.Type,
					})
				}
			}
		}
	}

	return changes
}

// FormatTypeChanges returns a human-readable summary of type-level changes.
func FormatTypeChanges(changes []TypeChange) string {
	if len(changes) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Type-level ABI changes (%d):\n", len(changes)))
	for _, c := range changes {
		sb.WriteString(fmt.Sprintf("  ! %s: %s\n      was: %s\n      now: %s\n", c.Symbol, c.Kind, c.Old, c.New))
	}
	return sb.String()
}

// HasBreakingTypeChanges returns true if any type change would break ABI.
// All type changes are considered breaking — a changed parameter type or
// struct size means existing compiled consumers will malfunction.
func HasBreakingTypeChanges(changes []TypeChange) bool {
	return len(changes) > 0
}

// ExtractTypeInfoBestEffort extracts DWARF type info, returning nil silently
// if the binary was built without debug info (which is normal for release builds).
func ExtractTypeInfoBestEffort(path string) *TypeInfo {
	info, _ := ExtractTypeInfo(path)
	return info
}

// FullDiff performs both symbol-level and type-level diffing.
// It returns the symbol diff, type changes, and the required version bump.
func FullDiff(oldPath, newPath string) (*ABIDiff, []TypeChange, VersionBump, error) {
	oldSyms, err := ExtractSymbols(oldPath)
	if err != nil {
		return nil, nil, BumpNone, fmt.Errorf("extracting old symbols: %w", err)
	}
	newSyms, err := ExtractSymbols(newPath)
	if err != nil {
		return nil, nil, BumpNone, fmt.Errorf("extracting new symbols: %w", err)
	}

	symbolDiff := DiffSymbols(oldSyms, newSyms)

	// Try DWARF type-level diff
	oldTypes := ExtractTypeInfoBestEffort(oldPath)
	newTypes := ExtractTypeInfoBestEffort(newPath)
	typeChanges := DiffTypes(oldTypes, newTypes)

	// Determine bump level
	bump := BumpNone
	if len(symbolDiff.Removed) > 0 || HasBreakingTypeChanges(typeChanges) {
		bump = BumpMajor
	} else if len(symbolDiff.Added) > 0 {
		bump = BumpMinor
	}

	return symbolDiff, typeChanges, bump, nil
}
