package lexer

// TokenType представляет тип токена
type TokenType string

// Token представляет один токен в исходном коде
type Token struct {
	Type     TokenType
	Literal  string
	Line     int
	Column   int
	Filename string
}

// Типы токенов
const (
	// Специальные
	ILLEGAL = "ILLEGAL"
	EOF     = "EOF"
	COMMENT = "COMMENT" // # комментарий, сохраняется для форматера

	// Идентификаторы и литералы
	IDENT  = "IDENT"  // имена переменных, функций
	INT    = "INT"    // 123
	FLOAT  = "FLOAT"  // 123.45
	STRING = "STRING" // "hello"

	// Операторы
	ASSIGN   = "="
	PLUS     = "+"
	MINUS    = "-"
	BANG     = "!"
	ASTERISK = "*"
	SLASH    = "/"
	INT_DIV  = "//"
	PERCENT  = "%"
	
	PLUS_ASSIGN     = "+="
	MINUS_ASSIGN    = "-="
	ASTERISK_ASSIGN = "*="
	SLASH_ASSIGN    = "/="
	
	EQ     = "=="
	NOT_EQ = "!="
	LT     = "<"
	GT     = ">"
	LTE    = "<="
	GTE    = ">="

	// Разделители
	COMMA        = ","
	COLON        = ":"
	SEMICOLON    = ";"
	DOT          = "."
	DOTDOT       = ".."
	SPREAD       = "..."
	OPTIONAL_DOT = "?."
	LPAREN       = "("
	RPAREN       = ")"
	LBRACE       = "{"
	RBRACE       = "}"
	LBRACKET     = "["
	RBRACKET     = "]"
	ARROW        = "=>"
	RARROW       = "->"
	PIPE         = "|>"
	AT           = "@"
	QUESTION     = "?"
	NULLISH      = "??"

	// Ключевые слова
	LET      = "ПУСТЬ"
	VAR      = "ИЗМЕНИ"
	FUNCTION = "СЛОВО"
	RETURN   = "АМИНЬ"
	IF       = "ЕСЛИ"
	ELSE_IF  = "ИНАЧЕЕСЛИ"
	ELSE     = "ИНАЧЕ"
	FOR      = "ДЛЯ"
	FROM     = "ОТ"
	TO       = "ДО"
	STEP     = "ПО"
	IN       = "В"
	WHILE    = "ДОКОЛЕ"
	REPEAT   = "ПОВТОРИ"
	TIMES    = "РАЗ"
	CLASS    = "ОБРАЗ"
	EXTENDS  = "НАСЛЕДУЕТ"
	ENUM     = "РОД"
	TEST     = "ТЕСТ"
	ASSERT   = "ПРОВЕРИТЬ"
	STATIC   = "СТАТИЧНАЯ"
	YIELD    = "ВЫДАТЬ"
	ASYNC    = "АСИНХ"
	AWAIT    = "ЖДАТЬ"
	NEW      = "НОВЫЙ"
	PARENT   = "РОДИТЕЛЬ"
	END      = "КОНЕЦ"
	TRUE     = "ИСТИНА"
	FALSE    = "ЛОЖЬ"
	NULL_LIT = "НИЧЕГО"
	AND      = "И"
	OR       = "ИЛИ"
	NOT      = "НЕ"
	THIS     = "ЭТО"
	TRY      = "ПОПЫТКА"
	CATCH    = "ПОЙМАТЬ"
	FINALLY  = "ВСЕГДА"
	THROW    = "БРОСИТЬ"
	BREAK    = "ПРЕРВИ"
	CONTINUE = "ПРОДОЛЖИ"
	MATCH    = "РАЗБЕРИ"
	WHEN     = "КОГДА"
	IMPORT   = "ПРИИМИ"
	EXPORT   = "ОТКРОЙ"
	FROM_KW  = "ИЗ"
	AS       = "КАК"
)

// keywords — ключевые слова языка Завет.
var keywords = map[string]TokenType{
	"пусть":     LET,
	"измени":    VAR,
	"слово":     FUNCTION,
	"аминь":     RETURN,
	"доколе":    WHILE,
	"истина":    TRUE,
	"ложь":      FALSE,
	"разбери":   MATCH,
	"образ":     CLASS,
	"род":       ENUM,
	"приими":    IMPORT,
	"открой":    EXPORT,
	"если":      IF,
	"иначеесли": ELSE_IF,
	"иначе":     ELSE,
	"для":       FOR,
	"от":        FROM,
	"до":        TO,
	"по":        STEP,
	"в":         IN,
	"повтори":   REPEAT,
	"раз":       TIMES,
	"тест":      TEST,
	"проверить": ASSERT,
	"статичная": STATIC,
	"выдать":    YIELD,
	"асинх":     ASYNC,
	"ждать":     AWAIT,
	"наследует": EXTENDS,
	"родитель":  PARENT,
	"новый":     NEW,
	"конец":     END,
	"ничего":    NULL_LIT,
	"и":         AND,
	"или":       OR,
	"не":        NOT,
	"это":       THIS,
	"попытка":   TRY,
	"поймать":   CATCH,
	"всегда":    FINALLY,
	"бросить":   THROW,
	"прерви":     BREAK,
	"продолжи":   CONTINUE,
	"когда":     WHEN,
	"из":        FROM_KW,
	"как":       AS,
}

// LookupIdent проверяет, является ли идентификатор ключевым словом
func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
