package vm

import (
	"fmt"

	"zavet/ast"
)

// CompileError — узел не поддержан VM (нужен tree-walker).
type CompileError struct {
	Msg string
}

func (e *CompileError) Error() string { return e.Msg }

type loopContext struct {
	continueTarget int
	breakJumps     []int
	continueJumps  []int
}

type compiler struct {
	chunk           *Chunk
	isFunc          bool
	locals          map[string]int
	immutableLocals map[string]bool
	numLocal        int
	loops           []*loopContext
	globals         *Chunk // корневой chunk для таблицы имён глобальных
}

func newCompiler(globals *Chunk, isFunc bool) *compiler {
	c := &Chunk{}
	if globals == nil {
		globals = c
	}
	return &compiler{
		chunk:           c,
		isFunc:          isFunc,
		locals:          make(map[string]int),
		immutableLocals: make(map[string]bool),
		globals:         globals,
	}
}

func (cp *compiler) defineVar(name string) (int, bool) {
	if !cp.isFunc {
		return cp.globals.nameIndex(name), false
	}
	if slot, ok := cp.locals[name]; ok {
		return slot, true
	}
	slot := cp.numLocal
	cp.locals[name] = slot
	cp.numLocal++
	return slot, true
}

func (cp *compiler) resolveVar(name string) (int, bool) {
	if cp.isFunc {
		if slot, ok := cp.locals[name]; ok {
			return slot, true
		}
	}
	return cp.globals.nameIndex(name), false
}

// Compile компилирует программу в байткод.
// Поддержка: числа/строки/булевы/ничего/массивы, арифметика (+ - * / // %),
// сравнения, логика (и, или, не, !), пусть/измени/=/+=/−=/*=//=,
// если, доколе, для i от a до b [по s], повтори N раз, прерви, продолжи,
// слова (функции, рекурсия, аминь), индексация массивов, возгласи, длина.
func Compile(program *ast.Program) (*Chunk, error) {
	cp := newCompiler(nil, false)
	for _, stmt := range program.Statements {
		if err := cp.compileStmt(stmt); err != nil {
			return nil, err
		}
	}
	cp.chunk.emit(OpNull, 0)
	cp.chunk.emit(OpReturn, 0)
	return cp.chunk, nil
}

func (cp *compiler) compileStmt(stmt ast.Statement) error {
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		if s.Expression == nil {
			return nil
		}
		// Именованное слово верхнего уровня: слово f(x) ... конец
		if fn, ok := s.Expression.(*ast.FunctionLiteral); ok && fn.Name != nil {
			return cp.compileNamedFunc(fn.Name.Value, fn)
		}
		if err := cp.compileExpr(s.Expression); err != nil {
			return err
		}
		cp.chunk.emit(OpPop, 0)
		return nil

	case *ast.LetStatement:
		if s.TypeName != "" {
			return &CompileError{Msg: "VM: аннотации типов через tree-walker"}
		}
		if fn, ok := s.Value.(*ast.FunctionLiteral); ok {
			return cp.compileNamedFunc(s.Name.Value, fn)
		}
		if err := cp.compileExpr(s.Value); err != nil {
			return err
		}
		slot, isLocal := cp.defineVar(s.Name.Value)
		if isLocal {
			cp.immutableLocals[s.Name.Value] = true
			cp.chunk.emit(OpSetLocal, slot)
		} else {
			cp.chunk.emit(OpDefineGlobalConst, slot)
		}
		return nil

	case *ast.VarStatement:
		if s.TypeName != "" {
			return &CompileError{Msg: "VM: аннотации типов через tree-walker"}
		}
		if err := cp.compileExpr(s.Value); err != nil {
			return err
		}
		slot, isLocal := cp.defineVar(s.Name.Value)
		if isLocal {
			delete(cp.immutableLocals, s.Name.Value)
			cp.chunk.emit(OpSetLocal, slot)
		} else {
			cp.chunk.emit(OpDefineGlobal, slot)
		}
		return nil

	case *ast.AssignmentStatement:
		switch left := s.Left.(type) {
		case *ast.Identifier:
			slot, isLocal := cp.resolveVar(left.Value)
			if isLocal && cp.immutableLocals[left.Value] {
				msg := fmt.Sprintf("сын мой, «%s» сотворено словом «пусть» и не меняется", left.Value)
				cp.chunk.emit(OpError, cp.chunk.addConstant(msg))
				return nil
			}
			if s.Operator == "=" {
				if err := cp.compileExpr(s.Value); err != nil {
					return err
				}
			} else {
				if isLocal {
					cp.chunk.emit(OpGetLocal, slot)
				} else {
					cp.chunk.emit(OpGetGlobal, slot)
				}
				if err := cp.compileExpr(s.Value); err != nil {
					return err
				}
				switch s.Operator {
				case "+=":
					cp.chunk.emit(OpAdd, 0)
				case "-=":
					cp.chunk.emit(OpSub, 0)
				case "*=":
					cp.chunk.emit(OpMul, 0)
				case "/=":
					cp.chunk.emit(OpDiv, 0)
				default:
					return &CompileError{Msg: "VM: составное присваивание " + s.Operator}
				}
			}
			if isLocal {
				cp.chunk.emit(OpSetLocal, slot)
			} else {
				cp.chunk.emit(OpSetGlobal, slot)
			}
			return nil

		case *ast.IndexExpression:
			if s.Operator != "=" {
				return &CompileError{Msg: "VM: составное присваивание по индексу"}
			}
			if err := cp.compileExpr(left.Left); err != nil {
				return err
			}
			if err := cp.compileExpr(left.Index); err != nil {
				return err
			}
			if err := cp.compileExpr(s.Value); err != nil {
				return err
			}
			cp.chunk.emit(OpSetIndex, 0)
			cp.chunk.emit(OpPop, 0)
			return nil

		default:
			return &CompileError{Msg: "VM: присваивание только идентификатору или индексу"}
		}

	case *ast.ReturnStatement:
		if s.ReturnValue != nil {
			if err := cp.compileExpr(s.ReturnValue); err != nil {
				return err
			}
		} else {
			cp.chunk.emit(OpNull, 0)
		}
		cp.chunk.emit(OpReturn, 0)
		return nil

	case *ast.BreakStatement:
		if len(cp.loops) == 0 {
			return &CompileError{Msg: "VM: прерви вне цикла"}
		}
		lp := cp.loops[len(cp.loops)-1]
		lp.breakJumps = append(lp.breakJumps, cp.chunk.emit(OpJump, 0))
		return nil

	case *ast.ContinueStatement:
		if len(cp.loops) == 0 {
			return &CompileError{Msg: "VM: продолжи вне цикла"}
		}
		lp := cp.loops[len(cp.loops)-1]
		if lp.continueTarget >= 0 {
			cp.chunk.emit(OpJump, lp.continueTarget)
		} else {
			lp.continueJumps = append(lp.continueJumps, cp.chunk.emit(OpJump, 0))
		}
		return nil

	case *ast.BlockStatement:
		for _, st := range s.Statements {
			if err := cp.compileStmt(st); err != nil {
				return err
			}
		}
		return nil

	default:
		return &CompileError{Msg: fmt.Sprintf("VM: не поддержано %T", stmt)}
	}
}

func (cp *compiler) compileNamedFunc(name string, fn *ast.FunctionLiteral) error {
	if fn.HasRest || fn.ReturnType != "" {
		return &CompileError{Msg: "VM: rest/типы в функции через tree-walker"}
	}
	for _, d := range fn.Defaults {
		if d != nil {
			return &CompileError{Msg: "VM: параметры по умолчанию через tree-walker"}
		}
	}
	for _, t := range fn.ParamTypes {
		if t != "" {
			return &CompileError{Msg: "VM: аннотации типов через tree-walker"}
		}
	}

	sub := newCompiler(cp.globals, true)
	for _, p := range fn.Parameters {
		sub.defineVar(p.Value)
	}
	if fn.Body != nil {
		for _, st := range fn.Body.Statements {
			if err := sub.compileStmt(st); err != nil {
				return err
			}
		}
	}
	sub.chunk.emit(OpNull, 0)
	sub.chunk.emit(OpReturn, 0)

	cf := &CompiledFunc{
		Name:      name,
		NumParams: len(fn.Parameters),
		NumLocals: sub.numLocal,
		Chunk:     sub.chunk,
	}
	cp.chunk.emit(OpConstant, cp.chunk.addConstant(cf))
	slot, isLocal := cp.defineVar(name)
	if isLocal {
		cp.chunk.emit(OpSetLocal, slot)
	} else {
		cp.chunk.emit(OpDefineGlobal, slot)
	}
	return nil
}

func (cp *compiler) compileExpr(expr ast.Expression) error {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		cp.chunk.emit(OpConstant, cp.chunk.addConstant(e.Value))
		return nil
	case *ast.FloatLiteral:
		cp.chunk.emit(OpConstant, cp.chunk.addConstant(e.Value))
		return nil
	case *ast.StringLiteral:
		if len(e.Value) > 0 && e.Value[0] == '\x00' {
			return &CompileError{Msg: "VM: интерполяция строки через tree-walker"}
		}
		cp.chunk.emit(OpConstant, cp.chunk.addConstant(e.Value))
		return nil
	case *ast.NullLiteral:
		cp.chunk.emit(OpNull, 0)
		return nil
	case *ast.Boolean:
		if e.Value {
			cp.chunk.emit(OpTrue, 0)
		} else {
			cp.chunk.emit(OpFalse, 0)
		}
		return nil
	case *ast.Identifier:
		slot, isLocal := cp.resolveVar(e.Value)
		if isLocal {
			cp.chunk.emit(OpGetLocal, slot)
		} else {
			cp.chunk.emit(OpGetGlobal, slot)
		}
		return nil
	case *ast.ArrayLiteral:
		for _, el := range e.Elements {
			if _, isSpread := el.(*ast.SpreadExpression); isSpread {
				return &CompileError{Msg: "VM: spread в массиве через tree-walker"}
			}
			if err := cp.compileExpr(el); err != nil {
				return err
			}
		}
		cp.chunk.emit(OpBuildArray, len(e.Elements))
		return nil
	case *ast.HashLiteral:
		// Определения образов (классов) содержат служебный ключ __class__ — оставляем tree-walker.
		keys := e.KeyOrder
		if len(keys) == 0 && len(e.Pairs) > 0 {
			for k := range e.Pairs {
				keys = append(keys, k)
			}
		}
		for _, k := range keys {
			if sl, ok := k.(*ast.StringLiteral); ok && sl.Value == "__class__" {
				return &CompileError{Msg: "VM: образ (класс) через tree-walker"}
			}
		}
		for _, k := range keys {
			if err := cp.compileExpr(k); err != nil {
				return err
			}
			if err := cp.compileExpr(e.Pairs[k]); err != nil {
				return err
			}
		}
		cp.chunk.emit(OpBuildHash, len(keys))
		return nil
	case *ast.IndexExpression:
		if err := cp.compileExpr(e.Left); err != nil {
			return err
		}
		if err := cp.compileExpr(e.Index); err != nil {
			return err
		}
		cp.chunk.emit(OpIndex, 0)
		return nil
	case *ast.PrefixExpression:
		if err := cp.compileExpr(e.Right); err != nil {
			return err
		}
		switch e.Operator {
		case "-":
			cp.chunk.emit(OpNeg, 0)
		case "!", "не":
			cp.chunk.emit(OpNot, 0)
		default:
			return &CompileError{Msg: "VM: префикс " + e.Operator}
		}
		return nil
	case *ast.InfixExpression:
		// Короткое замыкание для «и» / «или» (результат — bool, как в AST)
		if e.Operator == "и" || e.Operator == "&&" {
			if err := cp.compileExpr(e.Left); err != nil {
				return err
			}
			cp.chunk.emit(OpDup, 0)
			jEnd := cp.chunk.emit(OpJumpIfFalse, 0)
			cp.chunk.emit(OpPop, 0)
			if err := cp.compileExpr(e.Right); err != nil {
				return err
			}
			cp.chunk.patch(jEnd, len(cp.chunk.Code))
			cp.chunk.emit(OpToBool, 0)
			return nil
		}
		if e.Operator == "или" || e.Operator == "||" {
			if err := cp.compileExpr(e.Left); err != nil {
				return err
			}
			cp.chunk.emit(OpDup, 0)
			jEnd := cp.chunk.emit(OpJumpIfTrue, 0)
			cp.chunk.emit(OpPop, 0)
			if err := cp.compileExpr(e.Right); err != nil {
				return err
			}
			cp.chunk.patch(jEnd, len(cp.chunk.Code))
			cp.chunk.emit(OpToBool, 0)
			return nil
		}
		if err := cp.compileExpr(e.Left); err != nil {
			return err
		}
		if err := cp.compileExpr(e.Right); err != nil {
			return err
		}
		switch e.Operator {
		case "+":
			cp.chunk.emit(OpAdd, 0)
		case "-":
			cp.chunk.emit(OpSub, 0)
		case "*":
			cp.chunk.emit(OpMul, 0)
		case "/":
			cp.chunk.emit(OpDiv, 0)
		case "//":
			cp.chunk.emit(OpIntDiv, 0)
		case "%":
			cp.chunk.emit(OpMod, 0)
		case "==":
			cp.chunk.emit(OpEqual, 0)
		case "!=":
			cp.chunk.emit(OpNotEqual, 0)
		case ">":
			cp.chunk.emit(OpGreater, 0)
		case ">=":
			cp.chunk.emit(OpGreaterEq, 0)
		case "<":
			cp.chunk.emit(OpLess, 0)
		case "<=":
			cp.chunk.emit(OpLessEq, 0)
		default:
			return &CompileError{Msg: "VM: оператор " + e.Operator}
		}
		return nil
	case *ast.IfExpression:
		return cp.compileIf(e)
	case *ast.WhileExpression:
		return cp.compileWhile(e)
	case *ast.RepeatExpression:
		return cp.compileRepeat(e)
	case *ast.ForExpression:
		return cp.compileFor(e)
	case *ast.CallExpression:
		return cp.compileCall(e)
	case *ast.GroupedExpression:
		return cp.compileExpr(e.Inner)
	default:
		return &CompileError{Msg: fmt.Sprintf("VM: выражение %T", expr)}
	}
}

func (cp *compiler) compileIf(e *ast.IfExpression) error {
	if err := cp.compileExpr(e.Condition); err != nil {
		return err
	}
	jumpFalse := cp.chunk.emit(OpJumpIfFalse, 0)
	if err := cp.compileStmt(e.Consequence); err != nil {
		return err
	}
	if e.Alternative != nil {
		jumpEnd := cp.chunk.emit(OpJump, 0)
		cp.chunk.patch(jumpFalse, len(cp.chunk.Code))
		if err := cp.compileStmt(e.Alternative); err != nil {
			return err
		}
		cp.chunk.patch(jumpEnd, len(cp.chunk.Code))
	} else {
		cp.chunk.patch(jumpFalse, len(cp.chunk.Code))
	}
	cp.chunk.emit(OpNull, 0)
	return nil
}

func (cp *compiler) compileWhile(e *ast.WhileExpression) error {
	loopStart := len(cp.chunk.Code)
	lp := &loopContext{continueTarget: loopStart}
	cp.loops = append(cp.loops, lp)

	if err := cp.compileExpr(e.Condition); err != nil {
		return err
	}
	jumpFalse := cp.chunk.emit(OpJumpIfFalse, 0)
	if err := cp.compileStmt(e.Body); err != nil {
		return err
	}
	cp.chunk.emit(OpJump, loopStart)
	endPos := len(cp.chunk.Code)
	cp.chunk.patch(jumpFalse, endPos)
	for _, bj := range lp.breakJumps {
		cp.chunk.patch(bj, endPos)
	}
	cp.loops = cp.loops[:len(cp.loops)-1]
	cp.chunk.emit(OpNull, 0)
	return nil
}

func (cp *compiler) compileRepeat(e *ast.RepeatExpression) error {
	if err := cp.compileExpr(e.Count); err != nil {
		return err
	}
	cp.chunk.emit(OpCheckInt, 0)

	remName := fmt.Sprintf("__rep_rem_%d", len(cp.chunk.Code))
	remSlot, remLocal := cp.defineVar(remName)
	if remLocal {
		cp.chunk.emit(OpSetLocal, remSlot)
	} else {
		cp.chunk.emit(OpDefineGlobal, remSlot)
	}

	loopStart := len(cp.chunk.Code)
	lp := &loopContext{continueTarget: -1}
	cp.loops = append(cp.loops, lp)

	// Условие: rem > 0
	if remLocal {
		cp.chunk.emit(OpGetLocal, remSlot)
	} else {
		cp.chunk.emit(OpGetGlobal, remSlot)
	}
	cp.chunk.emit(OpConstant, cp.chunk.addConstant(int64(0)))
	cp.chunk.emit(OpGreater, 0)
	jumpExit := cp.chunk.emit(OpJumpIfFalse, 0)

	if err := cp.compileStmt(e.Body); err != nil {
		return err
	}

	// Цель для «продолжи»: декремент счётчика
	decPos := len(cp.chunk.Code)
	for _, cj := range lp.continueJumps {
		cp.chunk.patch(cj, decPos)
	}
	if remLocal {
		cp.chunk.emit(OpGetLocal, remSlot)
	} else {
		cp.chunk.emit(OpGetGlobal, remSlot)
	}
	cp.chunk.emit(OpConstant, cp.chunk.addConstant(int64(1)))
	cp.chunk.emit(OpSub, 0)
	if remLocal {
		cp.chunk.emit(OpSetLocal, remSlot)
	} else {
		cp.chunk.emit(OpSetGlobal, remSlot)
	}
	cp.chunk.emit(OpJump, loopStart)

	endPos := len(cp.chunk.Code)
	cp.chunk.patch(jumpExit, endPos)
	for _, bj := range lp.breakJumps {
		cp.chunk.patch(bj, endPos)
	}
	cp.loops = cp.loops[:len(cp.loops)-1]
	cp.chunk.emit(OpNull, 0)
	return nil
}

func (cp *compiler) compileFor(e *ast.ForExpression) error {
	// Инициализация счётчика
	if err := cp.compileExpr(e.From); err != nil {
		return err
	}
	varSlot, isLocal := cp.defineVar(e.Variable.Value)
	if isLocal {
		cp.chunk.emit(OpSetLocal, varSlot)
	} else {
		cp.chunk.emit(OpDefineGlobal, varSlot)
	}

	// Вычисляем границу один раз в скрытую переменную
	endName := fmt.Sprintf("__for_end_%d", len(cp.chunk.Code))
	if err := cp.compileExpr(e.To); err != nil {
		return err
	}
	endSlot, endLocal := cp.defineVar(endName)
	if endLocal {
		cp.chunk.emit(OpSetLocal, endSlot)
	} else {
		cp.chunk.emit(OpDefineGlobal, endSlot)
	}

	stepName := fmt.Sprintf("__for_step_%d", len(cp.chunk.Code))
	if e.Step != nil {
		if err := cp.compileExpr(e.Step); err != nil {
			return err
		}
	} else {
		cp.chunk.emit(OpConstant, cp.chunk.addConstant(int64(1)))
	}
	stepSlot, stepLocal := cp.defineVar(stepName)
	if stepLocal {
		cp.chunk.emit(OpSetLocal, stepSlot)
	} else {
		cp.chunk.emit(OpDefineGlobal, stepSlot)
	}

	loopStart := len(cp.chunk.Code)
	lp := &loopContext{continueTarget: -1}
	cp.loops = append(cp.loops, lp)

	// Условие: i <= end (для положительного шага)
	if isLocal {
		cp.chunk.emit(OpGetLocal, varSlot)
	} else {
		cp.chunk.emit(OpGetGlobal, varSlot)
	}
	if endLocal {
		cp.chunk.emit(OpGetLocal, endSlot)
	} else {
		cp.chunk.emit(OpGetGlobal, endSlot)
	}
	cp.chunk.emit(OpLessEq, 0)
	jumpExit := cp.chunk.emit(OpJumpIfFalse, 0)

	if err := cp.compileStmt(e.Body); err != nil {
		return err
	}

	// Цель для «продолжи»: инкремент
	incPos := len(cp.chunk.Code)
	for _, cj := range lp.continueJumps {
		cp.chunk.patch(cj, incPos)
	}
	if isLocal {
		cp.chunk.emit(OpGetLocal, varSlot)
	} else {
		cp.chunk.emit(OpGetGlobal, varSlot)
	}
	if stepLocal {
		cp.chunk.emit(OpGetLocal, stepSlot)
	} else {
		cp.chunk.emit(OpGetGlobal, stepSlot)
	}
	cp.chunk.emit(OpAdd, 0)
	if isLocal {
		cp.chunk.emit(OpSetLocal, varSlot)
	} else {
		cp.chunk.emit(OpSetGlobal, varSlot)
	}
	cp.chunk.emit(OpJump, loopStart)

	endPos := len(cp.chunk.Code)
	cp.chunk.patch(jumpExit, endPos)
	for _, bj := range lp.breakJumps {
		cp.chunk.patch(bj, endPos)
	}
	cp.loops = cp.loops[:len(cp.loops)-1]
	cp.chunk.emit(OpNull, 0)
	return nil
}

func (cp *compiler) compileCall(e *ast.CallExpression) error {
	ident, ok := e.Function.(*ast.Identifier)
	if !ok {
		return &CompileError{Msg: "VM: вызов не по имени через tree-walker"}
	}
	for _, arg := range e.Arguments {
		if _, isSpread := arg.(*ast.SpreadExpression); isSpread {
			return &CompileError{Msg: "VM: spread в вызове через tree-walker"}
		}
	}
	if ident.Value == "возгласи" {
		for _, arg := range e.Arguments {
			if err := cp.compileExpr(arg); err != nil {
				return err
			}
		}
		cp.chunk.emit(OpCallPrint, len(e.Arguments))
		return nil
	}
	if ident.Value == "длина" && len(e.Arguments) == 1 {
		if err := cp.compileExpr(e.Arguments[0]); err != nil {
			return err
		}
		cp.chunk.emit(OpCallLen, 0)
		return nil
	}

	// Обычный вызов слова (функции)
	slot, isLocal := cp.resolveVar(ident.Value)
	if isLocal {
		cp.chunk.emit(OpGetLocal, slot)
	} else {
		cp.chunk.emit(OpGetGlobal, slot)
	}
	for _, arg := range e.Arguments {
		if err := cp.compileExpr(arg); err != nil {
			return err
		}
	}
	cp.chunk.emit(OpCall, len(e.Arguments))
	return nil
}
