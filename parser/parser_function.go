package parser

import (
	"fmt"

	"zavet/ast"
	"zavet/lexer"
)

// === Литералы и определения функций ===

// parseFunctionLiteral: функция [имя](параметры) <блок> конец.
// Имя необязательно; если есть, литерал может использоваться как
// именованная функция в LetStatement.
func (p *Parser) parseFunctionLiteral() ast.Expression {
	lit := &ast.FunctionLiteral{Token: p.curToken}

	if p.peekTokenIs(lexer.IDENT) {
		p.nextToken()
		lit.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	}

	if !p.expectPeek(lexer.LPAREN) {
		return nil
	}

	lit.Parameters, lit.ParamTypes, lit.Defaults, lit.HasRest = p.parseFunctionParametersTyped()

	if !p.expectPeek(lexer.RPAREN) {
		return nil
	}

	// Опциональный тип возврата: слово сумма(a: целое, b: целое) -> целое
	if p.peekTokenIs(lexer.RARROW) {
		p.nextToken() // на '->'
		lit.ReturnType = p.parseTypeAnnotation()
	}

	body, inline := p.parseInlineOrBlockImplicitReturn()
	lit.Body = body
	lit.IsInline = inline

	return lit
}

// parseTypeAnnotation читает имя типа (целое, строка, слово, ничего, Точка)
// и необязательный суффикс '?' для nullable-типа (строка?).
// Вызывается, когда curToken стоит на ':' или '->'.
func (p *Parser) parseTypeAnnotation() string {
	if !p.peekTokenIs(lexer.IDENT) && !p.peekTokenIs(lexer.FUNCTION) && !p.peekTokenIs(lexer.NULL_LIT) {
		p.errors = append(p.errors, fmt.Sprintf("❌ Ошибка парсинга в строке %d: ожидалось имя типа, а явилось %s",
			p.peekToken.Line, translateTokenType(p.peekToken.Type)))
		return ""
	}
	p.nextToken()
	name := p.curToken.Literal
	if p.peekTokenIs(lexer.QUESTION) {
		p.nextToken()
		name += "?"
	}
	return name
}

// parseFunctionParametersTyped парсит параметры с опциональными аннотациями типов,
// default-значениями и rest-параметром (...args).
func (p *Parser) parseFunctionParametersTyped() ([]*ast.Identifier, []string, []ast.Expression, bool) {
	identifiers := []*ast.Identifier{}
	paramTypes := []string{}
	defaults := []ast.Expression{}
	hasRest := false

	if p.peekTokenIs(lexer.RPAREN) {
		return identifiers, paramTypes, defaults, hasRest
	}

	parseOne := func() bool {
		if p.curTokenIs(lexer.SPREAD) {
			hasRest = true
			p.nextToken()
		}
		if !p.curTokenIs(lexer.IDENT) {
			return false
		}
		ident := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		identifiers = append(identifiers, ident)

		typeName := ""
		if p.peekTokenIs(lexer.COLON) {
			p.nextToken() // на ':'
			typeName = p.parseTypeAnnotation()
		}
		paramTypes = append(paramTypes, typeName)

		if p.peekTokenIs(lexer.ASSIGN) {
			p.nextToken() // на '='
			p.nextToken() // на выражение
			defaults = append(defaults, p.parseExpression(LOWEST))
		} else {
			defaults = append(defaults, nil)
		}
		return true
	}

	p.nextToken()
	if !parseOne() {
		return identifiers, paramTypes, defaults, hasRest
	}

	for p.peekTokenIs(lexer.COMMA) {
		if hasRest {
			p.errors = append(p.errors, "«...args» (остаток) должен стоять последним")
			return identifiers, paramTypes, defaults, hasRest
		}
		p.nextToken() // на ','
		p.nextToken() // на следующий параметр
		if !parseOne() {
			break
		}
	}

	return identifiers, paramTypes, defaults, hasRest
}

// parseFunctionParametersFull — обёртка обратной совместимости.
func (p *Parser) parseFunctionParametersFull() ([]*ast.Identifier, []ast.Expression, bool) {
	idents, _, defaults, hasRest := p.parseFunctionParametersTyped()
	return idents, defaults, hasRest
}

// parseFunctionParameters — упрощённая версия, возвращает только имена.
// Используется там, где default/rest не нужны.
func (p *Parser) parseFunctionParameters() []*ast.Identifier {
	idents, _, _ := p.parseFunctionParametersFull()
	return idents
}

// === Вызовы и списки аргументов ===

// parseCallExpression: foo(arg1, arg2, ...). Левая часть — функция.
func (p *Parser) parseCallExpression(function ast.Expression) ast.Expression {
	exp := &ast.CallExpression{Token: p.curToken, Function: function}
	exp.Arguments = p.parseExpressionList(lexer.RPAREN)
	return exp
}

// parseExpressionList читает разделённый запятыми список выражений
// до указанного завершающего токена. Поддерживает spread (...x).
func (p *Parser) parseExpressionList(end lexer.TokenType) []ast.Expression {
	list := []ast.Expression{}

	if p.peekTokenIs(end) {
		p.nextToken()
		return list
	}

	p.nextToken()

	if p.curTokenIs(lexer.SPREAD) {
		spreadToken := p.curToken
		p.nextToken()
		list = append(list, &ast.SpreadExpression{
			Token: spreadToken,
			Value: p.parseExpression(LOWEST),
		})
	} else {
		list = append(list, p.parseExpression(LOWEST))
	}

	for p.peekTokenIs(lexer.COMMA) {
		p.nextToken()
		p.nextToken()

		if p.curTokenIs(lexer.SPREAD) {
			spreadToken := p.curToken
			p.nextToken()
			list = append(list, &ast.SpreadExpression{
				Token: spreadToken,
				Value: p.parseExpression(LOWEST),
			})
		} else {
			list = append(list, p.parseExpression(LOWEST))
		}
	}

	if !p.expectPeek(end) {
		return nil
	}

	return list
}

// === Async/await ===

// parseAsyncExpression: асинх <выражение> — запускает выражение в
// горутине, возвращает Future.
func (p *Parser) parseAsyncExpression() ast.Expression {
	tok := p.curToken
	p.nextToken()
	body := p.parseExpression(PREFIX)
	return &ast.AsyncExpression{Token: tok, Body: body}
}

// parseAwaitExpression: ждать <выражение> — ждёт результат Future.
func (p *Parser) parseAwaitExpression() ast.Expression {
	tok := p.curToken
	p.nextToken()
	body := p.parseExpression(PREFIX)
	return &ast.AwaitExpression{Token: tok, Body: body}
}

// === Декораторы ===

// parseDecoratedFunction: @декоратор1 [@декоратор2 ...] функция имя().
// Сохраняет декораторы как отдельный AST-узел для форматера.
// Семантику разворачивает эвалуатор: @a @b f становится a(b(f)).
func (p *Parser) parseDecoratedFunction() ast.Statement {
	tok := p.curToken
	decorators := []ast.Expression{}

	for p.curTokenIs(lexer.AT) {
		p.nextToken()
		dec := p.parseExpression(LOWEST)
		decorators = append(decorators, dec)
		p.nextToken()
	}

	if !p.curTokenIs(lexer.FUNCTION) {
		msg := fmt.Sprintf("после украшений ожидается слово 'функция', а дано '%s'", p.curToken.Literal)
		p.errors = append(p.errors, msg)
		return nil
	}

	fnExpr := p.parseFunctionLiteral()
	fn, ok := fnExpr.(*ast.FunctionLiteral)
	if !ok || fn.Name == nil {
		p.errors = append(p.errors, "@-украшение потребно именованной функции")
		return nil
	}

	return &ast.DecoratedFunctionStatement{
		Token:      tok,
		Decorators: decorators,
		Function:   fn,
	}
}
