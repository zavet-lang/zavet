package interpreter

import (
	"fmt"

	"zavet/ast"
	"zavet/lexer"
)

// === Срезы ===

// evalSliceExpression — срез массива или строки: x[start:end].
// Любая граница может отсутствовать (открытый срез x[:n], x[n:]).
// Отрицательные индексы считаются с конца, как в Python.
func evalSliceExpression(tok lexer.Token, left, start, end Object) Object {
	normalize := func(length int64) (int64, int64, *Error) {
		var s, e int64
		s = 0
		e = length
		if start != nil {
			si, ok := start.(*Integer)
			if !ok {
				return 0, 0, ErrorWithHint(tok, "начало среза должно быть целым числом", "")
			}
			s = si.Value
			if s < 0 {
				s = length + s
			}
		}
		if end != nil {
			ei, ok := end.(*Integer)
			if !ok {
				return 0, 0, ErrorWithHint(tok, "конец среза должен быть целым числом", "")
			}
			e = ei.Value
			if e < 0 {
				e = length + e
			}
		}
		if s < 0 {
			s = 0
		}
		if e > length {
			e = length
		}
		if s > e {
			s = e
		}
		return s, e, nil
	}

	switch left := left.(type) {
	case *Array:
		s, e, err := normalize(int64(len(left.Elements)))
		if err != nil {
			return err
		}
		return &Array{Elements: append([]Object{}, left.Elements[s:e]...)}
	case *String:
		runes := []rune(left.Value)
		s, e, err := normalize(int64(len(runes)))
		if err != nil {
			return err
		}
		return &String{Value: string(runes[s:e])}
	default:
		return ErrorWithHint(tok,
			fmt.Sprintf("срез не сотворим для вида %s", translateType(left.Type())),
			"Срезы работают для массивов и строк.",
		)
	}
}

// === Индексирование ===

func evalIndexExpression(tok lexer.Token, left, index Object) Object {
	return evalIndexExpressionWithEnv(tok, left, index, nil)
}

func evalIndexExpressionWithEnv(tok lexer.Token, left, index Object, env *Environment) Object {
	switch {
	case left.Type() == "ARRAY" && index.Type() == "INTEGER":
		return evalArrayIndexExpression(tok, left, index)
	case left.Type() == "STRING" && index.Type() == "INTEGER":
		return evalStringIndexExpression(tok, left, index)
	case left.Type() == "HASH":
		return evalHashIndexExpressionWithEnv(tok, left, index, env)
	case left.Type() == "INSTANCE":
		return evalInstanceIndexExpression(tok, left, index)
	case index.Type() == "STRING":
		return evalMethodAccess(tok, left, index)
	default:
		return ErrorWithHint(
			tok,
			fmt.Sprintf("индексация не сотворима для вида %s", left.Type()),
			"Индексация работает для массивов, строк, объектов и экземпляров классов.",
		)
	}
}

func evalStringIndexExpression(tok lexer.Token, str, index Object) Object {
	s := str.(*String).Value
	runes := []rune(s)
	idx := int(index.(*Integer).Value)
	if idx < 0 {
		idx = len(runes) + idx
	}
	if idx < 0 || idx >= len(runes) {
		return NULL
	}
	return &String{Value: string(runes[idx])}
}

// evalMethodAccess возвращает builtin-функцию по имени метода;
// первый аргумент builtin'а — это сам объект, на котором вызывают
// метод (например, "массив".фильтр(fn) → фильтр(массив, fn)).
func evalMethodAccess(tok lexer.Token, obj, methodName Object) Object {
	name := methodName.(*String).Value
	if builtin, ok := builtins[name]; ok {
		return builtin
	}

	objType := "объект"
	if obj.Type() == "INSTANCE" {
		inst := obj.(*Instance)
		if className, ok := inst.Properties["__class__"]; ok {
			objType = fmt.Sprintf("класс '%s'", className.Inspect())
		}
	}

	return ErrorMethodNotFound(tok, name, objType)
}

func evalArrayIndexExpression(tok lexer.Token, array, index Object) Object {
	arrayObject := array.(*Array)
	idx := index.(*Integer).Value
	max := int64(len(arrayObject.Elements) - 1)

	// Отрицательные индексы (как в Python).
	if idx < 0 {
		idx = int64(len(arrayObject.Elements)) + idx
	}

	if idx < 0 || idx > max {
		return ErrorIndexOutOfRange(tok, index.(*Integer).Value, len(arrayObject.Elements))
	}

	return arrayObject.Elements[idx]
}

func evalHashIndexExpression(tok lexer.Token, hash, index Object) Object {
	return evalHashIndexExpressionWithEnv(tok, hash, index, nil)
}

func evalHashIndexExpressionWithEnv(tok lexer.Token, hash, index Object, env *Environment) Object {
	hashObject := hash.(*Hash)

	key, ok := index.(Hashable)
	if !ok {
		return ErrorWithHint(
			tok,
			fmt.Sprintf("ключ словаря должен быть пригоден для ключа, а дан вид %s", index.Type()),
			"Употребляй как ключи строки, числа или булевы.",
		)
	}

	pair, ok := hashObject.Pairs[key.HashKey()]
	if !ok {
		// 'создать' и 'инициализация' — взаимозаменяемые имена
		// конструктора. Если запросили одно, а в словаре только
		// второе, тоже отдаём.
		if s, ok := index.(*String); ok {
			var altKey Hashable
			if s.Value == "создать" {
				altKey = &String{Value: "инициализация"}
			} else if s.Value == "инициализация" {
				altKey = &String{Value: "создать"}
			}
			if altKey != nil {
				if altPair, ok := hashObject.Pairs[altKey.HashKey()]; ok {
					return altPair.Value
				}
			}
			// Статические члены образа наследуются по цепочке предков.
			if env != nil && s.Value != "" && !isInternalClassKey(s.Value) {
				if _, hasParent := hashObject.Pairs[(&String{Value: "__parent_name__"}).HashKey()]; hasParent {
					chain := classChain(hashObject, env)
					for i := 1; i < len(chain); i++ {
						if pPair, found := chain[i].Pairs[key.HashKey()]; found {
							if isStaticClassMember(chain[i], s.Value, pPair.Value) {
								return pPair.Value
							}
						}
					}
				}
			}
		}
		return NULL
	}

	return pair.Value
}

func isInternalClassKey(name string) bool {
	switch name {
	case "__name__", "__parent_name__", "__static__", "__static_const__", "__static_types__":
		return true
	}
	return false
}

// === Литералы словарей ===

func evalHashLiteral(node *ast.HashLiteral, env *Environment) Object {
	hash := NewHash()

	// Если у литерала есть KeyOrder, используем порядок исходника.
	// Иначе fallback — итерация Go-карты (для редких случаев, когда
	// HashLiteral построен программно без KeyOrder).
	keys := node.KeyOrder
	if len(keys) == 0 && len(node.Pairs) > 0 {
		keys = make([]ast.Expression, 0, len(node.Pairs))
		for k := range node.Pairs {
			keys = append(keys, k)
		}
	}

	for _, keyNode := range keys {
		valueNode, ok := node.Pairs[keyNode]
		if !ok {
			continue
		}

		key := Eval(keyNode, env)
		if isError(key) {
			return key
		}

		if _, ok := key.(Hashable); !ok {
			return newError("непригодный ключ для хеша: %s", key.Type())
		}

		value := Eval(valueNode, env)
		if isError(value) {
			return value
		}

		hash.Set(key, value)
	}

	return hash
}

// === Деструктуризация ===

// bindPattern рекурсивно связывает паттерн со значением.
// Поддерживает имя переменной, массивный паттерн [a, b, ...rest]
// и объектный паттерн {"ключ": переменная}.
func bindPattern(pattern ast.Node, val Object, env *Environment, tok lexer.Token, mutable bool) Object {
	bind := func(name string, v Object) {
		if mutable {
			env.Set(name, v)
		} else {
			env.SetImmutable(name, v)
		}
	}
	switch p := pattern.(type) {
	case *ast.Identifier:
		bind(p.Value, val)
		return nil
	case *ast.ArrayLiteral:
		if val.Type() != "ARRAY" {
			return ErrorInvalidDestructuring(tok, "массива", val.Type())
		}
		arr := val.(*Array)
		restIndex := -1
		for i, elem := range p.Elements {
			if _, ok := elem.(*ast.SpreadExpression); ok {
				restIndex = i
				break
			}
		}
		// Элементы до rest.
		for i, elem := range p.Elements {
			if i == restIndex {
				break
			}
			var subVal Object = NULL
			if i < len(arr.Elements) {
				subVal = arr.Elements[i]
			}
			if err := bindPattern(elem, subVal, env, tok, mutable); err != nil {
				return err
			}
		}
		// Rest собирает хвост в массив.
		if restIndex >= 0 {
			spread := p.Elements[restIndex].(*ast.SpreadExpression)
			ident, ok := spread.Value.(*ast.Identifier)
			if !ok {
				return ErrorWithHint(tok, "«...» (остаток) должен содержать имя переменной",
					"Употребляй: пусть [первый, ...остальные] = массив")
			}
			var restElems []Object
			if restIndex < len(arr.Elements) {
				restElems = append([]Object{}, arr.Elements[restIndex:]...)
			}
			bind(ident.Value, &Array{Elements: restElems})
		}
		return nil
	case *ast.HashLiteral:
		if val.Type() != "HASH" {
			return ErrorInvalidDestructuring(tok, "объекта", val.Type())
		}
		hash := val.(*Hash)
		for keyExpr, valueExpr := range p.Pairs {
			keyStr, ok := keyExpr.(*ast.StringLiteral)
			if !ok {
				return ErrorWithHint(tok, "ключи в паттерне должны быть строками",
					"Употребляй: пусть {\"ключ\": переменная} = объект")
			}
			hashKey := &String{Value: keyStr.Value}
			var subVal Object = NULL
			if pair, ok := hash.Pairs[hashKey.HashKey()]; ok {
				subVal = pair.Value
			}
			if err := bindPattern(valueExpr, subVal, env, tok, mutable); err != nil {
				return err
			}
		}
		return nil
	default:
		return ErrorWithHint(tok, "неверный образец разложения",
			"Употребляй имена переменных или вложенные [..]/{..}")
	}
}
