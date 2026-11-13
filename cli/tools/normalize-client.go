package main

import (
	"bytes"
	"flag"
	"go/format"
	"go/parser"
	"go/token"
	"os"
)

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		panic("expected a generated Go client path")
	}
	path := flag.Arg(0)
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, path, nil, 0)
	if err != nil {
		panic(err)
	}
	var source bytes.Buffer
	if err := format.Node(&source, files, parsed); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, source.Bytes(), 0600); err != nil {
		panic(err)
	}
}
