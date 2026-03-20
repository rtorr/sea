package registry

// Local is an alias for Filesystem, used for dev/test registries.
// It's identical in behavior but semantically distinct in config.

// NewLocal creates a local dev/test registry (same as filesystem).
func NewLocal(name, path string) (*Filesystem, error) {
	return NewFilesystem(name, path)
}
