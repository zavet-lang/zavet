package vm

import (
	"bytes"
	"strings"
	"testing"

	"zavet/lexer"
	"zavet/parser"
)

func runVM(t *testing.T, src string) string {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	chunk, err := Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	prev := Output
	Output = &buf
	defer func() { Output = prev }()
	if err := New(chunk).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	return buf.String()
}

func TestVMArithmetic(t *testing.T) {
	out := runVM(t, `
пусть a = 2 + 3 * 4
возгласи(a)
`)
	if strings.TrimSpace(out) != "14" {
		t.Fatalf("got %q", out)
	}
}

func TestVMWhile(t *testing.T) {
	out := runVM(t, `
измени i = 0
измени s = 0
доколе i < 5
    s += i
    i += 1
конец
возгласи(s)
`)
	if strings.TrimSpace(out) != "10" {
		t.Fatalf("got %q", out)
	}
}

func TestVMStringConcat(t *testing.T) {
	out := runVM(t, `возгласи("привет " + 1)`)
	if strings.TrimSpace(out) != "привет 1" {
		t.Fatalf("got %q", out)
	}
}

func TestVMForBreakContinue(t *testing.T) {
	out := runVM(t, `
измени s = 0
для i от 1 до 10
    если i == 3: продолжи
    если i == 7: прерви
    s += i
конец
возгласи(s)
`)
	if strings.TrimSpace(out) != "18" {
		t.Fatalf("expected 18, got %q", out)
	}
}

func TestVMFunctionsAndRecursion(t *testing.T) {
	out := runVM(t, `
слово фиб(n)
    если n <= 1: аминь n
    аминь фиб(n - 1) + фиб(n - 2)
конец
возгласи(фиб(15))
`)
	if strings.TrimSpace(out) != "610" {
		t.Fatalf("expected 610, got %q", out)
	}
}

func TestVMArraysAndLogic(t *testing.T) {
	out := runVM(t, `
пусть а = [10, 20, 30]
а[1] = 25
возгласи(а[1], длина(а), истина и не ложь)
`)
	if strings.TrimSpace(out) != "25 3 истина" {
		t.Fatalf("got %q", out)
	}
}

func TestVMRepeat(t *testing.T) {
	out := runVM(t, `
измени с = 0
повтори 5 раз
    с += 2
конец
повтори 0 раз: с += 100
повтори -3 раз: с += 100
измени к = 3
повтори к раз
    к = 10
    с += 1
конец
возгласи(с)
`)
	if strings.TrimSpace(out) != "13" {
		t.Fatalf("expected 13, got %q", out)
	}
}

func TestVMRepeatBreakContinue(t *testing.T) {
	out := runVM(t, `
измени i = 0
измени s = 0
повтори 6 раз
    i += 1
    если i == 2: продолжи
    если i == 5: прерви
    s += i
конец
возгласи(s)
`)
	if strings.TrimSpace(out) != "8" {
		t.Fatalf("expected 8, got %q", out)
	}
}

func TestVMRepeatNonIntError(t *testing.T) {
	l := lexer.New(`повтори "три" раз: возгласи(1)`)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	chunk, err := Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := New(chunk).Run(); err == nil {
		t.Fatalf("expected runtime error for non-integer repeat count")
	}
}

func TestVMHashLiteralAndIndex(t *testing.T) {
	out := runVM(t, `
пусть д = {"имя": "Завет", 1: "число", "1": "строка"}
д["версия"] = 2
возгласи(д["имя"], д[1], д["1"], д["версия"], д["нет"], длина(д))
`)
	if strings.TrimSpace(out) != "Завет число строка 2 ничего 4" {
		t.Fatalf("got %q", out)
	}
}

func TestVMInterpolatedStringFallback(t *testing.T) {
	l := lexer.New(`пусть x = 1` + "\n" + `возгласи("x={x}")`)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	if _, err := Compile(prog); err == nil {
		t.Fatalf("expected CompileError for interpolated string in VM")
	}
}

func BenchmarkVMFib20(b *testing.B) {
	src := `
слово фиб(n)
    если n <= 1: аминь n
    аминь фиб(n - 1) + фиб(n - 2)
конец
пусть r = фиб(20)
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	chunk, err := Compile(prog)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := New(chunk).Run(); err != nil {
			b.Fatal(err)
		}
	}
}
