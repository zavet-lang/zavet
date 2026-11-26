//go:build js && wasm
package main

import (
	"bytes"
	"fmt"
	"syscall/js"
	"zavet/lexer"
	"zavet/parser"
	"zavet/interpreter"
)

func runZavet(code string) (result string) {
	// Защита от паник - в WASM критично, иначе runtime умирает
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("❌ Внутренний грех: %v\n\n📖 Коли это связано с http_получить, знай: в браузере поддержка ограничена.\nПоищи локальную установку или используй mock-данные.", r)
		}
	}()
	
	// Перехватываем вывод
	outputBuffer := &bytes.Buffer{}
	interpreter.OutputWriter = outputBuffer
	
	l := lexer.NewWithFilename(code, "playground.ya")
	p := parser.New(l)
	program := p.ParseProgram()
	
	if len(p.Errors()) > 0 {
		result := "❌ Ошибки в речении:\n"
		for _, err := range p.Errors() {
			result += "  - " + err + "\n"
		}
		return result
	}
	
	env := interpreter.NewEnvironment()
	evaluated := interpreter.Eval(program, env)
	
	output := outputBuffer.String()
	
	if evaluated != nil && evaluated.Type() == "ERROR" {
		return output + "\n" + evaluated.Inspect()
	}
	
	return output
}

func main() {
	js.Global().Set("zavetRun", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		// Каждый вызов - в собственном recover-обёрнутом контексте
		defer func() {
			recover()
		}()
		
		if len(args) < 1 {
			return "Ошибка: нет кода"
		}
		code := args[0].String()
		return runZavet(code)
	}))
	
	println("Завет WASM v1.0 загружен!")
	select {}
}
