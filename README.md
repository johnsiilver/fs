# fs
Utilities to help with the io/fs package

## Introduction

The io.FS package is a great addition into Go 1.16 . To start with, its main use seems to be around using the embed package.

The embed package allows embedding files at compile time. But it has this negative consquence of really needing to embed
the files somewhere close to the root into a single FS object.  

I was lookin for a way to embed files at each package layer and them merge them into a single FS. This feels more managable to me
in that I have each package handling its own embed and then one file just merging the embed FS that is in each package.

I also wanted to have a file system I could write files into after compile time and them make read only.

## Simple FS

The Simple FS is a simplistic filesystem that has all the basics needed for an fs.FS. 
As it states, you really want to stay ASCII and not try to get fancy with /\/ kind of things.

It comes with an option call `WithPearson()` that uses a Pearson hash to do real O(1) non-collision file lookups. 

Creation is easy:

```go
	sfs := fs.NewSimple(fs.WithPearson())
```

Writing a file is simple:

```go
	if err := sfs.WriteFile("path/to/file", []byte("hello world"); err != nil {
		// Do something
	}
```

Once we are done writing, we simply need to set our Simple FS to ReadOnly:
```go
	sfs.RO()
```

Reading a file is just as easy:

```go
	b, err := sfs.Readfile("path/to/file")
	if err != nil {
		// Do something
	}
	fmt.Println(string(b))
```

And there are various other methods.

But what makes Simple FS particularly useful (besides being writable) is using it as a central location to merge other io.FS into a single structure:

```go
	if err := fs.Merge(sfs, pkg.FS, "into/sub/directory/"); err != nil {
		// Do something
	}
```
The above merge method will add all the content of pkg.FS and store it in a directory
from our sfs root "into/sub/directory". This is a recursive walk and will contain all the files.
