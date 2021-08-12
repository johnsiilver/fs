// Package os provides an io.FS that is implemented using the os package.
package os

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	jsfs "github.com/johnsiilver/fs"
)

// File implememnts fs.File.
type File struct {
	file *os.File
}

// OSFile returns the underlying *os.File.
func (f *File) OSFile() *os.File {
	return f.file
}

func (f *File) ReadDir(n int) ([]fs.DirEntry, error) {
	return f.file.ReadDir(n)
}

func (f *File) Read(b []byte) (n int, err error) {
	return f.file.Read(b)
}

func (f *File) Seek(offset int64, whence int) (ret int64, err error) {
	return f.file.Seek(offset, whence)
}

func (f *File) Stat() (fs.FileInfo, error) {
	return f.file.Stat()
}

func (f *File) Write(b []byte) (n int, err error) {
	return f.file.Write(b)
}

func (f *File) Close() error {
	return f.file.Close()
}

type fileInfo struct {
	fs.FileInfo
}

// FS implemements fs.ReadDirFS/StatFS/ReadFileFS/GlobFS using functions defined
// in the "os" and "filepath" packages. In addition we support
// github.com/johnsiilver/fs/OpenFiler to allow for writing files.
type FS struct{}

// Open implements fs.FS.Open().
func (f *FS) Open(name string) (fs.File, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return &File{file}, nil
}

// ReadDir implements fs.ReadDirFS.ReadDir().
func (f *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(name)
}

// Stat implememnts fs.StatFS.Stat().
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	fi, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	return fileInfo{fi}, nil
}

// ReadFile implements fs.ReadFileFS.ReadFile().
func (f *FS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// Glob implements fs.GlobFS.Glob().
func (f *FS) Glob(pattern string) (matches []string, err error) {
	return filepath.Glob(pattern)
}

type ofOptions struct {
	mode fs.FileMode
}

// FileMode sets the fs.FileMode when opening a file with OpenFile().
func FileMode(mode fs.FileMode) jsfs.OFOption {
	return func(o interface{}) error {
		v, ok := o.(*ofOptions)
		if !ok {
			return fmt.Errorf("FileMode received wrong type %T", o)
		}
		v.mode = mode
		return nil
	}
}

// OpenFile opens a file with the set flags and fs.FileMode. If you want to use the fs.File
// to write, you need to type assert if to *os.File. If Opening a file for
func (f *FS) OpenFile(name string, flags int, options ...jsfs.OFOption) (fs.File, error) {
	opts := ofOptions{}
	for _, o := range options {
		 if err := o(&opts); err != nil {
			 return nil, err
		 }
	}
	file, err := os.OpenFile(name, flags, opts.mode)
	if err != nil {
		return nil, err
	}
	return &File{file}, nil
}
