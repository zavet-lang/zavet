package interpreter

import (
	"fmt"
	"strconv"
	"strings"
)

func init() {
	registerYAMLModule()
}

// registerYAMLModule регистрирует стандартный модуль «yaml»
// (чистый Go-парсер и сериализатор без внешних зависимостей).
func registerYAMLModule() {
	fns := map[string]func(args ...Object) Object{
		"разобрать": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("yaml.разобрать", 1, len(args))
			}
			s, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("yaml.разобрать", 1, "строка", args[0].Type())
			}
			obj, err := parseYAML(s.Value)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "скрижали YAML начертаны неверно: "+err.Error(), "Вникни в отступы и двоеточия YAML.")
			}
			return obj
		},
		"создать": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("yaml.создать", 1, len(args))
			}
			return &String{Value: strings.TrimRight(marshalYAML(args[0], 0), "\n")}
		},
	}
	stdModules["yaml"] = makeHashFromBuiltins(fns)
}

type yamlLine struct {
	indent int
	text   string
	lineNo int
}

func parseYAML(src string) (Object, error) {
	rawLines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	lines := make([]yamlLine, 0, len(rawLines))
	for i, raw := range rawLines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" || trimmed == "..." {
			continue
		}
		indent := 0
		for indent < len(raw) && raw[indent] == ' ' {
			indent++
		}
		if indent < len(raw) && raw[indent] == '\t' {
			return nil, fmt.Errorf("строка %d: табуляция в отступе YAML не дозволена", i+1)
		}
		lines = append(lines, yamlLine{
			indent: indent,
			text:   stripYAMLComment(strings.TrimSpace(raw)),
			lineNo: i + 1,
		})
	}
	if len(lines) == 0 {
		return NULL, nil
	}
	val, _, err := parseYAMLNode(lines, 0, lines[0].indent)
	return val, err
}

func stripYAMLComment(s string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if ch == '"' && !inSingle && (i == 0 || s[i-1] != '\\') {
			inDouble = !inDouble
		} else if ch == '#' && !inSingle && !inDouble {
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return s
}

func parseYAMLNode(lines []yamlLine, idx int, baseIndent int) (Object, int, error) {
	if idx >= len(lines) {
		return NULL, idx, nil
	}
	line := lines[idx]
	if strings.HasPrefix(line.text, "- ") || line.text == "-" {
		return parseYAMLSeq(lines, idx, line.indent)
	}
	if hasYAMLMappingColon(line.text) {
		return parseYAMLMap(lines, idx, line.indent)
	}
	return parseYAMLScalar(line.text), idx + 1, nil
}

func hasYAMLMappingColon(s string) bool {
	return findYAMLColon(s) >= 0
}

func findYAMLColon(s string) int {
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if ch == '"' && !inSingle && (i == 0 || s[i-1] != '\\') {
			inDouble = !inDouble
		} else if ch == ':' && !inSingle && !inDouble {
			if i+1 == len(s) || s[i+1] == ' ' {
				return i
			}
		}
	}
	return -1
}

func parseYAMLSeq(lines []yamlLine, idx int, indent int) (Object, int, error) {
	arr := &Array{Elements: []Object{}}
	for idx < len(lines) && lines[idx].indent == indent {
		t := lines[idx].text
		if t != "-" && !strings.HasPrefix(t, "- ") {
			break
		}
		rest := ""
		if len(t) > 2 {
			rest = strings.TrimSpace(t[2:])
		}
		if rest == "" {
			idx++
			if idx < len(lines) && lines[idx].indent > indent {
				val, nextIdx, err := parseYAMLNode(lines, idx, lines[idx].indent)
				if err != nil {
					return nil, nextIdx, err
				}
				arr.Elements = append(arr.Elements, val)
				idx = nextIdx
			} else {
				arr.Elements = append(arr.Elements, NULL)
			}
			continue
		}
		// Inline mapping item: "- key: val" possibly followed by indented keys
		if ci := findYAMLColon(rest); ci >= 0 {
			h := NewHash()
			k := unquoteYAML(strings.TrimSpace(rest[:ci]))
			vStr := strings.TrimSpace(rest[ci+1:])
			idx++
			if vStr == "" && idx < len(lines) && lines[idx].indent > indent+2 {
				val, nextIdx, err := parseYAMLNode(lines, idx, lines[idx].indent)
				if err != nil {
					return nil, nextIdx, err
				}
				h.Set(&String{Value: k}, val)
				idx = nextIdx
			} else if vStr == "" {
				h.Set(&String{Value: k}, NULL)
			} else {
				h.Set(&String{Value: k}, parseYAMLScalar(vStr))
			}
			// Additional keys belonging to this list-item mapping
			for idx < len(lines) && lines[idx].indent > indent {
				subIndent := lines[idx].indent
				subCI := findYAMLColon(lines[idx].text)
				if subCI < 0 {
					break
				}
				sk := unquoteYAML(strings.TrimSpace(lines[idx].text[:subCI]))
				svStr := strings.TrimSpace(lines[idx].text[subCI+1:])
				idx++
				if svStr == "" && idx < len(lines) && lines[idx].indent > subIndent {
					val, nextIdx, err := parseYAMLNode(lines, idx, lines[idx].indent)
					if err != nil {
						return nil, nextIdx, err
					}
					h.Set(&String{Value: sk}, val)
					idx = nextIdx
				} else if svStr == "" {
					h.Set(&String{Value: sk}, NULL)
				} else {
					h.Set(&String{Value: sk}, parseYAMLScalar(svStr))
				}
			}
			arr.Elements = append(arr.Elements, h)
			continue
		}
		arr.Elements = append(arr.Elements, parseYAMLScalar(rest))
		idx++
	}
	return arr, idx, nil
}

func parseYAMLMap(lines []yamlLine, idx int, indent int) (Object, int, error) {
	h := NewHash()
	for idx < len(lines) && lines[idx].indent == indent {
		t := lines[idx].text
		ci := findYAMLColon(t)
		if ci < 0 {
			break
		}
		key := unquoteYAML(strings.TrimSpace(t[:ci]))
		valStr := strings.TrimSpace(t[ci+1:])
		idx++
		if valStr == "" {
			if idx < len(lines) && lines[idx].indent > indent {
				val, nextIdx, err := parseYAMLNode(lines, idx, lines[idx].indent)
				if err != nil {
					return nil, nextIdx, err
				}
				h.Set(&String{Value: key}, val)
				idx = nextIdx
			} else {
				h.Set(&String{Value: key}, NULL)
			}
		} else {
			h.Set(&String{Value: key}, parseYAMLScalar(valStr))
		}
	}
	return h, idx, nil
}

func parseYAMLScalar(s string) Object {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" || s == "~" || s == "ничего" {
		return NULL
	}
	if s == "true" || s == "истина" {
		return TRUE
	}
	if s == "false" || s == "ложь" {
		return FALSE
	}
	// Inline array: [a, b, c]
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		arr := &Array{Elements: []Object{}}
		if inner != "" {
			for _, part := range splitYAMLInline(inner) {
				arr.Elements = append(arr.Elements, parseYAMLScalar(part))
			}
		}
		return arr
	}
	// Inline mapping: {a: 1, b: 2}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		h := NewHash()
		if inner != "" {
			for _, part := range splitYAMLInline(inner) {
				if ci := findYAMLColon(part); ci >= 0 {
					k := unquoteYAML(strings.TrimSpace(part[:ci]))
					v := parseYAMLScalar(strings.TrimSpace(part[ci+1:]))
					h.Set(&String{Value: k}, v)
				}
			}
		}
		return h
	}
	if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		return &String{Value: unquoteYAML(s)}
	}
	if iv, err := strconv.ParseInt(s, 10, 64); err == nil {
		return NewInteger(iv)
	}
	if fv, err := strconv.ParseFloat(s, 64); err == nil {
		return &Float{Value: fv}
	}
	return &String{Value: s}
}

func splitYAMLInline(s string) []string {
	var parts []string
	depth := 0
	inSingle, inDouble := false, false
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if ch == '"' && !inSingle && (i == 0 || s[i-1] != '\\') {
			inDouble = !inDouble
		} else if !inSingle && !inDouble {
			switch ch {
			case '[', '{':
				depth++
			case ']', '}':
				depth--
			case ',':
				if depth == 0 {
					parts = append(parts, strings.TrimSpace(s[start:i]))
					start = i + 1
				}
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

func unquoteYAML(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}

func marshalYAML(obj Object, indent int) string {
	pad := strings.Repeat("  ", indent)
	switch v := obj.(type) {
	case *Null:
		return "null"
	case *Boolean:
		if v.Value {
			return "true"
		}
		return "false"
	case *Integer:
		return strconv.FormatInt(v.Value, 10)
	case *Float:
		return strconv.FormatFloat(v.Value, 'g', -1, 64)
	case *String:
		return formatYAMLString(v.Value)
	case *Array:
		if len(v.Elements) == 0 {
			return "[]"
		}
		var sb strings.Builder
		for _, el := range v.Elements {
			switch el.(type) {
			case *Array, *Hash:
				sb.WriteString(pad + "-\n")
				sub := marshalYAML(el, indent+1)
				if !strings.HasSuffix(sub, "\n") {
					sb.WriteString(strings.Repeat("  ", indent+1) + sub + "\n")
				} else {
					sb.WriteString(sub)
				}
			default:
				sb.WriteString(pad + "- " + marshalYAML(el, 0) + "\n")
			}
		}
		return sb.String()
	case *Hash:
		pairs := v.orderedPairs()
		if len(pairs) == 0 {
			return "{}"
		}
		var sb strings.Builder
		for _, p := range pairs {
			k := p.Key.Inspect()
			switch val := p.Value.(type) {
			case *Array:
				if len(val.Elements) == 0 {
					sb.WriteString(pad + k + ": []\n")
				} else {
					sb.WriteString(pad + k + ":\n" + marshalYAML(val, indent+1))
				}
			case *Hash:
				if len(val.Pairs) == 0 {
					sb.WriteString(pad + k + ": {}\n")
				} else {
					sb.WriteString(pad + k + ":\n" + marshalYAML(val, indent+1))
				}
			default:
				sb.WriteString(pad + k + ": " + marshalYAML(val, 0) + "\n")
			}
		}
		return sb.String()
	default:
		return formatYAMLString(obj.Inspect())
	}
}

func formatYAMLString(s string) string {
	if s == "" || s == "true" || s == "false" || s == "null" || s == "~" ||
		strings.ContainsAny(s, ":\n\r#[]{}'\"&*!|>%@`") ||
		strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return strconv.Quote(s)
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return strconv.Quote(s)
	}
	return s
}
