//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd

package reconcile

import "os"

func ctimeNS(info os.FileInfo) int64 {
	return info.ModTime().UnixNano()
}
