package services

import (
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to path atomically: it first writes to a
// temporary file in the same directory, fsyncs it, then renames it over the
// destination. This prevents a half-written/truncated file if the process is
// killed or crashes mid-write. The temp file inherits perm; on success the
// destination ends up with perm.
//
// Callers that previously used os.WriteFile(path, data, perm) for JSON state
// files should use this instead so persisted state can never be corrupted by
// an interrupted write.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// Best-effort cleanup if anything below fails before the rename.
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
