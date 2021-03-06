# fs
In memory file system, "os" based file system and utilities
 
This package contains:

- Utilities to help with the io/fs package
- fs.Simple, a writeable in-memory io.FS
- os.FS - an io.FS that uses the os package

## Introduction

The io.FS package is a great addition into Go 1.16 . To start with, its main use seems to be around using the embed package.

The embed package allows embedding files at compile time. But it has this negative consquence of really needing to embed the files somewhere close to the root into a single FS object.  

I was looking for a way to embed files at each package layer and them merge them into a single FS. This feels more managable to me in that I have each package handling its own embed and then one file just merging the embed FS that is in each package.

I also wanted a way to optionally transform the files if I merged them into the new file system. One of the use cases is that I like to optimize JS an CSS files, but only when I'm not debugging. This provides an optional transform package that allows for me to optimize those files on the fly during startup in non-debug mode.

I also wanted to have a file system I could write files into after compile time and them make read only.

## Simple FS

The Simple FS is a simplistic filesystem that has all the basics needed for an fs.FS. As it states, you really want to stay with ASCII names and not try to get fancy with /\/ kind of things.

It comes with an option called `WithPearson()` that uses a Pearson hash to do real O(1) non-collision file lookups.
This can only be used if you have ASCII characters in your file name.

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
The above merge method will add all the content of pkg.FS and store it in a directory from our sfs root "into/sub/directory". This is a recursive walk and will contain all the files.

If you want to modify files before they are copied (compress certain files, optimize them or rewrite them in any way), use the `WithTransform()` option. 

## os.FS

Our sub-directory `os/` contains a io.FS that uses the underlying `os` package. I couldn't seem to find a package that provided this and it was simple to add.

This package has all the same features of our `Simple` fs, but with all the advanced features that a mature package like `os` provides.