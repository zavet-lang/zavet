package interpreter

import (
	"zavet/ast"
	"zavet/lexer"
)

// evalNewExpression создаёт экземпляр класса: новый Класс(args).
// В Завете вызов класса как функции (Класс(args)) тоже создаёт
// экземпляр — оба пути идут через instantiateClass, поэтому
// наследование и конструкторы предков работают одинаково.
func evalNewExpression(node *ast.NewExpression, env *Environment) Object {
	classObj, ok := env.Get(node.ClassName.Value)
	if !ok {
		return newError("образ не найден: %s", node.ClassName.Value)
	}

	class, isHash := classObj.(*Hash)
	if !isHash {
		return newError("%s не есть образ", node.ClassName.Value)
	}

	args := evalExpressions(node.Arguments, env)
	if len(args) == 1 && isError(args[0]) {
		return args[0]
	}

	return instantiateClass(class, args, node.Token, env, true)
}

// evalInstanceIndexExpression — доступ к свойству или методу
// экземпляра. Сначала ищется в собственных свойствах, потом в методах
// класса, затем по цепочке предков (родитель, дед, …).
func evalInstanceIndexExpression(tok lexer.Token, instance, index Object) Object {
	inst := instance.(*Instance)

	if index.Type() != "STRING" {
		return ErrorWithHint(
			tok,
			"индекс образа должен быть строкой",
			"Обращайся к свойствам строкой: объект.свойство или объект[\"свойство\"]",
		)
	}

	key := index.(*String).Value

	if val, ok := inst.Properties[key]; ok {
		return val
	}

	// Экземпляры, созданные без цепочки (старый путь), ищут только
	// в классе и в прямом родителе.
	chain := inst.Chain
	if len(chain) == 0 {
		chain = []*Hash{inst.Class}
		if inst.Parent != nil {
			chain = append(chain, inst.Parent)
		}
	}

	if val, owner, ok := findMethod(chain, 0, key); ok {
		return bindInstanceMethod(inst, val, owner)
	}

	return NULL
}

func bindInstanceMethod(inst *Instance, val Object, owner int) Object {
	if fn, ok := val.(*Function); ok {
		return &BoundMethod{Self: inst, Fn: fn, Owner: owner}
	}
	return val
}

// callParentMethod выполняет «родитель.метод(...)» внутри метода:
// метод ищется у предков, «это» остаётся текущим экземпляром.
// Возвращает nil, если вызов не распознан (тогда работает обычный путь).
func callParentMethod(indexExpr *ast.IndexExpression, parent *Hash, node *ast.CallExpression, env *Environment) Object {
	selfObj, ok := env.Get("это")
	if !ok {
		return nil
	}
	inst, ok := selfObj.(*Instance)
	if !ok {
		return nil
	}
	nameObj := Eval(indexExpr.Index, env)
	nameStr, ok := nameObj.(*String)
	if !ok {
		return nil
	}
	start := -1
	for i, c := range inst.Chain {
		if c == parent {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	val, owner, found := findMethod(inst.Chain, start, nameStr.Value)
	if !found {
		return nil
	}
	fn, isFn := val.(*Function)
	if !isFn {
		return nil
	}
	args := evalExpressions(node.Arguments, env)
	if len(args) == 1 && isError(args[0]) {
		return args[0]
	}
	return applyBoundMethod(&BoundMethod{Self: inst, Fn: fn, Owner: owner}, args, node.Token)
}
