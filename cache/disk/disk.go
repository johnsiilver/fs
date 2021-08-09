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
	"os"
	"path"
	"regexp"
	"sync"
	"time"

	jsfs "github.com/johnsiilver/fs"
	"github.com/johnsiilver/fs/cache"
	osfs "github.com/johnsiilver/fs/os"
	"github.com/petar/GoLLRB/llrb"
)

var _ cache.CacheFS = &FS{}

type expireKey struct {
	time.Time

	name string
}

func (e expireKey) Less(than llrb.Item) bool {
	return than.(expireKey).Before(e.Time)
}

// FS provides a disk cache based on the johnsiilver/fs/os package. FS must have
// Close() called to stop internal goroutines.
type FS struct {
	fs *osfs.FS

	location       string
	openTimeout    time.Duration
	expireDuration time.Duration

	writeFileOFOptions []writeFileOptions

	mu      sync.Mutex
	expires *llrb.LLRB

	closeCh   chan struct{}
	checkTime time.Duration
}

// Option is an optional argument for the New() constructor.
type Option func(f *FS) error

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
		fs:             fs,
		openTimeout:    3 * time.Second,
		expires:        llrb.New(),
		expireDuration: 30 * time.Minute,
		checkTime:      1 * time.Minute,
	}

	for _, o := range options {
		if err := o(sys); err != nil {
			return nil, err
		}
	}

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
	file, err := f.fs.Open(path.Join(f.location, name))
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.expires.ReplaceOrInsert(expireKey{Time: time.Now(), name: name})
	f.mu.Unlock()

	return file, nil
}

type ofOptions struct {
	mode        fs.FileMode
	expireFiles time.Duration
}

func (o *ofOptions) defaults() {
	o.expireFiles = 30 * time.Minute
}

func (o ofOptions) toOsOFOptions() []jsfs.OFOption {
	var options []jsfs.OFOption
	if o.mode != 0 {
		options = append(options, osfs.FileMode(o.mode))
	}
	return options
}

// ExpireFiles expires files at duration d. If not set for a file, redis.KeepTTL is used.
func ExpireFiles(d time.Duration) jsfs.OFOption {
	return func(o interface{}) error {
		opts, ok := o.(*ofOptions)
		if !ok {
			return fmt.Errorf("bug: redis.ofOptions was not passed(%T)", o)
		}
		opts.expireFiles = d
		return nil
	}
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
	opts := ofOptions{}
	opts.defaults()

	for _, o := range options {
		o(&opts)
	}

	file, err := f.fs.OpenFile(path.Join(f.location, name), flags, opts.toOsOFOptions()...)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.expires.ReplaceOrInsert(expireKey{Time: time.Now(), name: name})
	f.mu.Unlock()

	return file, nil
}

// ReadFile implements fs.ReadFileFS.ReadFile().
func (f *FS) ReadFile(name string) ([]byte, error) {
	file, err := f.Open(path.Join(f.location, name))
	if err != nil {
		return nil, err
	}
	r := file.(*osfs.File)

	f.mu.Lock()
	f.expires.ReplaceOrInsert(expireKey{Time: time.Now(), name: name})
	f.mu.Unlock()

	return io.ReadAll(r)
}

func (f *FS) Stat(name string) (fs.FileInfo, error) {
	return f.fs.Stat(path.Join(f.location, name))
}

func (f *FS) WriteFile(name string, content []byte, perm fs.FileMode) error {
	name = path.Join(f.location, name)
	var opts = []jsfs.OFOption{
		osfs.FileMode(perm),
	}

	for _, wfo := range f.writeFileOFOptions {
		if wfo.regex == nil {
			opts = wfo.options
			break
		}
		if wfo.regex.MatchString(name) {
			opts = wfo.options
			break
		}
	}

	file, err := f.fs.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, opts...)
	if err != nil {
		return err
	}

	rFile := file.(*osfs.File)
	_, err = rFile.Write(content)
	if err != nil {
		return err
	}

	f.mu.Lock()
	f.expires.ReplaceOrInsert(expireKey{Time: time.Now(), name: name})
	f.mu.Unlock()

	return err
}

func (f *FS) expireLoop() {
	for {
		select {
		case <-f.closeCh:
		case <-time.After(f.checkTime):
		}
		f.mu.Lock()
		f.expires.AscendLessThan(
			expireKey{Time: time.Now().Add(-f.expireDuration)},
			f.expireItem,
		)
		f.mu.Lock()
	}
}

func (f *FS) expireItem(item llrb.Item) bool {
	ek := item.(expireKey)
	f.expires.Delete(ek)
	return true
}
