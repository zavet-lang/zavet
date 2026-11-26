package interpreter

import (
	"fmt"
	"strings"

	"zavet/ast"
	"zavet/lexer"
	"zavet/parser"
)

// evalInterpolatedString вычисляет шаблонную строку с {выражениями}.
//
// Любая ошибка внутри {...} (пустые скобки, незакрытая скобка,
// ошибка парсинга или вычисления) возвращает явную ошибку выполнения.
// Для буквальных фигурных скобок в двойных кавычках используются \{ и \},
// а строки без интерполяции пишутся в одинарных кавычках '...'.
func evalInterpolatedString(tok lexer.Token, template string, env *Environment) Object {
	var result strings.Builder

	for i := 0; i < len(template); i++ {
		if template[i] == '\x01' {
			result.WriteByte('{')
			continue
		}
		if template[i] == '\x02' {
			result.WriteByte('}')
			continue
		}
		if template[i] == '{' {
			// Находим закрывающую скобку с учётом вложенности.
			j := i + 1
			depth := 1
			for j < len(template) && depth > 0 {
				if template[j] == '{' {
					depth++
				} else if template[j] == '}' {
					depth--
				}
				j++
			}

			if depth != 0 {
				return ErrorWithHint(
					tok,
					"незакрытая скобка '{' в интерполяции строки",
					"Закрой выражение '}' или экранируй скобку как \\{ (либо используй одинарные кавычки '...').",
				)
			}

			exprStr := strings.TrimSpace(template[i+1 : j-1])
			if len(exprStr) == 0 {
				return ErrorWithHint(
					tok,
					"пустая интерполяция {} в строке",
					"Укажи выражение внутри {...} или экранируй скобки как \\{\\} (либо используй одинарные кавычки '...').",
				)
			}

			l := lexer.New(exprStr)
			p := parser.New(l)
			program := p.ParseProgram()

			if len(p.Errors()) > 0 || len(program.Statements) == 0 {
				detail := "неверное выражение"
				if len(p.Errors()) > 0 {
					detail = p.Errors()[0]
				}
				return ErrorWithHint(
					tok,
					fmt.Sprintf("ошибка в интерполяции {%s}: %s", exprStr, detail),
					"Проверь выражение внутри {...}. Для буквальных скобок используй \\{ и \\} или одинарные кавычки '...'.",
				)
			}

			stmt, ok := program.Statements[0].(*ast.ExpressionStatement)
			if !ok {
				return ErrorWithHint(
					tok,
					fmt.Sprintf("внутри интерполяции {%s} ожидается выражение", exprStr),
					"Для буквальных скобок используй \\{ и \\} или одинарные кавычки '...'.",
				)
			}

			val := Eval(stmt.Expression, env)
			if isError(val) {
				return val
			}

			result.WriteString(val.Inspect())
			i = j - 1
			continue
		}

		result.WriteByte(template[i])
	}

	return &String{Value: result.String()}
}
