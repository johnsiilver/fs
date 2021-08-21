/*
Package disk provides an FS that wraps the johnsiilver/fs/os package to be
used for a disk cache that expires files.
*/
package disk

import (
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	jsfs "github.com/johnsiilver/fs"
	"github.com/johnsiilver/fs/cache"
	osfs "github.com/johnsiilver/fs/os"
)

var _ cache.CacheFS = &FS{}

// FS provides a disk cache based on the johnsiilver/fs/os package. FS must have
// Close() called to stop internal goroutines.
type FS struct {
	fs *osfs.FS

	location       string
	openTimeout    time.Duration
	expireDuration time.Duration
	index          *index

	writeFileOFOptions []writeFileOptions

	closeCh   chan struct{}
	checkTime time.Duration
}

// Option is an optional argument for the New() constructor.
type Option func(f *FS) error

// WithExpireCheck changes at what interval we check for file expiration.
func WithExpireCheck(d time.Duration) Option {
	return func(f *FS) error {
		f.checkTime = d
		return nil
	}
}

func WithExpireFiles(d time.Duration) Option {
	return func(f *FS) error {
		f.expireDuration = d
		return nil
	}
}

type writeFileOptions struct {
	regex   *regexp.Regexp
	options []jsfs.OFOption
}

// WithWriteFileOFOption uses a regex on the file path given and if it matches
// will apply the options provided on that file when .WriteFile() is called.
// First match wins. A "nil" for a regex applies to all that are not matched. It is suggested
// for speed reasons to keep this relatively small or the first rules should match
// the majority of files. This can be passed multiple times with different regexes.
func WithWriteFileOFOptions(regex *regexp.Regexp, options ...jsfs.OFOption) Option {
	return func(f *FS) error {
		f.writeFileOFOptions = append(f.writeFileOFOptions, writeFileOptions{regex: regex, options: options})
		return nil
	}
}

// New creates a new FS that uses disk located at 'location' to store cache data.
// If location == "", a new cache root is setup in TEMPDIR with prepended name
// "diskcache_". It is the responsibility of the caller to cleanup the disk.
func New(location string, options ...Option) (*FS, error) {
	fs := &osfs.FS{}

	if location == "" {
		var err error
		location, err = ioutil.TempDir("", "diskcache_")
		if err != nil {
			return nil, err
		}
	} else {
		fi, err := fs.Stat(location)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("location(%s) was not a directory", location)
		}
	}

	sys := &FS{
		location:       location,
		expireDuration: 30 * time.Minute,
		fs:             fs,
		openTimeout:    3 * time.Second,
		checkTime:      1 * time.Minute,
	}

	for _, o := range options {
		if err := o(sys); err != nil {
			return nil, err
		}
	}

	sys.index = newIndex(location, sys.expireDuration)

	go sys.expireLoop()

	return sys, nil
}

func (f *FS) Close() {
	close(f.closeCh)
}

// Location returns the location of our disk cache.
func (f *FS) Location() string {
	return f.location
}

// Open implements fs.FS.Open(). fs.File is an *johnsiilver/fs/os/File.
func (f *FS) Open(name string) (fs.File, error) {
	file, err := f.OpenFile(name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}

	return file, nil
}

type ofOptions struct {
	mode fs.FileMode
}

func (o *ofOptions) defaults() {
	o.mode = 0644
}

func (o ofOptions) toOsOFOptions() []jsfs.OFOption {
	var options []jsfs.OFOption
	if o.mode != 0 {
		options = append(options, osfs.FileMode(o.mode))
	}
	return options
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

// OpenFile implements fs.OpenFiler.OpenFile(). We support os.O_CREATE, os.O_EXCL, os.O_RDONLY, os.O_WRONLY,
// and os.O_TRUNC. If OpenFile is passed O_RDONLY, this calls Open() and ignores all options.
// When writing a file, the file is not written until Close() is called on the file.
func (f *FS) OpenFile(name string, flags int, options ...jsfs.OFOption) (fs.File, error) {
	name = strings.Replace(name, "/", "_slash_", -1)
	opts := &ofOptions{}
	opts.defaults()

	for _, o := range options {
		if err := o(opts); err != nil {
			return nil, err
		}
	}

	log.Printf("OpenFile sees(%v): %s", opts.mode, path.Join(f.location, name))
	file, err := f.fs.OpenFile(path.Join(f.location, name), flags, opts.toOsOFOptions()...)
	if err != nil {
		return nil, err
	}

	f.index.addOrUpdate(name)

	return file, nil
}

// ReadFile implements fs.ReadFileFS.ReadFile().
func (f *FS) ReadFile(name string) ([]byte, error) {
	file, err := f.Open(name)
	if err != nil {
		return nil, err
	}

	return io.ReadAll(file)
}

func (f *FS) Stat(name string) (fs.FileInfo, error) {
	name = strings.Replace(name, "/", "_slash_", -1)
	return f.fs.Stat(path.Join(f.location, name))
}

func (f *FS) WriteFile(name string, content []byte, perm fs.FileMode) error {
	opts := []jsfs.OFOption{}

	for _, wfo := range f.writeFileOFOptions {
		if wfo.regex == nil {
			for _, o := range wfo.options {
				opts = append(opts, o)
			}
			break
		}
		if wfo.regex.MatchString(name) {
			for _, o := range wfo.options {
				opts = append(opts, o)
			}
			break
		}
	}

	log.Println("writeFile sees: ", name)
	file, err := f.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, opts...)
	if err != nil {
		return err
	}
	log.Println("get here")

	rFile := file.(*osfs.File)
	_, err = rFile.Write(content)
	if err != nil {
		return err
	}

	f.index.addOrUpdate(name)

	return err
}

func (f *FS) expireLoop() {
	for {
		select {
		case <-f.closeCh:
			return
		case <-time.After(f.checkTime):
			f.index.deleteOld()
		}
	}
}
