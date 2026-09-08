// Command format checks handwritten Go files; -w applies formatting instead.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	write := flag.Bool("w", false, "write formatted source files")
	flag.Parse()
	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"cmd", "config", "global", "internal", "pkg", "scripts"}
	}

	failed := false
	for _, root := range paths {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.Type().IsRegular() || filepath.Ext(path) != ".go" {
				return nil
			}
			changed, err := formatFile(path, *write)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if changed {
				fmt.Println(path)
				if !*write {
					failed = true
				}
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
		}
	}
	if failed {
		fmt.Fprintln(os.Stderr, "format check failed; run make fmt to format handwritten Go files")
		os.Exit(1)
	}
}

func formatFile(path string, write bool) (bool, error) {
	source, err := os.ReadFile(path) // #nosec G304 -- This local CLI intentionally reads the Go source paths selected by the developer.
	if err != nil {
		return false, err
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ParseComments|parser.PackageClauseOnly)
	if err != nil {
		return false, err
	}
	if ast.IsGenerated(file) {
		return false, nil
	}
	formatted, err := format.Source(source)
	if err != nil {
		return false, err
	}
	if bytes.Equal(source, formatted) {
		return false, nil
	}
	if write {
		if err := os.WriteFile(path, formatted, 0o600); err != nil { // #nosec G703 -- -w explicitly authorizes formatting the developer-selected source file.
			return false, err
		}
	}
	return true, nil
}
