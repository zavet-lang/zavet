package interpreter

import (
	"fmt"
	"zavet/lexer"
)

// ErrorWithHint создает ошибку с подсказкой
func ErrorWithHint(tok lexer.Token, message string, hint string) *Error {
	fullMessage := message
	if hint != "" {
		fullMessage = fmt.Sprintf("%s\n📖 Наставление: %s", message, hint)
	}

	location := ""
	if tok.Filename != "" {
		location = fmt.Sprintf("[%s:%d] ", tok.Filename, tok.Line)
	} else if tok.Line > 0 {
		location = fmt.Sprintf("[строка %d] ", tok.Line)
	}

	// Добавляем stack trace если есть активные вызовы.
	trace := formatCallStack()

	return &Error{
		Message:    fmt.Sprintf("❌ ОШИБКА %s%s%s", location, fullMessage, trace),
		RawMessage: message,
		Line:       tok.Line,
		Column:     tok.Column,
	}
}

// Типичные ошибки с подсказками

func ErrorCannotReassignConst(tok lexer.Token, varName string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("сын мой, «%s» сотворена словом «пусть» и не меняется", varName),
		"Словом «пусть» сие сотворено неизменным. Аще хочешь менять значение, сотвори его словом «измени».",
	)
}

func ErrorVariableNotDeclared(tok lexer.Token, varName string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("сын мой, «%s» ещё не сотворена: ни «пусть», ни «измени» о ней не сказано", varName),
		"Сотвори его словом «пусть» или «измени» прежде, чем призывать.",
	)
}

func ErrorIndexOutOfRange(tok lexer.Token, index int64, length int) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("индекс %d вне пределов стана (в массиве всего %d)", index, length),
		fmt.Sprintf("Дозволенные индексы: от 0 до %d. Счёт ведётся с нуля.", length-1),
	)
}

func ErrorDivisionByZero(tok lexer.Token) *Error {
	return ErrorWithHint(
		tok,
		"сын мой, не сотвори деления на ноль",
		"Вникни, что делитель не нуль, прежде чем делить.",
	)
}

func ErrorTypeMismatch(tok lexer.Token, left, operator, right string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("смешение несходного: %s %s %s", left, operator, right),
		"Не смешивай несмешиваемого: оба операнда должны быть сходны родом.",
	)
}

func ErrorUnknownOperator(tok lexer.Token, left, operator, right string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("неведомое действо: %s %s %s", left, operator, right),
		"Сей знак не ведом для таких видов данных.",
	)
}

func ErrorWrongArgumentCount(tok lexer.Token, funcName string, expected, got int) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("слову «%s» надлежит принять %d приношение(й), принесено %d", funcName, expected, got),
		fmt.Sprintf("Призови слово с верным числом приношений: %s(...)", funcName),
	)
}

func ErrorWrongArgumentType(tok lexer.Token, funcName string, argNum int, expected, got string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("слово «%s»: приношение %d должно быть %s, а дано %s", funcName, argNum, expected, got),
		fmt.Sprintf("Принеси значение вида %s", expected),
	)
}

func ErrorIdentifierNotFound(tok lexer.Token, name string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("имя «%s» не найдено в книге сотворённого", name),
		"Вникни в написание или сотвори имя прежде, чем призывать.",
	)
}

// translateType - переводит технические имена типов в человеческие на русском
func translateType(typeName string) string {
	switch typeName {
	case "INTEGER":
		return "целое число"
	case "FLOAT":
		return "дробное число"
	case "STRING":
		return "строка"
	case "BOOLEAN":
		return "булево"
	case "ARRAY":
		return "массив"
	case "HASH":
		return "объект"
	case "FUNCTION":
		return "слово"
	case "BUILTIN":
		return "встроенное слово"
	case "INSTANCE":
		return "экземпляр образа"
	case "NULL":
		return "пусто"
	case "ERROR":
		return "ошибка"
	default:
		return typeName
	}
}

func ErrorNotCallable(tok lexer.Token, typeName string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("«%s» нельзя призвать: это не слово", translateType(typeName)),
		"Призывай слово, а не иное.",
	)
}

func ErrorPropertyNotFound(tok lexer.Token, propName, objType string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("у образа нет свойства «%s» (образ вида %s)", propName, objType),
		"Вникни в имя свойства или прибавь его в образ.",
	)
}

func ErrorMethodNotFound(tok lexer.Token, methodName, className string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("метод «%s» не найден в образе «%s»", methodName, className),
		"Вникни в имя метода или сотвори его в образе.",
	)
}

func ErrorInvalidDestructuring(tok lexer.Token, expected, got string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("разложение %s требует %s, а дано %s", expected, expected, got),
		"Разбирай лишь сосуд подходящего вида.",
	)
}

func ErrorSpreadNotArray(tok lexer.Token, got string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("«...» (рассеяние) дозволено лишь к массивам, а дано %s", got),
		"Употребляй «...» только с массивами: [...стадо]",
	)
}

func ErrorFileNotFound(tok lexer.Token, filename string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("файл «%s» не найден, как заблудшая овца", filename),
		"Вникни в путь: есть ли файл на деле.",
	)
}

func ErrorInvalidJSON(tok lexer.Token) *Error {
	return ErrorWithHint(
		tok,
		"скрижали JSON начертаны неверно",
		"Вникни в кавычки, запятые и скобки JSON.",
	)
}

func ErrorInvalidRegex(tok lexer.Token, pattern string) *Error {
	return ErrorWithHint(
		tok,
		fmt.Sprintf("неверный узор регулярного выражения: %s", pattern),
		"Вникни в строение узора.",
	)
}
