package archive

// PackageMetaFile is the metadata file inside a package archive.
const PackageMetaFile = "sea-package.toml"

// SymbolsFile is the exported symbols list inside a package archive.
const SymbolsFile = "symbols.txt"

// PackageMeta is the sea-package.toml inside an archive.
type PackageMeta struct {
	Package      MetaPackage      `toml:"package"`
	ABI          MetaABI          `toml:"abi"`
	Contents     MetaContents     `toml:"contents"`
	Libs         []MetaLib        `toml:"libraries,omitempty"`
	Symbols      MetaSymbols      `toml:"symbols"`
	Dependencies []MetaDependency `toml:"dependencies,omitempty"`
}

type MetaPackage struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
	Channel string `toml:"channel,omitempty"` // "stable" | "beta" | "rc" | "dev"
	Kind    string `toml:"kind"`
}

type MetaABI struct {
	Tag             string `toml:"tag"`
	OS              string `toml:"os"`
	Arch            string `toml:"arch"`
	Compiler        string `toml:"compiler"`
	CompilerVersion string `toml:"compiler_version"`
	CppStdlib       string `toml:"cpp_stdlib"`
	BuildType       string `toml:"build_type"`

	// Fingerprint is the empirical ABI fingerprint hash from the ABI probe.
	// It captures actual type layout (sizeof std::string, std::vector, etc.),
	// name mangling scheme, and exception ABI — the things that actually
	// determine binary compatibility. Two packages with the same fingerprint
	// are link-compatible regardless of compiler version strings.
	// Empty for packages published before probe support was added.
	Fingerprint string `toml:"fingerprint,omitempty"`
}

type MetaContents struct {
	IncludeDirs []string `toml:"include_dirs,omitempty"`
	LibDirs     []string `toml:"lib_dirs,omitempty"`
}

type MetaLib struct {
	Path                string `toml:"path"`
	Type                string `toml:"type"` // "shared" | "static"
	Soname              string `toml:"soname,omitempty"`
	ExportedSymbolCount int    `toml:"exported_symbol_count"`
}

type MetaSymbols struct {
	SymbolsHash     string   `toml:"symbols_hash"`
	VisibilityClean bool     `toml:"visibility_clean"`
	Exported        []string `toml:"exported,omitempty"` // sorted list of exported symbol names
}

// MetaDependency records a dependency inside the package metadata so the
// resolver can read transitive dependencies without fetching the full manifest.
type MetaDependency struct {
	Name    string `toml:"name"`
	Version string `toml:"version"` // version range string
}

// Validate checks that required fields are present.
func (m *PackageMeta) Validate() error {
	if m.Package.Name == "" {
		return errMetaField("package.name")
	}
	if m.Package.Version == "" {
		return errMetaField("package.version")
	}
	if m.ABI.Tag == "" {
		return errMetaField("abi.tag")
	}
	return nil
}

type metaFieldError struct {
	field string
}

func (e *metaFieldError) Error() string {
	return "sea-package.toml: missing required field " + e.field
}

func errMetaField(field string) error {
	return &metaFieldError{field: field}
}
