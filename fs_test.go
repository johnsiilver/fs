package fs

import (
	"bytes"
	"crypto/md5"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

//go:embed fs.go fs_test.go go.mod
var FS embed.FS

var (
	fsmd5, fstestmd5 string
)

func mustRead(fsys fs.FS, name string) []byte {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		panic(err)
	}
	return b
}

func md5Sum(b []byte) string {
	h := md5.New()
	h.Write(mustRead(FS, "fs.go"))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestMerge(t *testing.T) {
	simple := NewSimple(WithPearson())
	simple.WriteFile("/where/the/streets/have/no/name/u2.txt", []byte("joshua tree"))

	if err := Merge(simple, FS, "/songs/"); err != nil {
		panic(err)
	}
	simple.RO()

	if err := simple.WriteFile("/some/file", []byte("who cares")); err == nil {
		t.Fatalf("TestMerge(write after .RO()): should not be able to write, but did")
	}

	pathsToCheck := []string{
		"songs",
		"where",
		"where/the",
		"where/the/streets",
		"where/the/streets/have",
		"where/the/streets/have/no",
		"where/the/streets/have/no/name",
	}

	for _, p := range pathsToCheck {
		fi, err := fs.Stat(simple, p)
		if err != nil {
			t.Fatalf("TestMerge(stat dir): (%s) err: %s", p, err)
		}
		if !fi.IsDir() {
			t.Fatalf("TestMerge(fi.IsDir): (%s) was false", p)
		}
	}

	fs.WalkDir(simple, ".",
		func(path string, d fs.DirEntry, err error) error {
			log.Println("simple walk: ", path)
			return nil
		},
	)

	b, err := simple.ReadFile("where/the/streets/have/no/name/u2.txt")
	if err != nil {
		t.Fatalf("TestMerge(simple.ReadFile): expected file gave error: %s", err)
	}
	if bytes.Compare(b, []byte("joshua tree")) != 0 {
		t.Fatalf("TestMerge(simple.ReadFile): -want/+got:\n%s", pretty.Compare("joshua tree", string(b)))
	}

	if md5Sum(mustRead(simple, "songs/fs.go")) != md5Sum(mustRead(FS, "fs.go")) {
		t.Fatalf("TestMerge(md5 check on fs.go): got %q, want %q", md5Sum(mustRead(simple, "songs/fs.go")), md5Sum(mustRead(FS, "fs.go")))
	}
	if md5Sum(mustRead(simple, "songs/fs_test.go")) != md5Sum(mustRead(FS, "fs_test.go")) {
		t.Fatalf("TestMerge(md5 check on fs_test.go): got %q, want %q", md5Sum(mustRead(simple, "songs/fs_test.go")), md5Sum(mustRead(FS, "fs_test.go")))
	}
}
