package interpreter

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"zavet/ast"
	"zavet/lexer"
	"zavet/parser"
)

// ActiveDebugger — глобальный экземпляр активного отладчика.
// Когда nil (обычный запуск), проверка стоит один branch и не замедляет Eval.
var ActiveDebugger *Debugger

type debugMode int

const (
	debugStepInto debugMode = iota
	debugStepOver
	debugStepOut
	debugContinue
)

// Debugger — пошаговый интерактивный отладчик Завета.
type Debugger struct {
	sourceLines []string
	filename    string
	breakpoints map[int]bool
	mode        debugMode
	targetDepth int
	lastLine    int
	in          *bufio.Scanner
	out         io.Writer
	aborted     bool
}

// NewDebugger создаёт отладчик для файла.
func NewDebugger(filename, source string, in io.Reader, out io.Writer) *Debugger {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return &Debugger{
		sourceLines: strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n"),
		filename:    filename,
		breakpoints: make(map[int]bool),
		mode:        debugStepInto, // останавливаемся на первой инструкции
		in:          bufio.NewScanner(in),
		out:         out,
	}
}

// AddBreakpoint ставит точку останова на строку (1-based).
func (d *Debugger) AddBreakpoint(line int) {
	d.breakpoints[line] = true
}

// BeforeStatement вызывается интерпретатором перед выполнением каждой инструкции.
func (d *Debugger) BeforeStatement(stmt ast.Statement, env *Environment) *Error {
	if d.aborted {
		return ErrorWithHint(stmt.GetToken(), "отладка прервана", "")
	}
	tok := stmt.GetToken()
	line := tok.Line
	if line <= 0 {
		return nil
	}
	// Не останавливаемся повторно на той же строке при входе в ту же инструкцию
	if line == d.lastLine && !d.breakpoints[line] {
		return nil
	}

	depth := len(callStack)
	shouldPause := false
	if d.breakpoints[line] {
		shouldPause = true
	} else {
		switch d.mode {
		case debugStepInto:
			shouldPause = true
		case debugStepOver:
			shouldPause = depth <= d.targetDepth
		case debugStepOut:
			shouldPause = depth < d.targetDepth
		case debugContinue:
			shouldPause = false
		}
	}

	if !shouldPause {
		return nil
	}
	d.lastLine = line
	return d.prompt(line, env)
}

func (d *Debugger) prompt(line int, env *Environment) *Error {
	d.printLine(line)
	for {
		fmt.Fprint(d.out, "(отладка) ")
		if !d.in.Scan() {
			d.mode = debugContinue
			return nil
		}
		raw := strings.TrimSpace(d.in.Text())
		if raw == "" {
			// Enter повторяет шаг
			d.mode = debugStepOver
			d.targetDepth = len(callStack)
			return nil
		}
		parts := strings.Fields(raw)
		cmd := parts[0]
		arg := strings.TrimSpace(strings.TrimPrefix(raw, cmd))

		switch cmd {
		case "шаг", "s", "step":
			d.mode = debugStepInto
			return nil
		case "далее", "n", "next":
			d.mode = debugStepOver
			d.targetDepth = len(callStack)
			return nil
		case "выйти", "o", "out":
			d.mode = debugStepOut
			d.targetDepth = len(callStack)
			return nil
		case "продолжить", "c", "cont":
			d.mode = debugContinue
			return nil
		case "точка", "b", "break":
			ln, err := strconv.Atoi(arg)
			if err != nil || ln <= 0 {
				fmt.Fprintln(d.out, "Укажи номер строки: точка 10")
				continue
			}
			d.breakpoints[ln] = true
			fmt.Fprintf(d.out, "Точка останова поставлена на строке %d\n", ln)
		case "убрать", "d", "del":
			ln, err := strconv.Atoi(arg)
			if err != nil {
				fmt.Fprintln(d.out, "Укажи номер строки: убрать 10")
				continue
			}
			delete(d.breakpoints, ln)
			fmt.Fprintf(d.out, "Точка останова на строке %d снята\n", ln)
		case "точки", "bl":
			if len(d.breakpoints) == 0 {
				fmt.Fprintln(d.out, "Точек останова нет.")
				continue
			}
			var ls []int
			for ln := range d.breakpoints {
				ls = append(ls, ln)
			}
			sort.Ints(ls)
			fmt.Fprintf(d.out, "Точки останова: %v\n", ls)
		case "переменные", "v", "vars":
			vars := env.LocalVars()
			if len(vars) == 0 {
				fmt.Fprintln(d.out, "(локальных переменных нет)")
				continue
			}
			names := make([]string, 0, len(vars))
			for k := range vars {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				fmt.Fprintf(d.out, "  %s = %s\n", k, vars[k].Inspect())
			}
		case "вычислить", "p", "print":
			if arg == "" {
				fmt.Fprintln(d.out, "Укажи выражение: вычислить x + 1")
				continue
			}
			prev := ActiveDebugger
			ActiveDebugger = nil
			l := lexer.New(arg)
			p := parser.New(l)
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				fmt.Fprintln(d.out, p.Errors()[0])
			} else {
				res := Eval(prog, env)
				if res != nil {
					fmt.Fprintln(d.out, res.Inspect())
				}
			}
			ActiveDebugger = prev
		case "стек", "bt", "stack":
			if len(callStack) == 0 {
				fmt.Fprintf(d.out, "  <верхний уровень> — %s:%d\n", d.filename, line)
				continue
			}
			for i := len(callStack) - 1; i >= 0; i-- {
				f := callStack[i]
				fmt.Fprintf(d.out, "  %d. %s — %s:%d\n", len(callStack)-i, f.FunctionName, f.CallToken.Filename, f.CallToken.Line)
			}
		case "код", "l", "list":
			d.printContext(line, 3)
		case "выход", "q", "quit":
			d.aborted = true
			return ErrorWithHint(lexer.Token{Line: line, Filename: d.filename}, "отладка завершена пользователем", "")
		case "помощь", "h", "help":
			d.printHelp()
		default:
			fmt.Fprintf(d.out, "Неизвестная команда «%s». Напиши «помощь».\n", cmd)
		}
	}
}

func (d *Debugger) printLine(line int) {
	if line >= 1 && line <= len(d.sourceLines) {
		fmt.Fprintf(d.out, "▸ %s:%d | %s\n", d.filename, line, d.sourceLines[line-1])
	} else {
		fmt.Fprintf(d.out, "▸ %s:%d\n", d.filename, line)
	}
}

func (d *Debugger) printContext(line, radius int) {
	start := line - radius
	if start < 1 {
		start = 1
	}
	end := line + radius
	if end > len(d.sourceLines) {
		end = len(d.sourceLines)
	}
	for i := start; i <= end; i++ {
		mark := "  "
		if i == line {
			mark = "▸ "
		} else if d.breakpoints[i] {
			mark = "● "
		}
		fmt.Fprintf(d.out, "%s%4d | %s\n", mark, i, d.sourceLines[i-1])
	}
}

func (d *Debugger) printHelp() {
	fmt.Fprintln(d.out, "Команды отладчика Завета:")
	fmt.Fprintln(d.out, "  шаг (s)              — шаг с заходом в слово")
	fmt.Fprintln(d.out, "  далее (n) / Enter    — шаг без захода")
	fmt.Fprintln(d.out, "  выйти (o)            — выйти из текущего слова")
	fmt.Fprintln(d.out, "  продолжить (c)       — идти до следующей точки останова")
	fmt.Fprintln(d.out, "  точка N (b N)        — поставить точку останова на строке N")
	fmt.Fprintln(d.out, "  убрать N (d N)       — снять точку останова")
	fmt.Fprintln(d.out, "  точки (bl)           — список точек останова")
	fmt.Fprintln(d.out, "  переменные (v)       — локальные переменные")
	fmt.Fprintln(d.out, "  вычислить <выр> (p)  — вычислить выражение в текущем контексте")
	fmt.Fprintln(d.out, "  стек (bt)            — стек вызовов")
	fmt.Fprintln(d.out, "  код (l)              — показать строки вокруг текущей")
	fmt.Fprintln(d.out, "  выход (q)            — прервать отладку")
}
