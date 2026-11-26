package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zavet/interpreter"
	"zavet/lexer"
	"zavet/parser"
)

// runProgram выполняет программу на Завете и возвращает её вывод.
func runProgram(t *testing.T, source, filename string) string {
	t.Helper()

	// Перенаправляем вывод интерпретатора в буфер.
	prev := interpreter.OutputWriter
	var buf bytes.Buffer
	interpreter.OutputWriter = &buf
	defer func() { interpreter.OutputWriter = prev }()

	l := lexer.NewWithFilename(source, filename)
	p := parser.New(l)
	program := p.ParseProgram()

	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("ошибки парсинга %s:\n%s", filename, strings.Join(errs, "\n"))
	}

	env := interpreter.NewEnvironment()
	result := interpreter.Eval(program, env)

	if result != nil && result.Type() == "ERROR" {
		t.Fatalf("ошибка выполнения %s:\n%s", filename, result.Inspect())
	}

	return buf.String()
}

// TestGoldenCases проходит по всем .ya файлам в tests/cases/ и проверяет,
// что их вывод совпадает с .expected рядом.
func TestGoldenCases(t *testing.T) {
	dir := "cases"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("не удалось прочитать %s: %v", dir, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".молитва") {
			continue
		}
		// Файлы, начинающиеся с _, — вспомогательные (модули
		// для тестов импорта и т.п.), а не самостоятельные кейсы.
		if strings.HasPrefix(name, "_") {
			continue
		}

		caseName := strings.TrimSuffix(name, ".молитва")
		yaPath := filepath.Join(dir, name)
		expPath := filepath.Join(dir, caseName+".expected")

		t.Run(caseName, func(t *testing.T) {
			source, err := os.ReadFile(yaPath)
			if err != nil {
				t.Fatalf("не удалось прочитать %s: %v", yaPath, err)
			}

			expected, err := os.ReadFile(expPath)
			if err != nil {
				t.Fatalf("не удалось прочитать %s: %v", expPath, err)
			}

			actual := runProgram(t, string(source), yaPath)
			want := strings.ReplaceAll(string(expected), "\r\n", "\n")
			got := strings.ReplaceAll(actual, "\r\n", "\n")
			// Windows пути в диагностиках: cases\x.ya vs cases/x.ya
			want = strings.ReplaceAll(want, `\`, `/`)
			got = strings.ReplaceAll(got, `\`, `/`)

			if got != want {
				t.Errorf(
					"вывод не совпадает с ожидаемым\n--- ожидалось (%s) ---\n%s\n--- получено ---\n%s",
					expPath, want, got,
				)
			}
		})
	}
}

// TestExamplesRun проверяет, что все файлы в examples/ просто
// выполняются без ошибок (без сравнения вывода — там может быть
// недетерминированный вывод вроде времени).
func TestExamplesRun(t *testing.T) {
	dir := "../examples"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("папка examples недоступна: %v", err)
		return
	}

	skip := map[string]bool{
		"18_оболочка.молитва":    true, // GUI-ish demo
		"19_подключение.молитва": true, // относительные импорты из examples/
		"20_кликер_демо.молитва": true, // пишет файлы / интерактив
		"21_змейка.молитва":      true, // GUI
		"22_браузер.молитва":     true, // GUI + сеть
		"13_файлы.молитва":       true, // побочные эффекты FS в CI
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".молитва") {
			continue
		}
		if skip[name] {
			t.Run(name, func(t *testing.T) {
				t.Skip("GUI / интерактивный пример — не в CI")
			})
			continue
		}

		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("не удалось прочитать %s: %v", name, err)
			}
			_ = runProgram(t, string(source), name)
		})
	}
}

// TestRemovedKeywords проверяет, что старые формы действительно удалены.
func TestRemovedKeywords(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		// пусть / истина / ложь — снова канон Завет (библейские слова)
		{"старое конст", "конст x = 5"},
		{"старое функция", "функция f()\nконец"},
		{"старое вернуть", "слово f()\n    вернуть 1\nконец\nf()"},
		{"старое да", "пусть x = да"},
		{"старое вывод", "вывод(1)"},
		{"процедура удалена", "процедура f()\nконец"},
		{"// не комментарий", "// это не комментарий"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := interpreter.OutputWriter
			interpreter.OutputWriter = &bytes.Buffer{}
			defer func() { interpreter.OutputWriter = prev }()

			l := lexer.New(tc.code)
			p := parser.New(l)
			program := p.ParseProgram()

			if len(p.Errors()) > 0 {
				return // парсинг сразу провалился — это и ожидалось
			}

			env := interpreter.NewEnvironment()
			result := interpreter.Eval(program, env)
			if result != nil && result.Type() == "ERROR" {
				return // ошибка интерпретации — тоже ожидаемо
			}

			t.Errorf("ожидалась ошибка для удалённой формы: %s", tc.code)
		})
	}
}
