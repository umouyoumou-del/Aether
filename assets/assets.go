// Package assets 嵌入五行的运行时库（Go 源码）和标准库（.ae 源码）。
package assets

import (
	"embed"
	"io/fs"
)

//go:embed rt
var rtFS embed.FS

//go:embed lib
var libFS embed.FS

// RT 返回运行时源码目录（rt/*.go 及其子包）。
func RT() fs.FS {
	sub, err := fs.Sub(rtFS, "rt")
	if err != nil {
		panic(err)
	}
	return sub
}

// Lib 返回标准库目录（lib/*.ae）。
func Lib() fs.FS {
	sub, err := fs.Sub(libFS, "lib")
	if err != nil {
		panic(err)
	}
	return sub
}
