package linter

import (
	"fmt"
	"strings"

	"zavet/ast"
)

// typeEnv хранит явные аннотации типов (annotated), выведенные типы (inferred)
// и сигнатуры функций.
type typeEnv struct {
	annotated map[string]string
	inferred  map[string]string
	funcs     map[string]funcSig
	outer     *typeEnv
}

type funcSig struct {
	paramTypes []string
	returnType string
}

func newTypeEnv(outer *typeEnv) *typeEnv {
	return &typeEnv{
		annotated: make(map[string]string),
		inferred:  make(map[string]string),
		funcs:     make(map[string]funcSig),
		outer:     outer,
	}
}

func (e *typeEnv) lookupVar(name string) (string, bool) {
	if t, ok := e.annotated[name]; ok {
		return t, true
	}
	if t, ok := e.inferred[name]; ok {
		return t, true
	}
	if e.outer != nil {
		return e.outer.lookupVar(name)
	}
	return "", false
}

func (e *typeEnv) lookupAnnotated(name string) (string, bool) {
	if t, ok := e.annotated[name]; ok {
		return t, true
	}
	if _, ok := e.inferred[name]; ok {
		// Локальная динамическая переменная затеняет внешнюю аннотированную.
		return "", false
	}
	if e.outer != nil {
		return e.outer.lookupAnnotated(name)
	}
	return "", false
}

func (e *typeEnv) updateInferred(name, typ string) {
	if _, ok := e.inferred[name]; ok {
		if typ == "" {
			delete(e.inferred, name)
		} else {
			e.inferred[name] = typ
		}
		return
	}
	if e.outer != nil {
		e.outer.updateInferred(name, typ)
	}
}

func (e *typeEnv) lookupFunc(name string) (funcSig, bool) {
	if f, ok := e.funcs[name]; ok {
		return f, true
	}
	if e.outer != nil {
		return e.outer.lookupFunc(name)
	}
	return funcSig{}, false
}

// checkStaticTypes выполняет статическую проверку явных аннотаций типов.
// Если тип выражения не выводится статически, предупреждение не выдаётся.
func (ctx *lintContext) checkStaticTypes(program *ast.Program) []Issue {
	var issues []Issue
	env := newTypeEnv(nil)

	// Первый проход: регистрируем сигнатуры функций верхнего уровня
	for _, stmt := range program.Statements {
		ctx.registerFuncSig(stmt, env)
	}

	// Второй проход: проверяем операторы
	for _, stmt := range program.Statements {
		ctx.typeCheckStmt(stmt, env, "", &issues)
	}
	return issues
}

func (ctx *lintContext) registerFuncSig(stmt ast.Statement, env *typeEnv) {
	es, ok := stmt.(*ast.ExpressionStatement)
	if !ok {
		return
	}
	fl, ok := es.Expression.(*ast.FunctionLiteral)
	if !ok || fl.Name == nil {
		return
	}
	env.funcs[fl.Name.Value] = funcSig{
		paramTypes: fl.ParamTypes,
		returnType: fl.ReturnType,
	}
}

func (ctx *lintContext) typeCheckStmt(stmt ast.Statement, env *typeEnv, expectedRet string, issues *[]Issue) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.LetStatement:
		inferred := ctx.inferExprType(s.Value, env, issues)
		if s.TypeName != "" {
			if inferred != "" && !typeCompatible(s.TypeName, inferred) {
				*issues = append(*issues, Issue{
					File:     ctx.filename,
					Line:     s.Token.Line,
					Severity: Error,
					Code:     "type-mismatch",
					Message:  fmt.Sprintf("несоответствие типа для «%s»: ожидается %s, получено %s", s.Name.Value, s.TypeName, inferred),
				})
			}
			env.annotated[s.Name.Value] = s.TypeName
			delete(env.inferred, s.Name.Value)
		} else if inferred != "" {
			env.inferred[s.Name.Value] = inferred
			delete(env.annotated, s.Name.Value)
		}

	case *ast.VarStatement:
		inferred := ctx.inferExprType(s.Value, env, issues)
		if s.TypeName != "" {
			if inferred != "" && !typeCompatible(s.TypeName, inferred) {
				*issues = append(*issues, Issue{
					File:     ctx.filename,
					Line:     s.Token.Line,
					Severity: Error,
					Code:     "type-mismatch",
					Message:  fmt.Sprintf("несоответствие типа для «%s»: ожидается %s, получено %s", s.Name.Value, s.TypeName, inferred),
				})
			}
			env.annotated[s.Name.Value] = s.TypeName
			delete(env.inferred, s.Name.Value)
		} else {
			delete(env.annotated, s.Name.Value)
			if inferred != "" {
				env.inferred[s.Name.Value] = inferred
			} else {
				delete(env.inferred, s.Name.Value)
			}
		}

	case *ast.AssignmentStatement:
		inferred := ctx.inferExprType(s.Value, env, issues)
		if id, ok := s.Left.(*ast.Identifier); ok && s.Operator == "=" {
			if declared, exists := env.lookupAnnotated(id.Value); exists {
				if inferred != "" && !typeCompatible(declared, inferred) {
					*issues = append(*issues, Issue{
						File:     ctx.filename,
						Line:     s.Token.Line,
						Severity: Error,
						Code:     "type-mismatch",
						Message:  fmt.Sprintf("присваивание несовместимого типа переменной «%s»: ожидается %s, получено %s", id.Value, declared, inferred),
					})
				}
			} else {
				// Динамическая переменная без аннотации типа: просто обновляем выведенный тип.
				env.updateInferred(id.Value, inferred)
			}
		}

	case *ast.ReturnStatement:
		inferred := ctx.inferExprType(s.ReturnValue, env, issues)
		if expectedRet != "" && inferred != "" && !typeCompatible(expectedRet, inferred) {
			*issues = append(*issues, Issue{
				File:     ctx.filename,
				Line:     s.Token.Line,
				Severity: Error,
				Code:     "type-mismatch",
				Message:  fmt.Sprintf("возвращаемое значение не соответствует типу функции: ожидается %s, получено %s", expectedRet, inferred),
			})
		}

	case *ast.BlockStatement:
		child := newTypeEnv(env)
		for _, sub := range s.Statements {
			ctx.registerFuncSig(sub, child)
		}
		for _, sub := range s.Statements {
			ctx.typeCheckStmt(sub, child, expectedRet, issues)
		}

	case *ast.ExpressionStatement:
		ctx.typeCheckExpr(s.Expression, env, expectedRet, issues)

	case *ast.ExportStatement:
		ctx.typeCheckStmt(s.Statement, env, expectedRet, issues)
	}
}

func (ctx *lintContext) typeCheckExpr(expr ast.Expression, env *typeEnv, expectedRet string, issues *[]Issue) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.FunctionLiteral:
		fnEnv := newTypeEnv(env)
		for i, p := range e.Parameters {
			if i < len(e.ParamTypes) && e.ParamTypes[i] != "" {
				fnEnv.annotated[p.Value] = e.ParamTypes[i]
			}
		}
		if e.Body != nil {
			for _, sub := range e.Body.Statements {
				ctx.registerFuncSig(sub, fnEnv)
			}
			for _, sub := range e.Body.Statements {
				ctx.typeCheckStmt(sub, fnEnv, e.ReturnType, issues)
			}
		}
	case *ast.IfExpression:
		ctx.inferExprType(e.Condition, env, issues)
		if e.Consequence != nil {
			ctx.typeCheckStmt(e.Consequence, env, expectedRet, issues)
		}
		if e.Alternative != nil {
			ctx.typeCheckStmt(e.Alternative, env, expectedRet, issues)
		}
	case *ast.WhileExpression:
		ctx.inferExprType(e.Condition, env, issues)
		if e.Body != nil {
			ctx.typeCheckStmt(e.Body, env, expectedRet, issues)
		}
	case *ast.ForExpression:
		if e.Body != nil {
			loopEnv := newTypeEnv(env)
			if e.Variable != nil {
				loopEnv.annotated[e.Variable.Value] = "целое"
			}
			ctx.typeCheckStmt(e.Body, loopEnv, expectedRet, issues)
		}
	case *ast.ForInExpression:
		if e.Body != nil {
			ctx.typeCheckStmt(e.Body, env, expectedRet, issues)
		}
	case *ast.RepeatExpression:
		cntType := ctx.inferExprType(e.Count, env, issues)
		if cntType != "" && !typeCompatible("целое", cntType) {
			*issues = append(*issues, Issue{
				File:     ctx.filename,
				Line:     e.Token.Line,
				Severity: Error,
				Code:     "type-mismatch",
				Message:  fmt.Sprintf("«повтори … раз» ожидает целое, получено %s", cntType),
			})
		}
		if e.Body != nil {
			ctx.typeCheckStmt(e.Body, env, expectedRet, issues)
		}
	case *ast.TryExpression:
		if e.Body != nil {
			ctx.typeCheckStmt(e.Body, env, expectedRet, issues)
		}
		if e.CatchBody != nil {
			ctx.typeCheckStmt(e.CatchBody, env, expectedRet, issues)
		}
		if e.FinallyBody != nil {
			ctx.typeCheckStmt(e.FinallyBody, env, expectedRet, issues)
		}
	case *ast.CallExpression:
		ctx.checkCallTypes(e, env, issues)
	default:
		ctx.inferExprType(expr, env, issues)
	}
}

func (ctx *lintContext) checkCallTypes(ce *ast.CallExpression, env *typeEnv, issues *[]Issue) {
	var sig funcSig
	hasSig := false
	fnName := ce.Function.TokenLiteral()
	if id, ok := ce.Function.(*ast.Identifier); ok {
		fnName = id.Value
		sig, hasSig = env.lookupFunc(id.Value)
	}
	for i, arg := range ce.Arguments {
		argType := ctx.inferExprType(arg, env, issues)
		if hasSig && i < len(sig.paramTypes) && sig.paramTypes[i] != "" && argType != "" {
			if !typeCompatible(sig.paramTypes[i], argType) {
				*issues = append(*issues, Issue{
					File:     ctx.filename,
					Line:     ce.Token.Line,
					Severity: Error,
					Code:     "type-mismatch",
					Message:  fmt.Sprintf("аргумент %d вызова «%s»: ожидается %s, получено %s", i+1, fnName, sig.paramTypes[i], argType),
				})
			}
		}
	}
}

func (ctx *lintContext) inferExprType(expr ast.Expression, env *typeEnv, issues *[]Issue) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return "целое"
	case *ast.FloatLiteral:
		return "дробное"
	case *ast.StringLiteral:
		return "строка"
	case *ast.Boolean:
		return "булево"
	case *ast.NullLiteral:
		return "ничего"
	case *ast.ArrayLiteral:
		for _, el := range e.Elements {
			ctx.inferExprType(el, env, issues)
		}
		return "массив"
	case *ast.HashLiteral:
		for k, v := range e.Pairs {
			ctx.inferExprType(k, env, issues)
			ctx.inferExprType(v, env, issues)
		}
		return "словарь"
	case *ast.FunctionLiteral:
		ctx.typeCheckExpr(e, env, "", issues)
		return "функция"
	case *ast.Identifier:
		if t, ok := env.lookupVar(e.Value); ok {
			return t
		}
		return ""
	case *ast.PrefixExpression:
		rt := ctx.inferExprType(e.Right, env, issues)
		switch e.Operator {
		case "не", "!":
			return "булево"
		case "-":
			if rt == "целое" || rt == "дробное" || rt == "число" {
				return rt
			}
		}
		return ""
	case *ast.InfixExpression:
		lt := ctx.inferExprType(e.Left, env, issues)
		rt := ctx.inferExprType(e.Right, env, issues)
		switch e.Operator {
		case "==", "!=", "<", "<=", ">", ">=", "и", "или":
			return "булево"
		case "+":
			if lt == "строка" || rt == "строка" {
				return "строка"
			}
			if lt == "целое" && rt == "целое" {
				return "целое"
			}
			if (lt == "целое" || lt == "дробное") && (rt == "целое" || rt == "дробное") {
				return "дробное"
			}
		case "-", "*", "%":
			if lt == "целое" && rt == "целое" {
				return "целое"
			}
			if (lt == "целое" || lt == "дробное") && (rt == "целое" || rt == "дробное") {
				return "дробное"
			}
		case "//":
			if (lt == "целое" || lt == "дробное") && (rt == "целое" || rt == "дробное") {
				return "целое"
			}
		}
		return ""
	case *ast.CallExpression:
		ctx.checkCallTypes(e, env, issues)
		if id, ok := e.Function.(*ast.Identifier); ok {
			if sig, exists := env.lookupFunc(id.Value); exists && sig.returnType != "" {
				return sig.returnType
			}
		}
		return ""
	default:
		return ""
	}
}

// typeCompatible проверяет, совместим ли выведенный тип actual с ожидаемой аннотацией expected.
func typeCompatible(expected, actual string) bool {
	if expected == "" || actual == "" || expected == "любой" || actual == "любой" {
		return true
	}
	if expected == actual {
		return true
	}
	// Опциональный тип Тип? допускает ничего и базовый тип
	if strings.HasSuffix(expected, "?") {
		if actual == "ничего" {
			return true
		}
		base := strings.TrimSuffix(expected, "?")
		return typeCompatible(base, actual)
	}
	// Не проверяем пользовательские имена образов против выведенных типов (кроме явных примитивов)
	if !isBuiltinTypeName(expected) {
		// Пользовательский образ: не совместим с примитивами
		if isBuiltinTypeName(actual) && actual != "словарь" {
			return false
		}
		return true
	}
	if expected == "число" && (actual == "целое" || actual == "дробное") {
		return true
	}
	if expected == "дробное" && actual == "целое" {
		return true
	}
	return false
}

func isBuiltinTypeName(t string) bool {
	switch t {
	case "целое", "дробное", "число", "строка", "булево", "массив", "словарь", "функция", "ничего", "любой":
		return true
	}
	return false
}
