//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package processlock

import (
	"errors"
	"os"
)

func lockFile(*os.File) error { return errors.New("当前平台尚不支持安全的数据目录锁") }
func lockBusy(error) bool     { return false }
