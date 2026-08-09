//go:build linux

package reconcile

import (
	"os"
	"syscall"
	"time"
)

func ctimeNS(info os.FileInfo) int64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return info.ModTime().UnixNano()
	}

	return stat.Ctim.Sec*int64(time.Second) + stat.Ctim.Nsec
}
