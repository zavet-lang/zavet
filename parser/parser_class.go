package parser

import (
	"fmt"

	"zavet/ast"
	"zavet/lexer"
)

// parseEnumStatement: перечисление ИМЯ <члены> конец.
// Парсит в LetStatement с Hash-литералом, где значение каждого
// члена равно его имени (как строка).
func (p *Parser) parseEnumStatement() ast.Statement {
	enumTok := p.curToken

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	name := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	p.nextToken()

	pairs := []ast.Expression{}
	for !p.curTokenIs(lexer.END) && !p.curTokenIs(lexer.EOF) {
		if p.curTokenIs(lexer.IDENT) {
			memberName := p.curToken.Literal
			pairs = append(pairs, &ast.StringLiteral{Token: p.curToken, Value: memberName})
			pairs = append(pairs, &ast.StringLiteral{Token: p.curToken, Value: memberName})
		}
		p.nextToken()
	}

	hashLit := &ast.HashLiteral{Token: enumTok, Pairs: make(map[ast.Expression]ast.Expression), KeyOrder: []ast.Expression{}}
	for i := 0; i < len(pairs); i += 2 {
		hashLit.Pairs[pairs[i]] = pairs[i+1]
		hashLit.KeyOrder = append(hashLit.KeyOrder, pairs[i])
	}

	return &ast.LetStatement{
		Token: enumTok,
		Name:  name,
		Value: hashLit,
	}
}

// parseClassStatement: класс ИМЯ [наследует РОДИТЕЛЬ] <методы> конец.
// Парсит в LetStatement с Hash-литералом, где ключи — имена методов,
// значения — функции. Имя 'создать' автоматически переименовывается
// в 'инициализация' (это канонический ключ конструктора).
func (p *Parser) parseClassStatement() ast.Statement {
	className := p.curToken

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}

	name := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	var parentName *ast.Identifier
	if p.peekTokenIs(lexer.EXTENDS) {
		p.nextToken()
		if !p.expectPeek(lexer.IDENT) {
			return nil
		}
		parentName = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	}

	// Сохраняем члены образа в map для удобства, но порядок отслеживаем
	// отдельно — он важен для форматера и для удобочитаемости.
	members := make(map[string]ast.Expression)
	memberKeyTokens := make(map[string]lexer.Token)
	memberOrder := []string{}
	staticNames := []string{}
	staticConstNames := []string{}
	staticTypes := make(map[string]string)

	p.nextToken()

	for !p.curTokenIs(lexer.END) && !p.curTokenIs(lexer.EOF) {
		isStatic := false
		startTok := p.curToken
		if p.curTokenIs(lexer.STATIC) {
			isStatic = true
			p.nextToken()
		}

		if p.curTokenIs(lexer.FUNCTION) {
			methodTok := p.curToken
			if isStatic {
				methodTok = startTok
			}
			p.nextToken()
			if !p.curTokenIs(lexer.IDENT) {
				p.errors = append(p.errors, fmt.Sprintf("❌ Ошибка парсинга в строке %d: после «слово» внутри образа ожидалось имя метода, а явилось %s.",
					p.curToken.Line, translateTokenType(p.curToken.Type)))
				p.nextToken()
				continue
			}
			methodName := p.curToken.Literal
			if methodName == "создать" {
				if isStatic {
					p.errors = append(p.errors, fmt.Sprintf("❌ Ошибка парсинга в строке %d: конструктор «создать» не может быть статичным.", startTok.Line))
					return nil
				}
				methodName = "инициализация"
			}

			if !p.expectPeek(lexer.LPAREN) {
				return nil
			}

			lit := &ast.FunctionLiteral{Token: methodTok, IsStatic: isStatic}
			lit.Parameters, lit.ParamTypes, lit.Defaults, lit.HasRest = p.parseFunctionParametersTyped()

			if !p.expectPeek(lexer.RPAREN) {
				return nil
			}
			if p.peekTokenIs(lexer.RARROW) {
				p.nextToken()
				lit.ReturnType = p.parseTypeAnnotation()
			}

			body, inline := p.parseInlineOrBlockImplicitReturn()
			lit.Body = body
			lit.IsInline = inline
			if _, ok := members[methodName]; !ok {
				memberOrder = append(memberOrder, methodName)
			}
			members[methodName] = lit
			memberKeyTokens[methodName] = methodTok
			if isStatic {
				staticNames = append(staticNames, methodName)
			}
		} else if isStatic && (p.curTokenIs(lexer.LET) || p.curTokenIs(lexer.VAR)) {
			isConst := p.curTokenIs(lexer.LET)
			if !p.expectPeek(lexer.IDENT) {
				return nil
			}
			fieldName := p.curToken.Literal
			if fieldName == "создать" || fieldName == "инициализация" {
				p.errors = append(p.errors, fmt.Sprintf("❌ Ошибка парсинга в строке %d: статичное поле не может называться «%s».", startTok.Line, fieldName))
				return nil
			}
			typeName := ""
			if p.peekTokenIs(lexer.COLON) {
				p.nextToken()
				typeName = p.parseTypeAnnotation()
			}
			if !p.expectPeek(lexer.ASSIGN) {
				return nil
			}
			p.nextToken()
			valExpr := p.parseExpression(LOWEST)
			if valExpr == nil {
				return nil
			}
			if _, ok := members[fieldName]; !ok {
				memberOrder = append(memberOrder, fieldName)
			}
			members[fieldName] = valExpr
			memberKeyTokens[fieldName] = startTok
			staticNames = append(staticNames, fieldName)
			if isConst {
				staticConstNames = append(staticConstNames, fieldName)
			}
			if typeName != "" {
				staticTypes[fieldName] = typeName
			}
		} else if isStatic {
			p.errors = append(p.errors, fmt.Sprintf("❌ Ошибка парсинга в строке %d: после «статичная» ожидалось «слово», «пусть» или «измени», а явилось %s.",
				p.curToken.Line, translateTokenType(p.curToken.Type)))
			return nil
		}
		p.nextToken()
	}

	pairs := []ast.Expression{
		&ast.StringLiteral{Value: "__name__"},
		&ast.StringLiteral{Value: name.Value},
	}
	for _, memberName := range memberOrder {
		pairs = append(pairs, &ast.StringLiteral{Token: memberKeyTokens[memberName], Value: memberName})
		pairs = append(pairs, members[memberName])
	}

	if parentName != nil {
		pairs = append(pairs, &ast.StringLiteral{Value: "__parent_name__"}, &ast.StringLiteral{Value: parentName.Value})
	}
	if len(staticNames) > 0 {
		els := make([]ast.Expression, 0, len(staticNames))
		for _, sn := range staticNames {
			els = append(els, &ast.StringLiteral{Value: sn})
		}
		pairs = append(pairs, &ast.StringLiteral{Value: "__static__"}, &ast.ArrayLiteral{Elements: els})
	}
	if len(staticConstNames) > 0 {
		els := make([]ast.Expression, 0, len(staticConstNames))
		for _, scn := range staticConstNames {
			els = append(els, &ast.StringLiteral{Value: scn})
		}
		pairs = append(pairs, &ast.StringLiteral{Value: "__static_const__"}, &ast.ArrayLiteral{Elements: els})
	}
	if len(staticTypes) > 0 {
		th := &ast.HashLiteral{Pairs: make(map[ast.Expression]ast.Expression), KeyOrder: []ast.Expression{}}
		for _, memberName := range memberOrder {
			if tn, ok := staticTypes[memberName]; ok {
				k := &ast.StringLiteral{Value: memberName}
				th.Pairs[k] = &ast.StringLiteral{Value: tn}
				th.KeyOrder = append(th.KeyOrder, k)
			}
		}
		pairs = append(pairs, &ast.StringLiteral{Value: "__static_types__"}, th)
	}

	hashLit := &ast.HashLiteral{Pairs: make(map[ast.Expression]ast.Expression), KeyOrder: []ast.Expression{}}
	for i := 0; i < len(pairs); i += 2 {
		hashLit.Pairs[pairs[i]] = pairs[i+1]
		hashLit.KeyOrder = append(hashLit.KeyOrder, pairs[i])
	}

	return &ast.LetStatement{
		Token: className,
		Name:  name,
		Value: hashLit,
	}
}

// parseNewExpression: новый ИМЯ_КЛАССА(аргументы).
// Альтернативная форма создания экземпляра класса. Эквивалентно
// просто ИМЯ_КЛАССА(аргументы).
func (p *Parser) parseNewExpression() ast.Expression {
	token := p.curToken
	p.nextToken()

	if !p.curTokenIs(lexer.IDENT) {
		return nil
	}

	className := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	if !p.expectPeek(lexer.LPAREN) {
		return nil
	}

	args := p.parseExpressionList(lexer.RPAREN)

	return &ast.NewExpression{
		Token:     token,
		ClassName: className,
		Arguments: args,
	}
}
