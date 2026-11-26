package interpreter

import (
	"fmt"

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
		if isStaticClassMember(chain[owner], key, val) {
			className := "Образ"
			if cn, hasName := chain[owner].Pairs[(&String{Value: "__name__"}).HashKey()]; hasName {
				className = cn.Value.Inspect()
			}
			return ErrorWithHint(tok,
				fmt.Sprintf("сын мой, «%s» — статичный член образа «%s»", key, className),
				fmt.Sprintf("Обращайся через сам образ: %s.%s", className, key))
		}
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
	val, owner, found := findInstanceMethod(inst.Chain, start, nameStr.Value)
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

func validateClassStaticTypes(tok lexer.Token, classHash *Hash) *Error {
	typesPair, ok := classHash.Pairs[(&String{Value: "__static_types__"}).HashKey()]
	if !ok {
		return nil
	}
	typesHash, ok := typesPair.Value.(*Hash)
	if !ok {
		return nil
	}
	for _, hp := range typesHash.orderedPairs() {
		ks, ok1 := hp.Key.(*String)
		ts, ok2 := hp.Value.(*String)
		if !ok1 || !ok2 {
			continue
		}
		if fPair, exists := classHash.Pairs[ks.HashKey()]; exists {
			if err := checkTypeAnnotation(tok, ks.Value, ts.Value, fPair.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkClassStaticAssignment(tok lexer.Token, o *Hash, key string, val Object, env *Environment) *Error {
	chain := []*Hash{o}
	if _, hasParent := o.Pairs[(&String{Value: "__parent_name__"}).HashKey()]; hasParent && env != nil {
		chain = classChain(o, env)
	}
	keyHash := (&String{Value: key}).HashKey()
	for _, cls := range chain {
		if hashContainsString(cls, "__static_const__", key) {
			return ErrorCannotReassignConst(tok, key)
		}
		if tp, ok := cls.Pairs[(&String{Value: "__static_types__"}).HashKey()]; ok {
			if th, ok := tp.Value.(*Hash); ok {
				if tPair, hasType := th.Pairs[keyHash]; hasType {
					if ts, ok := tPair.Value.(*String); ok {
						if err := checkTypeAnnotation(tok, key, ts.Value, val); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}
