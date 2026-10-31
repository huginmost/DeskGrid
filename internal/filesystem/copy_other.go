//go:build !windows

package filesystem

import (
	"context"
	"io"
	"os"
)

func copyFile(ctx context.Context, s, d string, onProgress func(int64)) error {
	in, e := os.Open(s)
	if e != nil {
		return e
	}
	defer in.Close()
	info, e := in.Stat()
	if e != nil {
		return e
	}
	out, e := os.OpenFile(d, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if e != nil {
		return e
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			os.Remove(d)
		}
	}()
	buf := make([]byte, 256*1024)
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, re := in.Read(buf)
		if n > 0 {
			wn, we := out.Write(buf[:n])
			onProgress(int64(wn))
			if we != nil {
				return we
			}
			if wn != n {
				return io.ErrShortWrite
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return re
		}
	}
	if e = out.Sync(); e != nil {
		return e
	}
	if e = out.Close(); e != nil {
		return e
	}
	ok = true
	return nil
}
