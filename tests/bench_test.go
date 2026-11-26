package tests

import (
	"os"
	"testing"

	"zavet/interpreter"
	"zavet/lexer"
	"zavet/parser"
)

// Бенчмарки. Запуск: go test ./tests -bench=. -benchtime=3s

const fibCode = `
слово фиб(n)
    если n < 2: аминь n
    аминь фиб(n - 1) + фиб(n - 2)
конец

фиб(20)
`

const bubbleSortCode = `
слово пузырёк(массив)
    пусть n = длина(массив)
    измени i = 0
    доколе i < n
        измени j = 0
        доколе j < n - i - 1
            если массив[j] > массив[j + 1]
                измени t = массив[j]
                массив[j] = массив[j + 1]
                массив[j + 1] = t
            конец
            j = j + 1
        конец
        i = i + 1
    конец
    аминь массив
конец

измени a = []
измени k = 100
доколе k > 0
    добавить(a, k)
    k = k - 1
конец
пузырёк(a)
`

const primesCode = `
слово простое(n)
    если n < 2: аминь ложь
    если n < 4: аминь истина
    если n % 2 == 0: аминь ложь
    измени д = 3
    доколе д * д <= n
        если n % д == 0: аминь ложь
        д = д + 2
    конец
    аминь истина
конец

измени счёт = 0
для i от 1 до 500
    если простое(i): счёт = счёт + 1
конец
`

func benchRun(b *testing.B, source string) {
	b.Helper()
	prev := interpreter.OutputWriter
	interpreter.OutputWriter = &discardWriter{}
	defer func() { interpreter.OutputWriter = prev }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := lexer.New(source)
		p := parser.New(l)
		program := p.ParseProgram()
		if len(p.Errors()) > 0 {
			b.Fatalf("parse: %v", p.Errors())
		}
		env := interpreter.NewEnvironment()
		result := interpreter.Eval(program, env)
		if result != nil && result.Type() == "ERROR" {
			b.Fatalf("eval: %s", result.Inspect())
		}
	}
}

type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func BenchmarkFib20(b *testing.B)        { benchRun(b, fibCode) }
func BenchmarkBubbleSort100(b *testing.B) { benchRun(b, bubbleSortCode) }
func BenchmarkPrimes500(b *testing.B)     { benchRun(b, primesCode) }

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
