package database

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// sqlx's named-query compiler reads `:name` greedily and treats the colon that
// starts a following type cast as a syntax error, so a named parameter with a
// cast appended makes the entire statement fail to compile before it ever
// reaches Postgres. The casts are unnecessary anyway — the jsonb Value()
// implementations return a string and Postgres infers the column type from the
// target — but the failure is invisible at build time and surfaces only as a
// runtime error on the write path, so guard against reintroduction here.
//
// See internal/database/debug/models.go (JSONMap) for the full explanation.
var namedParamCast = regexp.MustCompile(`:[a-zA-Z_][a-zA-Z_0-9]*::`)

// TestNamedQueriesCompile inspects string literals only. Scanning raw lines
// would also match the prose above, which is why this goes through the AST.
func TestNamedQueriesCompile(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if m := namedParamCast.FindString(s); m != "" {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d: named parameter followed by a cast (%q); "+
					"sqlx cannot compile this — drop the cast",
					filepath.ToSlash(rel), fset.Position(lit.Pos()).Line, m)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
