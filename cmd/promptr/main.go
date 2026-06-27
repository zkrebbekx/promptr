// Command promptr compiles .promptr schema files into Go.
//
// Usage:
//
//	promptr generate [path ...]   compile .promptr files (default ".")
//	promptr version               print version
//
// A path may be a .promptr file, a directory (its *.promptr files), or a
// "dir/..." pattern (recursive). Each foo.promptr is compiled to foo.promptr.go
// in the same directory, in that directory's Go package. Designed to run under
// //go:generate.
package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/zkrebbekx/promptr/codegen"
	"github.com/zkrebbekx/promptr/dsl"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "generate":
		os.Exit(cmdGenerate(os.Args[2:]))
	case "check":
		os.Exit(cmdCheck(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Println("promptr", version)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "promptr: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `promptr - typed prompt compiler

usage:
  promptr generate [-pkg name] [path ...]   compile .promptr files (default ".")
  promptr check [path ...]                  parse + validate without writing Go
  promptr version

`)
}

// cmdCheck parses and semantically validates .promptr files without generating
// Go, reporting syntax errors and resolution/test diagnostics. Exit 1 on any
// problem — suitable for CI and pre-commit.
func cmdCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	_ = fs.Parse(args)
	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	files, err := collect(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "promptr:", err)
		return 1
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "promptr: no .promptr files found")
		return 1
	}

	rc := 0
	for _, in := range files {
		srcBytes, rerr := os.ReadFile(in)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "promptr: %s: %v\n", in, rerr)
			rc = 1
			continue
		}
		f, perr := dsl.Parse(string(srcBytes))
		if perr != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", in, perr)
			rc = 1
		}
		diags := dsl.Validate(f)
		for _, d := range diags {
			fmt.Fprintf(os.Stderr, "%s:%d: %s\n", in, d.Line, d.Msg)
		}
		if len(diags) > 0 {
			rc = 1
		}
		if perr == nil && len(diags) == 0 {
			fmt.Println("promptr: ok", in)
		}
	}
	return rc
}

func cmdGenerate(args []string) int {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	pkg := fs.String("pkg", "", "override the Go package name (default: inferred)")
	_ = fs.Parse(args)

	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	files, err := collect(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "promptr:", err)
		return 1
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "promptr: no .promptr files found")
		return 1
	}

	rc := 0
	for _, in := range files {
		if err := compileFile(in, *pkg); err != nil {
			fmt.Fprintf(os.Stderr, "promptr: %s: %v\n", in, err)
			rc = 1
			continue
		}
		fmt.Println("promptr:", in, "->", outPath(in))
	}
	return rc
}

func compileFile(in, pkgOverride string) error {
	srcBytes, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	f, perr := dsl.Parse(string(srcBytes))
	if perr != nil {
		return perr
	}
	pkg := pkgOverride
	if pkg == "" {
		pkg = inferPackage(filepath.Dir(in))
	}
	out, err := codegen.Generate(pkg, f)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath(in), out, 0o644)
}

func outPath(in string) string {
	return strings.TrimSuffix(in, ".promptr") + ".promptr.go"
}

// collect resolves the path args to a deduplicated list of .promptr files.
func collect(paths []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range paths {
		switch {
		case strings.HasSuffix(p, "..."):
			root := strings.TrimSuffix(p, "...")
			if root == "" {
				root = "."
			}
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(path, ".promptr") {
					add(path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		default:
			info, err := os.Stat(p)
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				matches, _ := filepath.Glob(filepath.Join(p, "*.promptr"))
				for _, m := range matches {
					add(m)
				}
			} else {
				add(p)
			}
		}
	}
	return out, nil
}

// inferPackage reads the first Go file in dir for its package clause; failing
// that, it sanitizes the directory's base name into a valid identifier.
func inferPackage(dir string) string {
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.go")); len(matches) > 0 {
		fset := token.NewFileSet()
		for _, m := range matches {
			if strings.HasSuffix(m, ".promptr.go") {
				continue
			}
			af, err := parser.ParseFile(fset, m, nil, parser.PackageClauseOnly)
			if err == nil && af.Name != nil {
				return af.Name.Name
			}
		}
	}
	return sanitizeIdent(filepath.Base(dir))
}

func sanitizeIdent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
			b.WriteByte(c)
		case c >= '0' && c <= '9':
			if b.Len() == 0 {
				b.WriteByte('_')
			}
			b.WriteByte(c)
		}
	}
	if b.Len() == 0 {
		return "main"
	}
	return strings.ToLower(b.String())
}
