package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

// Size returns the total size of the cache in bytes.
func (c *Cache) Size() (int64, error) {
	var total int64
	err := filepath.Walk(c.Layout.Root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, fmt.Errorf("calculating cache size: %w", err)
	}
	return total, nil
}
