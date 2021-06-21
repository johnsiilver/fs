// Package fs provides utilities for making use of the new io/fs abstractions and the embed package.
package fs

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

const fileMode fs.FileMode = 0444

// Writer provides a filesystem with simple read and write primitives.
type Writer interface {
	fs.FS

	// Writes file with name (full path) a content to the file system.
	// Returns fs.ErrExist if the file already exists.
	WriteFile(name string, content []byte) error
}

// Merge will merge "from" into "into" by walking "from" the root "/". Each file will be
// prepended with "prepend" which must start and end with "/". If into does not
// implement Writer, this will panic. If the file already exists, this will error and
// leave a partial copied fs.FS.
func Merge(into Writer, from fs.FS, prepend string) error {
	if prepend == "/" {
		prepend = ""
	}
	if prepend != "" {
		if !strings.HasSuffix(prepend, "/") {
			return fmt.Errorf("prepend(%s) does not end with '/'", prepend)
		}
		strings.TrimPrefix(prepend, ".")
		strings.TrimPrefix(prepend, "/")
	}

	fn := func(p string, d fs.DirEntry, err error) error {
		switch p {
		case "/", "":
			return nil
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(from, p)
		if err != nil {
			return err
		}

		return into.WriteFile(path.Join(prepend, p), b)
	}

	return fs.WalkDir(from, ".", fn)
}
