/*
Package cache provides helpers for building a caching system based on io/fs.FS.

Cache libraries are wrapped to implement cache.CacheFS and are added to our cache.FS.

A call to FS.ReadFile() will read from the cache and on a cache miss will read from
storage. If storage has the file, the file is loaded into the cache.

FS also implements CacheFS, so we can build muli-level caches such as an in-memory
cache that pulls from a disk cache which pulls from Redis which pulls from Azure
Blob Storage in a waterfall like system.

Create our FS that uses long term storage (Azure Blob Storage):
	cred, err := msi.Token(msi.AppID{ID: "your app ID"})
	if err != nil {
		panic(err)
	}

	cloudStore, err := azfs.NewFS("account", "container", *cred)
	if err != nil {
		// Do something
	}

Create a Redis CacheFS:
	redisFS := redit.New(
		redis.Args{
			Addr:     "localhost:6379",
        	Password: "", // no password set
        	DB:       0,  // use default DB
		},
	)

Setup our first cache layer:
	networkCache, err := cache.New(redisFS, cloudStore)
	if err != nil {
		// Do something
	}

Setup our local disk FS:
	diskFS, err := disk.New("")
	if err != nil {
		// Do something
	}

Create our second cache layer:
	diskCache, err := cache.New(diskFS, networkCache)
	if err != nil {
		// Do something
	}

Create our memory cache;
	memCache, err := memfs.New()
	if err != nil {
		// Do something
	}

Create our final cache:
	cacheSys, err := cache.New(memCache, diskCache)
	if err != nil {
		// Do something
	}

Get a file from our cache:
	// This first attempts to read this from memory. If it doesn't exist, it
	// attempts to reach it from our disk. If it doesn't exist, it tries to
	// read from Redis. If it doesn't exist, it reads it from Azure blob storage.
	// Once the file is found, we backfill each layer. This works best when
	// each layer down holds the data for longer than the previous layer until
	// you reach permanent storage.
	b, err := cacheSys.ReadFile("/path/to/file")
	if err != nil {
		// Do something
	}
*/
package cache

import (
	"io/fs"
	"log"
	"os"

	jsfs "github.com/johnsiilver/fs"
)

var _ CacheFS = &FS{}

// CacheFS represents some cache that we can read and write files from.
type CacheFS interface {
	jsfs.Writer
	fs.ReadFileFS
	fs.StatFS
}

// FS implemenents io/fs.FS to provide a cache reader and writer.
type FS struct {
	cache, store CacheFS

	Log jsfs.Logger
}

// New is the constructor for FS.
func New(cache CacheFS, store CacheFS) (*FS, error) {
	return &FS{
		cache: cache,
		store: store,
		Log:   log.New(os.Stderr, "", log.LstdFlags),
	}, nil
}

// Open opens a file for reading. The file will be served out of cache to start
// and if not available it will be served out of storage. Using Open() does NOT
// cause a non-cached file to be cache.
func (f *FS) Open(name string) (fs.File, error) {
	file, err := f.cache.Open(name)
	if err == nil {
		return file, nil
	}

	return f.store.Open(name)
}

// OpenFile implements fs.OpenFiler.OpenFile().
func (f *FS) OpenFile(name string, flags int, options ...jsfs.OFOption) (fs.File, error) {
	if isFlagSet(flags, os.O_RDONLY) {
		return f.Open(name)
	}

	return f.store.OpenFile(name, flags, options...)
}

func isFlagSet(flags, flag int) bool {
	return flags&flag != 0
}

// ReadFile reads a file. This checks the cache first and then checks storage.
// If the file is found in storage, a call to the cache's WriteFile() is made
// in a separate go routine so that it is served out of cache in the future.
func (f *FS) ReadFile(name string) ([]byte, error) {
	b, err := f.cache.ReadFile(name)
	if err == nil {
		return b, nil
	}

	b, err = f.store.ReadFile(name)
	if err != nil {
		return b, err
	}

	go func() {
		if err := f.cache.WriteFile(name, b, 0644); err != nil {
			f.Log.Printf("problem writing file to cache(%T): %s", f.cache, err)
		}
	}()

	return b, nil
}

// WriteFile implememnts jsfs.Writer.WriteFile().
func (f *FS) WriteFile(name string, content []byte, perm fs.FileMode) error {
	return f.store.WriteFile(name, content, perm)
}

// Stat implememnts fs.StatFS.Stat().
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	fi, err := f.cache.Stat(name)
	if err == nil {
		return fi, err
	}
	return f.store.Stat(name)
}
