package fs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"
)

// Simple provides a simple memory structure that implements io/fs.FS and fs.Writer(above).
// This is great for aggregating several different embeded fs.FS into a single structure using
// Merge() below. It uses "/" unix separators and doesn't deal with any funky "\/" things.
// If you want to use this don't start trying to get complicated with your pathing.
// This structure is safe for concurrent reading or concurrent writing, but not concurrent
// read/write. Once finished writing files, you should call .RO() to lock it.
type Simple struct {
	root *file

	writeMu sync.Mutex
	ro      bool

	pearson bool
	cache   []*file
	items   int
}

// SimpleOption provides an optional argument to NewSimple().
type SimpleOption func(s *Simple)

// WithPearson will create a lookup cache using Pearson hashing to make lookups actually happen
// at O(1) (after the hash calc) instead of walking the file system tree after various strings
// splits. When using this, realize that you MUST be using ASCII characters.
func WithPearson() SimpleOption {
	return func(s *Simple) {
		s.pearson = true
	}
}

// NewSimple is the constructor for Simple.
func NewSimple(options ...SimpleOption) *Simple {
	return &Simple{root: &file{name: ".", time: time.Now(), isDir: true}}
}

// Open implements fs.FS.Open().
func (s *Simple) Open(name string) (fs.File, error) {
	if name == "/" || name == "" || name == "." {
		return s.root, nil
	}

	strings.TrimPrefix(name, ".")
	strings.TrimPrefix(name, "/")

	sp := strings.Split(name, "/")

	if s.pearson && s.ro {
		h := pearson([]byte(name))
		i := int(h) % (len(s.cache) + 1)
		if i >= len(s.cache) {
			return nil, fs.ErrNotExist
		}
		return s.cache[i], nil
	}

	dir := s.root
	for _, p := range sp {
		f, err := dir.Search(p)
		if err != nil {
			return nil, err
		}
		dir = f
	}
	return dir, nil
}

func (s *Simple) ReadDir(name string) ([]fs.DirEntry, error) {
	switch name {
	case ".", "", "/":
		return s.root.objects, nil
	}
	name = strings.TrimPrefix(name, ".")
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	sp := strings.Split(name, "/")

	dir := s.root
	for _, p := range sp {
		f, err := dir.Search(p)
		if err != nil {
			return nil, fs.ErrNotExist
		}
		if !f.isDir {
			return nil, fs.ErrInvalid
		}
		dir = f
	}

	return dir.objects, nil
}

// ReadFile implememnts ReadFileFS.ReadFile(). The slice returned by ReadFile is not
// a copy of the file's contents like Open().File.Read() returns. Modifying it will
// modifiy the content so BE CAREFUL.
func (s *Simple) ReadFile(name string) ([]byte, error) {
	f, err := s.Open(name)
	if err != nil {
		return nil, err
	}
	r := f.(*file)
	if r.IsDir() {
		return nil, errors.New("cannot read a directory")
	}
	return r.content, nil
}

// WriteFile implememnts Writer. The content reference is copied, so modifying the original will
// modify it here.
func (s *Simple) WriteFile(name string, content []byte) error {
	if s.ro {
		return fmt.Errorf("Simple is locked from writing")
	}
	if name == "" {
		panic("can't write a file at root")
	}

	if strings.HasSuffix(name, "/") {
		return fmt.Errorf("cannot write a file directory(%s)", name)
	}

	name = strings.TrimPrefix(name, ".")
	name = strings.TrimPrefix(name, "/")

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	dir := s.root
	sp := strings.Split(name, "/")
	for i := 0; i < len(sp)-1; i++ {
		f, err := dir.Search(sp[i])
		if err != nil {
			dir.createDir(sp[i])
			f, err = dir.Search(sp[i])
			if err != nil {
				panic("wtf?")
			}
			dir = f
			continue
		}
		if !f.isDir {
			return fmt.Errorf("name(%s) contains element(%d)(%s) that is not a directory", name, i, sp[i])
		}
		dir = f
	}

	n := sp[len(sp)-1]
	if _, err := dir.Search(n); err == nil {
		return fs.ErrExist
	}

	dir.addFile(&file{name: n, content: content, time: time.Now()})
	s.items++

	return nil
}

// RO locks the file system from writing.
func (s *Simple) RO() {
	s.ro = true

	if s.pearson {
		sl := make([]*file, s.items)

		fs.WalkDir(
			s,
			".",
			func(path string, d fs.DirEntry, err error) error {
				if d.IsDir() {
					return nil
				}
				h := pearson([]byte(path))
				i := int(h) % (len(s.cache) + 1)
				sl[i] = d.(*file)
				return nil
			},
		)
		s.cache = sl
	}
}

type file struct {
	name    string
	content []byte
	offset  int64
	time    time.Time
	isDir   bool

	objects []fs.DirEntry
}

// createDir creates a new *file representing a dir inside this file (which must represent a dir).
func (f *file) createDir(name string) {
	if !f.isDir {
		panic("bug: createDir() called on file with isDir == false")
	}

	n := &file{name: name, isDir: true}
	f.objects = append(f.objects, n)
	sort.Slice(f.objects,
		func(i, j int) bool {
			return f.objects[i].Name() < f.objects[j].Name()
		},
	)
	s := []string{}
	for _, o := range f.objects {
		s = append(s, o.Name())
	}

	s = nil
	for _, o := range n.objects {
		s = append(s, o.Name())
	}

	return
}

func (f *file) addFile(nf *file) {
	if !f.isDir {
		panic("bug: cannot add a file to a non-directory")
	}
	f.objects = append(f.objects, nf)
	sort.Slice(f.objects,
		func(i, j int) bool {
			return f.objects[i].Name() < f.objects[j].Name()
		},
	)
}

// Search searches for the sub file named "name". This only works if isDir is true.
func (f *file) Search(name string) (*file, error) {
	if !f.isDir {
		return nil, errors.New("not a directory")
	}

	if len(f.objects) == 0 {
		return nil, fs.ErrNotExist
	}

	x := sort.Search(
		len(f.objects),
		func(i int) bool {
			return f.objects[i].(*file).name >= name
		},
	)
	if x < len(f.objects) && f.objects[x].(*file).name == name {
		return f.objects[x].(*file), nil
	}
	return nil, fs.ErrNotExist
}

func (f *file) Name() string {
	return f.name
}

func (f *file) IsDir() bool {
	return f.isDir
}

func (f *file) Type() fs.FileMode {
	return fileMode
}

func (f *file) Info() (fs.FileInfo, error) {
	fi, _ := f.Stat()
	return fi, nil
}

func (f *file) Stat() (fs.FileInfo, error) {
	return fileInfo{
		name:  f.name,
		size:  int64(len(f.content)),
		time:  f.time,
		isDir: f.isDir,
	}, nil
}

// Read implements io.Reader.
func (f *file) Read(b []byte) (int, error) {
	if f.isDir {
		return 0, fmt.Errorf("cannot Read() a directory")
	}
	if len(b) == 0 {
		return 0, nil
	}
	if int(f.offset) >= len(f.content) {
		return 0, io.EOF
	}
	i := copy(b, f.content[f.offset:])
	f.offset += int64(i)
	return i, nil
}

// Seek implement io.Seeker.
func (f *file) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return 0, fmt.Errorf("can't seek beyond start of file")
		}
		f.offset = offset
		return f.offset, nil
	case io.SeekCurrent:
		if f.offset + offset < 0 {
			return 0, fmt.Errorf("can't seek beyond start of file")
		}
		f.offset += offset
		return f.offset, nil
	case io.SeekEnd:
		if len(f.content) + int(offset) < 0 {
			return 0, fmt.Errorf("can't seek beyond start of file")
		}
		f.offset = int64(len(f.content)) + offset
		return f.offset, nil
	}
	return 0, fmt.Errorf("whence value was invalid(%d)", whence)
}

// Close implememnts io.Closer.
func (f *file) Close() error {
	return nil
}

type fileInfo struct {
	name  string
	size  int64
	time  time.Time
	isDir bool
}

func (f fileInfo) Name() string {
	return f.name
}

func (f fileInfo) Size() int64 {
	return f.size
}
func (f fileInfo) Mode() fs.FileMode {
	return fileMode
}
func (f fileInfo) ModTime() time.Time {
	return f.time
}
func (f fileInfo) IsDir() bool {
	return f.isDir
}
func (f fileInfo) Sys() interface{} {
	return nil
}
