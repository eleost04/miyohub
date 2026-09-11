// Package processlock prevents concurrent MiyoHub processes from opening one
// JSON store. In-process store mutexes alone cannot protect against a second CLI.
package processlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrInUse = errors.New("数据目录正在使用，请先停止使用该目录的 MiyoHub 服务或命令")

// Acquire returns an OS-held lock released on Close or process exit, including
// crashes. The harmless lock file stays in place to avoid inode replacement races.
func Acquire(directory string) (*os.File, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(directory, ".miyohub.lock")
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("数据目录锁必须是普通文件")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		if lockBusy(err) {
			return nil, ErrInUse
		}
		return nil, fmt.Errorf("不能锁定数据目录: %w", err)
	}
	return file, nil
}
