package interpreter

import (
	"fmt"
	"strings"
	"sync"
	"zavet/lexer"
)

// CallFrame — кадр стека вызовов.
// Создаётся при вызове пользовательской функции и удаляется при возврате.
type CallFrame struct {
	FunctionName string      // имя функции, например "сумма" или "Класс.метод"
	CallToken    lexer.Token // место в коде откуда был вызов
}

// callStack — стек активных вызовов, защищённый мьютексом для безопасности
// при параллельных вызовах из асинх и генераторов.
var (
	callStackMu      sync.Mutex
	callStack        []CallFrame
	callTokenMu      sync.RWMutex
	currentCallToken lexer.Token
)

// setCurrentCallToken потокобезопасно обновляет текущий токен вызова для builtin-функций.
func setCurrentCallToken(tok lexer.Token) {
	callTokenMu.Lock()
	currentCallToken = tok
	callTokenMu.Unlock()
}

// getCurrentCallToken потокобезопасно читает текущий токен вызова для builtin-функций.
func getCurrentCallToken() lexer.Token {
	callTokenMu.RLock()
	tok := currentCallToken
	callTokenMu.RUnlock()
	return tok
}

// pushFrame добавляет кадр в стек вызовов.
func pushFrame(name string, callTok lexer.Token) {
	callStackMu.Lock()
	callStack = append(callStack, CallFrame{
		FunctionName: name,
		CallToken:    callTok,
	})
	callStackMu.Unlock()
}

// popFrame убирает последний кадр из стека.
func popFrame() {
	callStackMu.Lock()
	if len(callStack) > 0 {
		callStack = callStack[:len(callStack)-1]
	}
	callStackMu.Unlock()
}

// resetCallStack очищает стек вызовов. Используется при перезапуске
// интерпретатора (например, между тестами или при загрузке модулей).
func resetCallStack() {
	callStackMu.Lock()
	callStack = callStack[:0]
	callStackMu.Unlock()
}

// callStackDepth возвращает текущую глубину стека (для отладчика).
func callStackDepth() int {
	callStackMu.Lock()
	n := len(callStack)
	callStackMu.Unlock()
	return n
}

// callStackSnapshot возвращает безопасную копию стека вызовов.
func callStackSnapshot() []CallFrame {
	callStackMu.Lock()
	snap := make([]CallFrame, len(callStack))
	copy(snap, callStack)
	callStackMu.Unlock()
	return snap
}

// formatCallStack форматирует стек вызовов в читаемый текст.
// Пустой стек не печатается (нет смысла показывать «вызов из main»).
func formatCallStack() string {
	snap := callStackSnapshot()
	if len(snap) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n📚 Стек вызовов (от свежего к старому):\n")
	// Идём от верха стека (последний вызов) к низу
	for i := len(snap) - 1; i >= 0; i-- {
		frame := snap[i]
		loc := ""
		if frame.CallToken.Filename != "" {
			loc = fmt.Sprintf("%s:%d", frame.CallToken.Filename, frame.CallToken.Line)
		} else if frame.CallToken.Line > 0 {
			loc = fmt.Sprintf("строка %d", frame.CallToken.Line)
		}
		if loc != "" {
			b.WriteString(fmt.Sprintf("   %d. %s — %s\n", len(snap)-i, frame.FunctionName, loc))
		} else {
			b.WriteString(fmt.Sprintf("   %d. %s\n", len(snap)-i, frame.FunctionName))
		}
	}
	return b.String()
}
