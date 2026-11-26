package tests

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zavet/docgen"
	"zavet/formatter"
	"zavet/interpreter"
	"zavet/lexer"
	"zavet/linter"
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
			out := runProgram(t, string(source), name)
			if strings.Contains(out, "✗") {
				t.Fatalf("встроенный тест в %s провалился:\n%s", name, out)
			}
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

func TestRepeatStaticTooling(t *testing.T) {
	src := `образ Счётчик
    статичная пусть ЛИМИТ: целое = 10
    статичная измени всего = 0

    # Увеличивает счётчик
    статичная слово прибавить(n: целое) -> целое
        Счётчик.всего += n
        аминь Счётчик.всего
    конец

    слово создать(x: целое)
        это.x = x
    конец
конец

повтори 3 раз
    Счётчик.прибавить(1)
конец
повтори 2 раз: возгласи(Счётчик.ЛИМИТ)
возгласи(Счётчик.всего)
`
	formatted, err := formatter.Format(src)
	if err != nil {
		t.Fatalf("formatter.Format: %v", err)
	}
	for _, want := range []string{
		"статичная пусть ЛИМИТ: целое = 10",
		"статичная измени всего = 0",
		"статичная слово прибавить(n: целое) -> целое",
		"повтори 3 раз",
		"повтори 2 раз: возгласи(Счётчик.ЛИМИТ)",
	} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("форматер потерял %q:\n%s", want, formatted)
		}
	}
	// Идемпотентность форматирования и исполнимость результата
	formatted2, err := formatter.Format(formatted)
	if err != nil {
		t.Fatalf("повторный formatter.Format: %v", err)
	}
	if formatted2 != formatted {
		t.Fatalf("форматер не идемпотентен:\n---1---\n%s\n---2---\n%s", formatted, formatted2)
	}
	if out := strings.TrimSpace(runProgram(t, formatted, "tooling.молитва")); out != "10\n10\n3" {
		t.Fatalf("неожиданный вывод после форматтера: %q", out)
	}

	// Линтер не должен выдавать ошибок парсинга/ложных срабатываний
	issues := linter.Lint(formatted, "tooling.молитва")
	for _, iss := range issues {
		if iss.Severity == linter.Error {
			t.Fatalf("линтер выдал ошибку: %s", iss.String())
		}
	}

	// Докген должен сохранять статический префикс
	fdoc, err := docgen.GenerateSource("tooling.молитва", formatted, false)
	if err != nil {
		t.Fatalf("docgen: %v", err)
	}
	md := docgen.RenderMarkdown(fdoc)
	if !strings.Contains(md, "статичная слово прибавить") || !strings.Contains(md, "статичная пусть ЛИМИТ: целое") {
		t.Fatalf("docgen не отобразил статические члены:\n%s", md)
	}

	// Недопустимое употребление «статичная» должно давать ошибку парсинга
	badCodes := []string{
		"статичная слово ф()\nконец",
		"образ А\n    статичная слово создать()\n    конец\nконец",
		"образ Б\n    статичная 123\nконец",
	}
	for _, bad := range badCodes {
		p := parser.New(lexer.New(bad))
		_ = p.ParseProgram()
		if len(p.Errors()) == 0 {
			t.Fatalf("ожидалась ошибка парсинга для:\n%s", bad)
		}
	}
}

// TestCoreQualityImprovements проверяет строгую интерполяцию, ленивые и/или,
// одинарные raw-строки для регулярок, алиасы stdlib, валидацию HTTP-таймаута
// и статическую проверку типов в линтере.
func TestCoreQualityImprovements(t *testing.T) {
	// 1. Ленивые и / или в AST (правый операнд не вычисляется)
	lazyCode := `
пусть x = ничего
возгласи(x != ничего и x["ключ"] == 1)
возгласи(истина или (1 / 0 == 0))
возгласи(ложь и (1 / 0 == 0))
`
	if out := strings.TrimSpace(runProgram(t, lazyCode, "lazy.молитва")); out != "ложь\nистина\nложь" {
		t.Fatalf("ленивые и/или дали %q", out)
	}

	// 2. Интерполяция: валидные формы (включая \" внутри {...} и \{ \}) + raw-одинарные кавычки
	interpOK := `
пусть отв = {"статус": 200}
возгласи("статус {отв[\"статус\"]}, скобки \{ок\}")
возгласи(регвыр.совпадает("abc123", '^[a-z]+\d+$'))
возгласи(json.строка({"a": 1}) == json.создать({"a": 1}))
возгласи(файл.существует("__no_such_file__") == файлы.существует("__no_such_file__"))
`
	if out := strings.TrimSpace(runProgram(t, interpOK, "interp_ok.молитва")); out != "статус 200, скобки {ок}\nистина\nистина\nистина" {
		t.Fatalf("неожиданный вывод interpOK: %q", out)
	}

	// 3. Интерполяция: невалидное выражение, пустые {} и незакрытая { — ошибки
	badInterps := []string{
		`возгласи("плохо {1 + * 2}")`,
		`возгласи("пусто {}")`,
		`возгласи("незакрыто {x")`,
		`пусть s = "незакрыто {x"`,
		`http.получить("http://127.0.0.1:1", -5)`,
	}
	for _, code := range badInterps {
		prev := interpreter.OutputWriter
		interpreter.OutputWriter = &bytes.Buffer{}
		l := lexer.New(code)
		p := parser.New(l)
		prog := p.ParseProgram()
		hasErr := len(p.Errors()) > 0
		var res interpreter.Object
		if !hasErr {
			env := interpreter.NewEnvironment()
			res = interpreter.Eval(prog, env)
			hasErr = res != nil && res.Type() == "ERROR"
		}
		interpreter.OutputWriter = prev
		if !hasErr {
			t.Fatalf("ожидалась ошибка для %q, получено %v", code, res)
		}
	}

	// 4. Статическая проверка типов в линтере
	badTypes := `
пусть а: целое = "строка"
измени б: строка = "ок"
б = 42
слово удвоить(n: целое) -> целое
    аминь "нет"
конец
возгласи(а, б, удвоить("текст"))
`
	issues := linter.Lint(badTypes, "types.молитва")
	typeErrs := 0
	for _, iss := range issues {
		if iss.Code == "type-mismatch" && iss.Severity == linter.Error {
			typeErrs++
		}
	}
	if typeErrs < 4 {
		t.Fatalf("ожидалось >= 4 ошибок type-mismatch, получено %d: %v", typeErrs, issues)
	}

	// Корректный код с опциональными типами не должен давать type-mismatch
	goodTypes := `
пусть опц: целое? = ничего
измени ч: число = 10
ч = 3.5
слово плюс(a: целое, b: целое) -> целое
    аминь a + b
конец
возгласи(опц, ч, плюс(1, 2))
`
	for _, iss := range linter.Lint(goodTypes, "good_types.молитва") {
		if iss.Code == "type-mismatch" {
			t.Fatalf("ложное срабатывание type-mismatch: %s", iss.String())
		}
	}
}

// TestReviewRegressionFixes проверяет все исправления по итогам глубокого ревью:
// песочницу (SQLite ATTACH, HTTP 302 CheckRedirect), лексический скоуп в рекурсии,
// рантайм-проверку типов при переприсваивании, динамические переменные в линтере,
// форматер на одинарных кавычках и \{/\}, и вложенные строки в интерполяции.
func TestReviewRegressionFixes(t *testing.T) {
	evalExpectError := func(code string) {
		t.Helper()
		prev := interpreter.OutputWriter
		interpreter.OutputWriter = &bytes.Buffer{}
		defer func() { interpreter.OutputWriter = prev }()
		l := lexer.New(code)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return
		}
		res := interpreter.Eval(prog, interpreter.NewEnvironment())
		if res == nil || res.Type() != "ERROR" {
			t.Fatalf("ожидалась ошибка для:\n%s\nполучено: %v", code, res)
		}
	}

	// 1. Песочница: SQLite ATTACH DATABASE и file: URI запрещены
	t.Run("safe_sqlite_attach_and_uri", func(t *testing.T) {
		prevMode, prevRoot, prevHosts := interpreter.SafeMode, interpreter.SafeRoot, interpreter.SafeHosts
		interpreter.SafeMode = true
		interpreter.SafeRoot = t.TempDir()
		interpreter.SafeHosts = nil
		defer func() {
			interpreter.SafeMode, interpreter.SafeRoot, interpreter.SafeHosts = prevMode, prevRoot, prevHosts
		}()

		evalExpectError(`
пусть д = бд.открыть(":memory:")
д.выполнить("ATTACH DATABASE 'outside.db' AS ext")
`)
		evalExpectError(`бд.открыть("file:outside.db?mode=rwc")`)
	})

	// 2. Песочница: HTTP 302 редирект на неразрешённый хост блокируется CheckRedirect
	t.Run("safe_http_redirect", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://blocked.invalid/secret", http.StatusFound)
		}))
		defer srv.Close()

		prevMode, prevRoot, prevHosts := interpreter.SafeMode, interpreter.SafeRoot, interpreter.SafeHosts
		interpreter.SafeMode = true
		interpreter.SafeRoot = t.TempDir()
		interpreter.SafeHosts = []string{"127.0.0.1"}
		defer func() {
			interpreter.SafeMode, interpreter.SafeRoot, interpreter.SafeHosts = prevMode, prevRoot, prevHosts
		}()

		evalExpectError(`http.получить("` + srv.URL + `")`)
	})

	// 3. Лексический скоуп в рекурсии: локальная переменная не утекает во вложенный вызов
	t.Run("lexical_scope_recursion", func(t *testing.T) {
		evalExpectError(`
слово ф(n)
    если n == 1
        пусть секрет = 42
        аминь ф(2)
    конец
    аминь секрет
конец
ф(1)
`)
	})

	// 4. Рантайм-проверка типа при переприсваивании измени x: Тип = ...
	t.Run("runtime_var_type_reassign", func(t *testing.T) {
		evalExpectError(`
измени x: целое = 1
x = "строка"
`)
	})

	// 5. Линтер не даёт ложных type-mismatch для динамического измени без аннотации
	t.Run("linter_dynamic_var", func(t *testing.T) {
		src := "измени x = ничего\nx = \"привет\"\nвозгласи(x)\n"
		for _, iss := range linter.Lint(src, "dyn.молитва") {
			if iss.Code == "type-mismatch" {
				t.Fatalf("ложное срабатывание на динамической переменной: %s", iss.String())
			}
		}
	})

	// 6. Форматер сохраняет исполнимость 24_веб_сайт.молитва и не выводит сырые 0x01/0x02
	t.Run("formatter_strings_and_web_example", func(t *testing.T) {
		raw, err := os.ReadFile("../examples/24_веб_сайт.молитва")
		if err != nil {
			t.Fatalf("read 24_веб_сайт: %v", err)
		}
		formatted, err := formatter.Format(string(raw))
		if err != nil {
			t.Fatalf("format 24_веб_сайт: %v", err)
		}
		out := runProgram(t, formatted, "24_formatted.молитва")
		if strings.Contains(out, "✗") {
			t.Fatalf("после форматирования 24_веб_сайт провалил встроенный тест:\n%s", out)
		}

		escSrc := "пусть x = 1\nвозгласи(\"\\{ok\\} {x}\")\n"
		escFmt, err := formatter.Format(escSrc)
		if err != nil {
			t.Fatalf("format escaped braces: %v", err)
		}
		if strings.ContainsRune(escFmt, '\x01') || strings.ContainsRune(escFmt, '\x02') {
			t.Fatalf("форматер записал сырые управляющие байты 0x01/0x02: %q", escFmt)
		}
		if got := strings.TrimSpace(runProgram(t, escFmt, "esc.молитва")); got != "{ok} 1" {
			t.Fatalf("неожиданный вывод после форматирования \\{\\}: %q", got)
		}
	})

	// 7. Интерполяция с '}' внутри вложенного строкового литерала и многострочные """..."""
	t.Run("interpolation_nested_brace_and_multiline", func(t *testing.T) {
		src := "пусть d = {\"}\": 7}\nвозгласи(\"val={d['}']}\")\nпусть шаб = \"\"\"{{имя}} {css}\"\"\"\nвозгласи(шаб)\n"
		got := strings.TrimSpace(runProgram(t, src, "nested.молитва"))
		if got != "val=7\n{{имя}} {css}" {
			t.Fatalf("неожиданный вывод: %q", got)
		}
	})
}
