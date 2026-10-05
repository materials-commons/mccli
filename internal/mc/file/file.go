package file

import (
	"os"
)

// FileExists returns true if the file exists, and false if it does not. It returns an error if the file exists but cannot be read.
func FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		// err != nil, and !os.IsNotExist(err)
		return false, err
	}
}

// IsDir returns true if the path is a directory, and false if it is not. It returns an error if the path cannot be read.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
