package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Exercise each literal assigned to a UI error state at narrow dialog widths.
// Dynamic errors are covered by the per-screen error-state tests.
func TestLiteralErrorMessagesWrapWithoutLosingText(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	styles := stylesForTheme("macchiato")
	count := 0
	isErrorField := func(name string) bool {
		switch name {
		case "err", "pinError", "validation":
			return true
		default:
			return false
		}
	}
	check := func(expr ast.Expr) {
		literal, ok := expr.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return
		}
		message, err := strconv.Unquote(literal.Value)
		if err != nil || message == "" {
			return
		}
		count++
		for _, width := range []int{24, 38, 56} {
			lines := renderErrorLines(message, width, &styles)
			if strings.Join(strings.Fields(ansi.Strip(strings.Join(lines, ""))), "") != strings.Join(strings.Fields(safe(message)), "") {
				t.Errorf("error message lost text when wrapped at width %d", width)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Errorf("error line exceeds width %d", width)
				}
			}
		}
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					if field, ok := lhs.(*ast.SelectorExpr); ok && isErrorField(field.Sel.Name) && i < len(n.Rhs) {
						check(n.Rhs[i])
					}
				}
			case *ast.KeyValueExpr:
				if key, ok := n.Key.(*ast.Ident); ok && isErrorField(key.Name) {
					check(n.Value)
				}
			}
			return true
		})
	}
	if count == 0 {
		t.Fatal("no literal error states found")
	}
}
