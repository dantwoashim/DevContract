// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package fsutil

import (
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to path by using a temporary file in the same
// directory and renaming it into place after the write succeeds.
func AtomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(dir, ".devcontract-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	return os.Rename(tempPath, path)
}
