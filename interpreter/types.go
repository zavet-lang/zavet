package interpreter

import (
	"fmt"
	"strings"

	"zavet/lexer"
)

// checkTypeAnnotation проверяет соответствие значения опциональной аннотации типа.
// Если typeName пуст или "любой" — проверка пропускается.
// Поддерживаемые типы: целое, дробное, число, строка, булево, массив, словарь, слово, ничего, любой,
// а также nullable-форма «Тип?» и имена пользовательских образов (классов).
func checkTypeAnnotation(tok lexer.Token, target string, typeName string, val Object) *Error {
	if typeName == "" || typeName == "любой" {
		return nil
	}
	nullable := strings.HasSuffix(typeName, "?")
	base := strings.TrimSuffix(typeName, "?")
	if nullable && (val == nil || val == NULL) {
		return nil
	}

	if matchesType(base, val) {
		return nil
	}
	actual := translateType(val.Type())
	if inst, ok := val.(*Instance); ok && inst.Class != nil {
		if nameObj, ok := hashGetString(inst.Class, "__name__"); ok && nameObj != "" {
			actual = nameObj
		}
	}
	return ErrorWithHint(
		tok,
		fmt.Sprintf("сын мой, «%s» ожидает вид «%s», а дано «%s»", target, typeName, actual),
		fmt.Sprintf("Приведи значение к виду «%s» или убери аннотацию типа.", typeName),
	)
}

func matchesType(base string, val Object) bool {
	if val == nil {
		return base == "ничего"
	}
	switch base {
	case "целое":
		_, ok := val.(*Integer)
		return ok
	case "дробное":
		_, ok := val.(*Float)
		return ok
	case "число":
		_, isI := val.(*Integer)
		_, isF := val.(*Float)
		return isI || isF
	case "строка":
		_, ok := val.(*String)
		return ok
	case "булево":
		_, ok := val.(*Boolean)
		return ok
	case "массив":
		_, ok := val.(*Array)
		return ok
	case "словарь", "объект":
		_, ok := val.(*Hash)
		return ok
	case "слово", "функция":
		switch val.(type) {
		case *Function, *Builtin, *BoundMethod:
			return true
		}
		return false
	case "ничего", "пусто":
		_, ok := val.(*Null)
		return ok
	case "любой":
		return true
	default:
		// Пользовательский образ (класс) — проверяем цепочку наследования
		if inst, ok := val.(*Instance); ok {
			if len(inst.Chain) > 0 {
				for _, cls := range inst.Chain {
					if name, ok := hashGetString(cls, "__name__"); ok && name == base {
						return true
					}
				}
			}
			if inst.Class != nil {
				if name, ok := hashGetString(inst.Class, "__name__"); ok && name == base {
					return true
				}
			}
		}
		return false
	}
}
