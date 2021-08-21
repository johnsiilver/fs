// Package fs provides utilities for making use of the new io/fs abstractions and the embed package.
package fs

import (
	"fmt"
	"io/fs"
	"log"
	"path"
	"strings"
)

// OFOption is an option for the OpenFiler.OpenFile() call. The passed "o" arge
// is implementation dependent.
type OFOption func(o interface{}) error

// OpenFiler provides a more robust method of opening a file that allows for additional
// capabilities like writing to files. The fs.File and options are generic and implementation
// specific. To gain access to additional capabilities usually requires type asserting the fs.File
// to the implementation specific type.
type OpenFiler interface {
	fs.FS

	// OpenFile opens the file at name with flags and options. flags can be any subset of the
	// flags defined in the fs module (O_CREATE, O_READONLY, ...). The set of options is implementation
	// dependent. The fs.File that is returned should be type asserted to gain access to additional
	// capabilities. If opening for ReadOnly, generally the standard fs.Open() call is better.
	OpenFile(name string, flags int, options ...OFOption) (fs.File, error)
}

// Writer provides a filesystem implememnting OpenFiler with a simple way to write and entire file.
type Writer interface {
	OpenFiler

	// Writes file with name (full path) a content to the file system. This implementation may
	// return fs.ErrExist if the file already exists and the FileSystem is write once. The FileMode
	// may or may not be honored, see the implementation details for more information.
	WriteFile(name string, data []byte, perm fs.FileMode) error
}

type mergeOptions struct {
	fileTransform FileTransform
}

// MergeOption is an optional argument for Merge().
type MergeOption func(o *mergeOptions)

// FileTransform gives the base name of a file and the content of the file. It returns
// the content that MAY be transformed in some way.
type FileTransform func(name string, content []byte) ([]byte, error)

// WithTransform instructs the Merge() to use a FileTransform on the files it reads before
// writing them to the destination.
func WithTransform(ft FileTransform) MergeOption {
	return func(o *mergeOptions) {
		o.fileTransform = ft
	}
}

// Merge will merge "from" into "into" by walking "from" the root "/". Each file will be
// prepended with "prepend" which must start and end with "/". If into does not
// implement Writer, this will panic. If the file already exists, this will error and
// leave a partial copied fs.FS.
func Merge(into Writer, from fs.FS, prepend string, options ...MergeOption) error {
	// Note: Testing this is done inside simple_test.go, to avoid some recursive imports
	opt := mergeOptions{}
	for _, o := range options {
		o(&opt)
	}

	if prepend == "/" {
		prepend = ""
	}
	if prepend != "" {
		if !strings.HasSuffix(prepend, "/") {
			return fmt.Errorf("prepend(%s) does not end with '/'", prepend)
		}
		prepend = strings.TrimPrefix(prepend, ".")
		prepend = strings.TrimPrefix(prepend, "/")
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

		if opt.fileTransform != nil {
			b, err = opt.fileTransform(path.Base(p), b)
			if err != nil {
				return err
			}
		}

		return into.WriteFile(path.Join(prepend, p), b, d.Type())
	}

	return fs.WalkDir(from, ".", fn)
}

// Logger provides the minimum interface for a logging client.
type Logger interface {
	Println(v ...interface{})
	Printf(format string, v ...interface{})
}

// DefaultLogger provides a default Logger implementation that uses Go's standard
// log.Println/Printf calls.
type DefaultLogger struct{}

func (DefaultLogger) Println(v ...interface{}) {
	log.Println(v...)
}

func (DefaultLogger) Printf(format string, v ...interface{}) {
	log.Printf(format, v...)
}
