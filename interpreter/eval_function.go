package interpreter

import (
	"fmt"

	"zavet/ast"
	"zavet/lexer"
)

// evalIdentifier ищет значение по имени: сначала среди переменных,
// затем среди встроенных функций, затем среди стандартных модулей.
func evalIdentifier(node *ast.Identifier, env *Environment) Object {
	if val, ok := env.Get(node.Value); ok {
		return val
	}

	if builtin, ok := builtins[node.Value]; ok {
		return builtin
	}

	if mod, ok := stdModules[node.Value]; ok {
		return mod
	}

	return ErrorIdentifierNotFound(node.Token, node.Value)
}

// evalExpressions вычисляет список выражений, разворачивая spread (...).
func evalExpressions(exps []ast.Expression, env *Environment) []Object {
	var result []Object

	for _, e := range exps {
		if spreadExpr, ok := e.(*ast.SpreadExpression); ok {
			spreadValue := Eval(spreadExpr.Value, env)
			if isError(spreadValue) {
				return []Object{spreadValue}
			}
			if arr, ok := spreadValue.(*Array); ok {
				result = append(result, arr.Elements...)
			} else {
				return []Object{ErrorSpreadNotArray(spreadExpr.Token, spreadValue.Type())}
			}
		} else {
			evaluated := Eval(e, env)
			if isError(evaluated) {
				return []Object{evaluated}
			}
			result = append(result, evaluated)
		}
	}

	return result
}

// applyFunction применяет вызов к объекту: классу (создаёт экземпляр),
// функции (выполняет тело) или builtin'у.
func applyFunction(fn Object, args []Object, tok lexer.Token, env *Environment) Object {
	switch fn := fn.(type) {
	case *Hash:
		return instantiateClass(fn, args, tok, env, false)
	case *BoundMethod:
		return applyBoundMethod(fn, args, tok, env)

	case *Function:
		// Генератор запускается в горутине, возвращает Generator.
		if fn.IsGenerator {
			ch := make(chan Object, 1)
			done := make(chan bool, 1)
			gen := &Generator{Ch: ch, Done: done}

			extendedEnv := extendFunctionEnv(fn, args)
			if env != nil {
				extendedEnv.callDepth = env.callDepth + 1
			}
			extendedEnv.Set("__gen_ch__", &GenChannel{Ch: ch, Done: done})

			go func() {
				defer func() {
					recover() // ловим panic от закрытого канала
					close(ch)
				}()
				Eval(fn.Body, extendedEnv)
			}()

			return gen
		}

		// Защита от бесконечной рекурсии без мутации fn.Env
		// (сохраняет лексический скоуп и потокобезопасность в асинх).
		currentDepth := 0
		if env != nil {
			currentDepth = env.callDepth
		}
		if currentDepth >= MaxCallDepth {
			return ErrorWithHint(
				tok,
				fmt.Sprintf("превышена глубина призываний (%d): стан переполнен", MaxCallDepth),
				"Быть может, слово призывает само себя без конца. Вникни в условие выхода или не давай слову имени встроенного (добавить, удалить, вставить и т.д.).",
			)
		}

		extendedEnv := NewEnclosedEnvironment(fn.Env)
		extendedEnv.callDepth = currentDepth + 1

		bindParams(fn, args, extendedEnv)

		// Проверка опциональных аннотаций типов параметров
		for i, param := range fn.Parameters {
			if i < len(fn.ParamTypes) && fn.ParamTypes[i] != "" {
				if val, ok := extendedEnv.Get(param.Value); ok {
					if err := checkTypeAnnotation(tok, param.Value, fn.ParamTypes[i], val); err != nil {
						return err
					}
				}
			}
		}

		// Записываем в стек вызовов для красивого stack trace при ошибке.
		fnName := fn.Name
		if fnName == "" {
			fnName = "<лямбда>"
		}
		pushFrame(fnName, tok)
		evaluated := Eval(fn.Body, extendedEnv)
		popFrame()

		result := unwrapReturnValue(evaluated)
		if !isError(result) && fn.ReturnType != "" {
			retLabel := fnName
			if retLabel == "" {
				retLabel = "возврат"
			}
			if err := checkTypeAnnotation(tok, "возврат "+retLabel, fn.ReturnType, result); err != nil {
				return err
			}
		}
		return result
	case *Builtin:
		setCurrentCallToken(tok)
		return fn.Fn(args...)
	default:
		return ErrorNotCallable(tok, fn.Type())
	}
}

// classChain строит цепочку наследования: класс, родитель, дед, …
// Имена предков разрешаются через окружение (классы — обычные
// переменные). Циклы наследования обрываются.
func classChain(class *Hash, env *Environment) []*Hash {
	parentNameKey := (&String{Value: "__parent_name__"}).HashKey()
	chain := []*Hash{class}
	seen := map[*Hash]bool{class: true}
	cur := class
	for {
		pn, ok := cur.Pairs[parentNameKey]
		if !ok || pn.Value.Type() != "STRING" {
			break
		}
		po, found := env.Get(pn.Value.(*String).Value)
		if !found {
			break
		}
		parent, isHash := po.(*Hash)
		if !isHash || seen[parent] {
			break
		}
		seen[parent] = true
		chain = append(chain, parent)
		cur = parent
	}
	return chain
}

// methodKeyAliases: конструктор хранится как «инициализация», но
// пишется и вызывается как «создать».
func methodKeyAliases(name string) []string {
	switch name {
	case "создать":
		return []string{"инициализация", "создать"}
	case "инициализация":
		return []string{"инициализация", "создать"}
	}
	return []string{name}
}

// hashContainsString проверяет, содержит ли массив в ключе metaKey (напр. "__static__") строку name.
func hashContainsString(class *Hash, metaKey, name string) bool {
	pair, ok := class.Pairs[(&String{Value: metaKey}).HashKey()]
	if !ok {
		return false
	}
	arr, ok := pair.Value.(*Array)
	if !ok {
		return false
	}
	for _, el := range arr.Elements {
		if s, ok := el.(*String); ok && s.Value == name {
			return true
		}
	}
	return false
}

// isStaticClassMember проверяет, является ли ключ статическим членом класса.
func isStaticClassMember(class *Hash, name string, val Object) bool {
	if fn, ok := val.(*Function); ok && fn.IsStatic {
		return true
	}
	return hashContainsString(class, "__static__", name)
}

// findMethod ищет метод по цепочке начиная с индекса start.
// Возвращает значение и индекс класса, где оно найдено.
func findMethod(chain []*Hash, start int, name string) (Object, int, bool) {
	for i := start; i < len(chain); i++ {
		for _, key := range methodKeyAliases(name) {
			if pair, ok := chain[i].Pairs[(&String{Value: key}).HashKey()]; ok {
				return pair.Value, i, true
			}
		}
	}
	return nil, -1, false
}

// findInstanceMethod ищет нестатический метод экземпляра по цепочке классов.
func findInstanceMethod(chain []*Hash, start int, name string) (Object, int, bool) {
	for i := start; i < len(chain); i++ {
		for _, key := range methodKeyAliases(name) {
			if pair, ok := chain[i].Pairs[(&String{Value: key}).HashKey()]; ok {
				if isStaticClassMember(chain[i], key, pair.Value) {
					continue
				}
				return pair.Value, i, true
			}
		}
	}
	return nil, -1, false
}

// instantiateClass создаёт экземпляр класса и вызывает конструктор,
// найденный по цепочке наследования. force=true (форма «новый Класс»)
// разрешает создавать экземпляр класса без конструктора и без родителя.
func instantiateClass(class *Hash, args []Object, tok lexer.Token, env *Environment, force bool) Object {
	parentNameKey := (&String{Value: "__parent_name__"}).HashKey()
	chain := classChain(class, env)

	var parentName string
	if pn, ok := class.Pairs[parentNameKey]; ok && pn.Value.Type() == "STRING" {
		parentName = pn.Value.(*String).Value
	}
	var parentHash *Hash
	if len(chain) > 1 {
		parentHash = chain[1]
	}

	initVal, initOwner, hasInit := findInstanceMethod(chain, 0, "инициализация")
	if !force && !hasInit && parentHash == nil {
		return ErrorNotCallable(tok, class.Type())
	}

	instance := &Instance{
		Class:      class,
		Parent:     parentHash,
		ParentName: parentName,
		Chain:      chain,
		Properties: make(map[string]Object),
	}

	if hasInit {
		if initFunc, ok := initVal.(*Function); ok {
			result := applyBoundMethod(&BoundMethod{Self: instance, Fn: initFunc, Owner: initOwner}, args, tok, env)
			if isError(result) {
				return result
			}
		}
	}
	return instance
}

func applyBoundMethod(m *BoundMethod, args []Object, tok lexer.Token, callerEnv *Environment) Object {
	f := m.Fn
	currentDepth := 0
	if callerEnv != nil {
		currentDepth = callerEnv.callDepth
	}
	if currentDepth >= MaxCallDepth {
		return ErrorWithHint(
			tok,
			fmt.Sprintf("превышена глубина призываний (%d): стан переполнен", MaxCallDepth),
			"Быть может, слово призывает само себя без конца.",
		)
	}
	extendedEnv := NewEnclosedEnvironment(f.Env)
	extendedEnv.callDepth = currentDepth + 1
	extendedEnv.Set("это", m.Self)
	// «родитель» — класс, стоящий выше того, где определён метод.
	if m.Self != nil && m.Owner+1 < len(m.Self.Chain) {
		extendedEnv.Set("родитель", m.Self.Chain[m.Owner+1])
	}
	bindParams(f, args, extendedEnv)
	for i, param := range f.Parameters {
		if i < len(f.ParamTypes) && f.ParamTypes[i] != "" {
			if val, ok := extendedEnv.Get(param.Value); ok {
				if err := checkTypeAnnotation(tok, param.Value, f.ParamTypes[i], val); err != nil {
					return err
				}
			}
		}
	}
	fnName := f.Name
	if fnName == "" {
		fnName = "<метод>"
	}
	pushFrame(fnName, tok)
	evaluated := Eval(f.Body, extendedEnv)
	popFrame()
	res := unwrapReturnValue(evaluated)
	if !isError(res) && f.ReturnType != "" {
		if err := checkTypeAnnotation(tok, "возврат "+fnName, f.ReturnType, res); err != nil {
			return err
		}
	}
	return res
}

func extendFunctionEnv(fn *Function, args []Object) *Environment {
	env := NewEnclosedEnvironment(fn.Env)
	bindParams(fn, args, env)
	return env
}

// bindParams связывает аргументы с параметрами функции, учитывая
// default-значения и rest-параметр (...args).
func bindParams(fn *Function, args []Object, env *Environment) {
	if fn.HasRest {
		fixedCount := len(fn.Parameters) - 1
		for i := 0; i < fixedCount; i++ {
			if i < len(args) {
				env.Set(fn.Parameters[i].Value, args[i])
			} else if i < len(fn.Defaults) && fn.Defaults[i] != nil {
				env.Set(fn.Parameters[i].Value, Eval(fn.Defaults[i], fn.Env))
			} else {
				env.Set(fn.Parameters[i].Value, NULL)
			}
		}
		restElems := []Object{}
		if len(args) > fixedCount {
			restElems = append(restElems, args[fixedCount:]...)
		}
		env.Set(fn.Parameters[fixedCount].Value, &Array{Elements: restElems})
	} else {
		for i, param := range fn.Parameters {
			if i < len(args) {
				env.Set(param.Value, args[i])
			} else if i < len(fn.Defaults) && fn.Defaults[i] != nil {
				env.Set(param.Value, Eval(fn.Defaults[i], fn.Env))
			} else {
				env.Set(param.Value, NULL)
			}
		}
	}
}

func unwrapReturnValue(obj Object) Object {
	if returnValue, ok := obj.(*ReturnValue); ok {
		return returnValue.Value
	}
	// continue/break не должны «вытекать» из функции наружу
	// (иначе внешний доколе воспринимает их как свой continue).
	if obj != nil && (obj.Type() == "CONTINUE" || obj.Type() == "BREAK") {
		return NULL
	}
	return obj
}

// applyFunctionFromBuiltin — упрощённая версия applyFunction, которую
// вызывают встроенные функции вроде фильтр/преобразовать.
func applyFunctionFromBuiltin(fn Object, args []Object) Object {
	switch fn := fn.(type) {
	case *Function:
		extendedEnv := NewEnclosedEnvironment(fn.Env)

		for paramIdx, param := range fn.Parameters {
			if paramIdx < len(args) {
				extendedEnv.Set(param.Value, args[paramIdx])
			}
		}

		evaluated := Eval(fn.Body, extendedEnv)
		if returnValue, ok := evaluated.(*ReturnValue); ok {
			return returnValue.Value
		}
		return evaluated
	case *Builtin:
		return fn.Fn(args...)
	case *BoundMethod:
		return applyBoundMethod(fn, args, getCurrentCallToken(), fn.Fn.Env)
	default:
		return newError("не слово: %s", fn.Type())
	}
}

// === Поддержка генераторов ===

// containsYield проверяет, есть ли в теле функции 'выдать' — это
// признак, что функция должна быть генератором.
func containsYield(node ast.Node) bool {
	if node == nil {
		return false
	}
	switch n := node.(type) {
	case *ast.YieldStatement:
		return n != nil
	case *ast.BlockStatement:
		if n == nil {
			return false
		}
		for _, stmt := range n.Statements {
			if containsYield(stmt) {
				return true
			}
		}
	case *ast.ExpressionStatement:
		if n == nil {
			return false
		}
		return containsYield(n.Expression)
	case *ast.IfExpression:
		if n == nil {
			return false
		}
		if containsYield(n.Consequence) {
			return true
		}
		if n.Alternative != nil && containsYield(n.Alternative) {
			return true
		}
	case *ast.ForExpression:
		if n == nil {
			return false
		}
		return containsYield(n.Body)
	case *ast.ForInExpression:
		if n == nil {
			return false
		}
		return containsYield(n.Body)
	case *ast.WhileExpression:
		if n == nil {
			return false
		}
		return containsYield(n.Body)
	case *ast.RepeatExpression:
		if n == nil {
			return false
		}
		return containsYield(n.Body)
	case *ast.TryExpression:
		if n == nil {
			return false
		}
		if containsYield(n.Body) {
			return true
		}
		if n.CatchBody != nil && containsYield(n.CatchBody) {
			return true
		}
		if n.FinallyBody != nil && containsYield(n.FinallyBody) {
			return true
		}
	}
	return false
}

// GenChannel — обёртка канала генератора для хранения в Environment.
type GenChannel struct {
	Ch   chan Object
	Done chan bool
}

func (c *GenChannel) Type() string    { return "GEN_CHANNEL" }
func (c *GenChannel) Inspect() string { return "<канал генератора>" }

// generatorStopSignal — служебный сигнал прерывания генератора при
// закрытии итератора.
type generatorStopSignal struct{}

func (s *generatorStopSignal) Type() string    { return "GEN_STOP" }
func (s *generatorStopSignal) Inspect() string { return "<стоп генератора>" }

// evalPipeExpression выполняет 'x |> f(a, b)' как 'f(x, a, b)'.
// Если правая часть не вызов функции — выполняет 'right(x)'.
func evalPipeExpression(node *ast.PipeExpression, env *Environment) Object {
	left := Eval(node.Left, env)
	if isError(left) {
		return left
	}

	if call, ok := node.Right.(*ast.CallExpression); ok {
		fn := Eval(call.Function, env)
		if isError(fn) {
			return fn
		}
		args := []Object{left}
		for _, a := range call.Arguments {
			arg := Eval(a, env)
			if isError(arg) {
				return arg
			}
			args = append(args, arg)
		}
		return applyFunction(fn, args, node.Token, env)
	}

	fn := Eval(node.Right, env)
	if isError(fn) {
		return fn
	}
	return applyFunction(fn, []Object{left}, node.Token, env)
}

// evalDecoratedFunction разворачивает @a @b f в a(b(f)) и связывает
// результат с именем функции в текущем окружении.
func evalDecoratedFunction(node *ast.DecoratedFunctionStatement, env *Environment) Object {
	var value Object = Eval(node.Function, env)
	if isError(value) {
		return value
	}

	for i := len(node.Decorators) - 1; i >= 0; i-- {
		dec := Eval(node.Decorators[i], env)
		if isError(dec) {
			return dec
		}
		value = applyFunction(dec, []Object{value}, node.Token, env)
		if isError(value) {
			return value
		}
	}

	if node.Function.Name != nil {
		env.Set(node.Function.Name.Value, value)
	}
	return value
}
