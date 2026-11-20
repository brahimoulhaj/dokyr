package i18n

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The messages the catalogs must cover are read from the source, so a message added to
// the code without its translations fails this test instead of showing up in English.

// sources says, per package, which calls carry a message for people and in which argument.
var sources = map[string]map[string]int{
	"api":         {"writeError": 3, "invalid": 1, "storeError": 3, "tr": 1},
	"gitsource":   {"invalidf": 0},
	"deploy":      {"printf": 0},
	"database":    {},
	"notify":      {},
	"objectstore": {},
	"hostcron":    {},
	"account":     {},
}

// messageOf turns the expression of a message into a catalog key: a literal as it is, a
// concatenation with %s where a value goes, the format of a Sprintf. "" when the message
// is not written there (an error's text, a variable).
func messageOf(e ast.Expr, format bool) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return ""
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return ""
		}
		if !format {
			s = strings.ReplaceAll(s, "%", "%%")
		}
		return s
	case *ast.ParenExpr:
		return messageOf(v.X, format)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return ""
		}
		part := func(x ast.Expr) string {
			if s := messageOf(x, false); s != "" {
				return s
			}
			return "%s"
		}
		key := part(v.X) + part(v.Y)
		if strings.Trim(strings.ReplaceAll(key, "%s", ""), " :;,.") == "" {
			return "" // nothing but values
		}
		return key
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && len(v.Args) > 0 {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" && sel.Sel.Name == "Sprintf" {
				return messageOf(v.Args[0], true)
			}
		}
	}
	return ""
}

func collectKeys(t *testing.T) map[string]string {
	t.Helper()
	keys := map[string]string{} // key → where it was first found
	add := func(key, where string) {
		// Text made of values and punctuation alone (a build's output line, "%s: %s") says
		// the same in every language.
		letters := 0
		for _, r := range verb.ReplaceAllString(key, "") {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		if letters < 3 {
			return
		}
		if _, ok := keys[key]; !ok {
			keys[key] = where
		}
	}
	fset := token.NewFileSet()
	for pkg, calls := range sources {
		files, err := filepath.Glob(filepath.Join("..", pkg, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no source for %s: %v", pkg, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(parsed, func(n ast.Node) bool {
				where := func(n ast.Node) string {
					p := fset.Position(n.Pos())
					return pkg + "/" + filepath.Base(p.Filename) + ":" + strconv.Itoa(p.Line)
				}
				switch v := n.(type) {
				case *ast.ValueSpec:
					// Sentinel errors, which lead "what: why" messages: var ErrX = errors.New("…").
					for i, name := range v.Names {
						if !strings.HasPrefix(name.Name, "Err") || i >= len(v.Values) {
							continue
						}
						if call, ok := v.Values[i].(*ast.CallExpr); ok && len(call.Args) == 1 {
							add(messageOf(call.Args[0], false), where(v))
						}
					}
				case *ast.CallExpr:
					name := ""
					switch f := v.Fun.(type) {
					case *ast.Ident:
						name = f.Name
					case *ast.SelectorExpr:
						name = f.Sel.Name
						// fmt.Errorf("%w: why", ErrInvalid, …): the why.
						if pkgName, ok := f.X.(*ast.Ident); ok && pkgName.Name == "fmt" && name == "Errorf" && len(v.Args) >= 2 {
							if why, ok := strings.CutPrefix(messageOf(v.Args[0], true), "%w: "); ok && why != "%s" {
								add(why, where(v))
							}
							return true
						}
					}
					if i, ok := calls[name]; ok && i < len(v.Args) {
						add(messageOf(v.Args[i], name == "printf" || name == "invalidf"), where(v))
					}
				}
				return true
			})
		}
	}
	return keys
}

func TestCatalogComplete(t *testing.T) {
	keys := collectKeys(t)
	if len(keys) < 150 {
		t.Fatalf("only %d messages found in the source: the scan is broken", len(keys))
	}
	if path := os.Getenv("I18N_DUMP"); path != "" {
		list := make([]string, 0, len(keys))
		for k := range keys {
			list = append(list, k)
		}
		sort.Strings(list)
		raw, _ := json.MarshalIndent(list, "", " ")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	verbs := func(s string) []string {
		var out []string
		for _, m := range verb.FindAllStringSubmatch(s, -1) {
			if m[0] != "%%" {
				out = append(out, m[0])
			}
		}
		return out
	}
	for lang, cat := range catalogs {
		var missing []string
		for key, where := range keys {
			if cat[key] == "" {
				missing = append(missing, where+"  "+strconv.Quote(key))
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s: %d messages have no translation:\n  %s", lang, len(missing), strings.Join(missing, "\n  "))
		}
		for key, to := range cat {
			if _, ok := keys[key]; !ok {
				t.Errorf("%s: %q is not a message of the source any more; remove it or fix its text", lang, key)
			}
			// A translation takes the values its message has: as many, or by index.
			from, into := verbs(key), verbs(to)
			indexed := strings.Contains(to, "%[")
			if (!indexed && len(from) != len(into)) || (indexed && len(into) > len(from)) {
				t.Errorf("%s: %q has %d values, its translation %q uses %d", lang, key, len(from), to, len(into))
			}
			for _, v := range into {
				if m := verb.FindStringSubmatch(v); m[2] != "" {
					if n, _ := strconv.Atoi(m[2]); n < 1 || n > len(from) {
						t.Errorf("%s: %q refers to value %d, which %q does not have", lang, to, n, key)
					}
				}
			}
		}
	}
}
