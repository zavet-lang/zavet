// Package docgen — автогенерация Markdown-документации API из исходников Завета (.молитва).
// Извлекает комментарии (#) непосредственно перед объявлениями:
// открой слово / образ / род / пусть, а также верхнеуровневые слова, образы и роды.
package docgen

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zavet/ast"
	"zavet/lexer"
	"zavet/parser"
)

// DocItem — один документируемый элемент модуля.
type DocItem struct {
	Kind       string // "слово", "образ", "род", "пусть"
	Name       string
	Signature  string
	Exported   bool
	Line       int
	Doc        string
	Methods    []DocItem // методы для образов
	ParentName string    // предок для образов
	Values     []string  // значения для родов
}

// FileDoc — документация одного файла .молитва.
type FileDoc struct {
	Path      string
	ModuleDoc string
	Items     []DocItem
}

// GenerateFile разбирает один файл .молитва и возвращает Markdown-документацию.
func GenerateFile(path string, onlyExported bool) (*FileDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return GenerateSource(path, string(data), onlyExported)
}

// GenerateSource разбирает исходный код Завета и извлекает API-документацию.
func GenerateSource(name, source string, onlyExported bool) (*FileDoc, error) {
	l := lexer.NewWithFilename(source, name)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("ошибки парсинга %s:\n  %s", name, strings.Join(p.Errors(), "\n  "))
	}

	rawLines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	doc := &FileDoc{
		Path:      name,
		ModuleDoc: extractModuleHeader(rawLines),
	}

	for _, stmt := range prog.Statements {
		exported := false
		inner := stmt
		line := stmt.GetToken().Line
		if exp, ok := stmt.(*ast.ExportStatement); ok {
			exported = true
			inner = exp.Statement
			if line == 0 {
				line = inner.GetToken().Line
			}
		}
		if onlyExported && !exported {
			continue
		}

		item, ok := extractStmtDoc(inner, rawLines, line, exported)
		if ok {
			doc.Items = append(doc.Items, item)
		}
	}

	return doc, nil
}

// GeneratePath генерирует Markdown для файла или всех .молитва в папке.
func GeneratePath(target string, onlyExported bool) (string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}

	var files []string
	if !info.IsDir() {
		files = []string{target}
	} else {
		err = filepath.WalkDir(target, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() {
				base := d.Name()
				if base == ".git" || base == "пакеты" || base == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ".молитва") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		sort.Strings(files)
	}

	var sb strings.Builder
	sb.WriteString("# Документация API\n\n")
	if len(files) == 0 {
		sb.WriteString("_Файлы `.молитва` не найдены._\n")
		return sb.String(), nil
	}

	for _, f := range files {
		fd, err := GenerateFile(f, onlyExported)
		if err != nil {
			return "", err
		}
		sb.WriteString(RenderMarkdown(fd))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// RenderMarkdown форматирует FileDoc в Markdown.
func RenderMarkdown(fd *FileDoc) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## `%s`\n\n", filepath.ToSlash(fd.Path)))
	if fd.ModuleDoc != "" {
		sb.WriteString(fd.ModuleDoc + "\n\n")
	}
	if len(fd.Items) == 0 {
		sb.WriteString("_Нет документируемых объявлений._\n")
		return sb.String()
	}

	for _, it := range fd.Items {
		badge := ""
		if it.Exported {
			badge = " _(открой)_"
		}
		sb.WriteString(fmt.Sprintf("### %s `%s`%s\n\n", it.Kind, it.Name, badge))
		if it.Signature != "" {
			sb.WriteString("```\n" + it.Signature + "\n```\n\n")
		}
		if it.Doc != "" {
			sb.WriteString(it.Doc + "\n\n")
		}
		if len(it.Values) > 0 {
			sb.WriteString("Значения: " + strings.Join(it.Values, ", ") + "\n\n")
		}
		if len(it.Methods) > 0 {
			sb.WriteString("**Методы:**\n\n")
			for _, m := range it.Methods {
				sb.WriteString(fmt.Sprintf("- `%s`", m.Signature))
				if m.Doc != "" {
					sb.WriteString(" — " + strings.ReplaceAll(m.Doc, "\n", " "))
				}
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func extractModuleHeader(lines []string) string {
	var out []string
	for _, raw := range lines {
		t := strings.TrimSpace(raw)
		if t == "" {
			if len(out) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(t, "#") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(t, "#")))
		} else {
			break
		}
	}
	// Если после комментария сразу идёт код без пустой строки, это doc-комментарий объявления, а не модуля.
	if len(out) > 0 && len(out) < len(lines) && strings.TrimSpace(lines[len(out)]) != "" {
		return ""
	}
	return strings.Join(out, "\n")
}

func extractDocComment(lines []string, line1Based int) string {
	idx := line1Based - 2 // строка прямо перед объявлением
	var rev []string
	for idx >= 0 {
		t := strings.TrimSpace(lines[idx])
		if strings.HasPrefix(t, "#") {
			rev = append(rev, strings.TrimSpace(strings.TrimPrefix(t, "#")))
			idx--
		} else {
			break
		}
	}
	if len(rev) == 0 {
		return ""
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return strings.Join(rev, "\n")
}

func extractStmtDoc(stmt ast.Statement, lines []string, line int, exported bool) (DocItem, bool) {
	switch s := stmt.(type) {
	case *ast.LetStatement:
		doc := extractDocComment(lines, line)
		switch s.Token.Type {
		case lexer.CLASS:
			it := DocItem{
				Kind:     "образ",
				Name:     s.Name.Value,
				Exported: exported,
				Line:     line,
				Doc:      doc,
			}
			if hash, ok := s.Value.(*ast.HashLiteral); ok {
				staticConst := map[string]bool{}
				staticTypes := map[string]string{}
				if sc, exists := hash.Pairs[&ast.StringLiteral{Value: "__static_const__"}]; exists {
					_ = sc
				}
				for _, k := range hash.KeyOrder {
					ks, isStr := k.(*ast.StringLiteral)
					if !isStr {
						continue
					}
					switch ks.Value {
					case "__static_const__":
						if arr, ok := hash.Pairs[k].(*ast.ArrayLiteral); ok {
							for _, el := range arr.Elements {
								if s2, ok := el.(*ast.StringLiteral); ok {
									staticConst[s2.Value] = true
								}
							}
						}
					case "__static_types__":
						if th, ok := hash.Pairs[k].(*ast.HashLiteral); ok {
							for tk, tv := range th.Pairs {
								if tks, ok1 := tk.(*ast.StringLiteral); ok1 {
									if tvs, ok2 := tv.(*ast.StringLiteral); ok2 {
										staticTypes[tks.Value] = tvs.Value
									}
								}
							}
						}
					}
				}
				for _, k := range hash.KeyOrder {
					ks, isStr := k.(*ast.StringLiteral)
					if !isStr {
						continue
					}
					if ks.Value == "__parent_name__" {
						if ps, ok := hash.Pairs[k].(*ast.StringLiteral); ok {
							it.ParentName = ps.Value
						}
						continue
					}
					if strings.HasPrefix(ks.Value, "__") {
						continue
					}
					v := hash.Pairs[k]
					if fn, ok := v.(*ast.FunctionLiteral); ok {
						mLine := fn.Token.Line
						displayName := ks.Value
						if displayName == "инициализация" {
							displayName = "создать"
						}
						it.Methods = append(it.Methods, DocItem{
							Kind:      "слово",
							Name:      displayName,
							Signature: formatFnSig(displayName, fn),
							Line:      mLine,
							Doc:       extractDocComment(lines, mLine),
						})
					} else if v != nil {
						kw := "статичная измени"
						if staticConst[ks.Value] {
							kw = "статичная пусть"
						}
						sig := kw + " " + ks.Value
						if tn := staticTypes[ks.Value]; tn != "" {
							sig += ": " + tn
						}
						fLine := ks.Token.Line
						it.Methods = append(it.Methods, DocItem{
							Kind:      kw,
							Name:      ks.Value,
							Signature: sig,
							Line:      fLine,
							Doc:       extractDocComment(lines, fLine),
						})
					}
				}
				sort.Slice(it.Methods, func(i, j int) bool { return it.Methods[i].Line < it.Methods[j].Line })
			}
			if it.ParentName != "" {
				it.Signature = fmt.Sprintf("образ %s наследует %s", it.Name, it.ParentName)
			} else {
				it.Signature = fmt.Sprintf("образ %s", it.Name)
			}
			return it, true

		case lexer.ENUM:
			it := DocItem{
				Kind:     "род",
				Name:     s.Name.Value,
				Signature: fmt.Sprintf("род %s", s.Name.Value),
				Exported: exported,
				Line:     line,
				Doc:      doc,
			}
			if hash, ok := s.Value.(*ast.HashLiteral); ok {
				for k := range hash.Pairs {
					if ks, ok := k.(*ast.StringLiteral); ok && !strings.HasPrefix(ks.Value, "__") {
						it.Values = append(it.Values, "`"+ks.Value+"`")
					}
				}
				sort.Strings(it.Values)
			}
			return it, true

		default:
			if fn, ok := s.Value.(*ast.FunctionLiteral); ok {
				return DocItem{
					Kind:      "слово",
					Name:      s.Name.Value,
					Signature: formatFnSig(s.Name.Value, fn),
					Exported:  exported,
					Line:      line,
					Doc:       doc,
				}, true
			}
			sig := "пусть " + s.Name.Value
			if s.TypeName != "" {
				sig += ": " + s.TypeName
			}
			return DocItem{
				Kind:      "пусть",
				Name:      s.Name.Value,
				Signature: sig,
				Exported:  exported,
				Line:      line,
				Doc:       doc,
			}, true
		}

	case *ast.ExpressionStatement:
		if fn, ok := s.Expression.(*ast.FunctionLiteral); ok && fn.Name != nil {
			return DocItem{
				Kind:      "слово",
				Name:      fn.Name.Value,
				Signature: formatFnSig(fn.Name.Value, fn),
				Exported:  exported,
				Line:      line,
				Doc:       extractDocComment(lines, line),
			}, true
		}
	}
	return DocItem{}, false
}

func formatFnSig(name string, fn *ast.FunctionLiteral) string {
	var parts []string
	for i, p := range fn.Parameters {
		s := p.Value
		if fn.HasRest && i == len(fn.Parameters)-1 {
			s = "..." + s
		}
		if i < len(fn.ParamTypes) && fn.ParamTypes[i] != "" {
			s += ": " + fn.ParamTypes[i]
		}
		if i < len(fn.Defaults) && fn.Defaults[i] != nil {
			s += " = …"
		}
		parts = append(parts, s)
	}
	prefix := "слово"
	if fn.IsStatic {
		prefix = "статичная слово"
	}
	sig := fmt.Sprintf("%s %s(%s)", prefix, name, strings.Join(parts, ", "))
	if fn.ReturnType != "" {
		sig += " -> " + fn.ReturnType
	}
	return sig
}
