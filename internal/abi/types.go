package abi

// Symbol represents an exported symbol from a binary.
type Symbol struct {
	Name       string
	Binding    SymbolBinding
	Visibility SymbolVisibility
	Type       SymbolType
}

// SymbolBinding is the ELF symbol binding.
type SymbolBinding int

const (
	BindLocal  SymbolBinding = iota
	BindGlobal
	BindWeak
)

func (b SymbolBinding) String() string {
	switch b {
	case BindLocal:
		return "local"
	case BindGlobal:
		return "global"
	case BindWeak:
		return "weak"
	default:
		return "unknown"
	}
}

// SymbolVisibility is the ELF symbol visibility.
type SymbolVisibility int

const (
	VisDefault   SymbolVisibility = iota
	VisInternal
	VisHidden
	VisProtected
)

func (v SymbolVisibility) String() string {
	switch v {
	case VisDefault:
		return "default"
	case VisInternal:
		return "internal"
	case VisHidden:
		return "hidden"
	case VisProtected:
		return "protected"
	default:
		return "unknown"
	}
}

// SymbolType classifies what a symbol represents.
type SymbolType int

const (
	TypeNone SymbolType = iota
	TypeFunc
	TypeObject
	TypeOther
)

func (t SymbolType) String() string {
	switch t {
	case TypeFunc:
		return "func"
	case TypeObject:
		return "object"
	default:
		return "other"
	}
}

// BinaryFormat identifies the binary format.
type BinaryFormat int

const (
	FormatUnknown BinaryFormat = iota
	FormatELF
	FormatMachO
	FormatPE
)

func (f BinaryFormat) String() string {
	switch f {
	case FormatELF:
		return "ELF"
	case FormatMachO:
		return "Mach-O"
	case FormatPE:
		return "PE"
	default:
		return "unknown"
	}
}
