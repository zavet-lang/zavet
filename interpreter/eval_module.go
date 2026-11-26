package interpreter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zavet/ast"
	"zavet/lexer"
	"zavet/parser"
)

// jsonUnmarshal — обёртка, чтобы не светить json в публичном API
// файла и упростить тестируемость.
func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// evalLoad — встроенная функция загрузить("путь.молитва"): читает файл и
// выполняет его в текущем окружении.
func evalLoad(tok lexer.Token, path string, env *Environment) Object {
	if err := denyIfSafe("загрузить"); err != nil {
		return err
	}
	resolved := resolveImportPath(tok.Filename, path)
	if err := CheckPath(resolved); err != nil {
		return err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return ErrorFileNotFound(tok, resolved)
	}

	l := lexer.NewWithFilename(string(content), resolved)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		errMsg := fmt.Sprintf("при призывании книги '%s':", resolved)
		for _, msg := range p.Errors() {
			errMsg += "\n  " + msg
		}
		return &Error{
			Message: fmt.Sprintf("❌ ОШИБКА %s", errMsg),
			Line:    tok.Line,
			Column:  tok.Column,
		}
	}

	return Eval(program, env)
}

// resolveImportPath приводит путь к импортируемому файлу.
//
// Алгоритм:
//   1. Абсолютный путь — оставляем как есть.
//   2. Путь начинается с "./" или "../" или содержит расширение
//      ".молитва" — это локальный файл, разрешаем относительно файла-
//      импортёра (или CWD, если импортёр неизвестен).
//   3. Иначе считаем именем пакета: ищем в пакеты/ начиная от
//      каталога импортёра и поднимаясь вверх, пока не найдём
//      пакет.json (корень проекта). Используем точка_входа из
//      манифеста пакета или главный.молитва по умолчанию.
//   4. Если пакет не найден — возвращаем исходный путь, чтобы
//      ошибку показал os.ReadFile с понятным сообщением.
func resolveImportPath(importerFile, importPath string) string {
	if filepath.IsAbs(importPath) {
		return importPath
	}

	// Локальный путь: явно относительный или с расширением .молитва.
	isLocal := strings.HasPrefix(importPath, "./") ||
		strings.HasPrefix(importPath, "../") ||
		strings.HasSuffix(importPath, ".молитва")
	if isLocal {
		if importerFile == "" {
			return importPath
		}
		dir := filepath.Dir(importerFile)
		return filepath.Join(dir, importPath)
	}

	// Пакетный импорт: ищем корень проекта.
	startDir := "."
	if importerFile != "" {
		startDir = filepath.Dir(importerFile)
	}
	root, err := findProjectRoot(startDir)
	if err != nil {
		// Не нашли манифест — fallback к относительному поведению.
		if importerFile == "" {
			return importPath
		}
		return filepath.Join(filepath.Dir(importerFile), importPath)
	}

	pkgDir := filepath.Join(root, "пакеты", importPath)
	// Пытаемся прочитать пакет.json самого пакета и взять его точку входа.
	if pm, err := readPackageEntry(pkgDir); err == nil && pm != "" {
		return filepath.Join(pkgDir, pm)
	}
	// Запасной вариант: пакеты/имя/главный.молитва
	candidate := filepath.Join(pkgDir, "главный.молитва")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Ещё один: пакеты/имя/имя.молитва
	candidate = filepath.Join(pkgDir, importPath+".молитва")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Не нашли — вернём первый кандидат, ошибку покажет читатель файла.
	return filepath.Join(pkgDir, "главный.молитва")
}

// findProjectRoot ищет папку с пакет.json начиная с указанной и
// поднимаясь вверх. Возвращает абсолютный путь к корню проекта
// или ошибку, если манифест не найден.
func findProjectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if _, err := os.Stat(filepath.Join(dir, "пакет.json")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("в дому нет пакет.json")
		}
		dir = parent
	}
}

// readPackageEntry читает поле точка_входа из пакет.json пакета.
// Если файла нет или поле пустое — возвращает пустую строку.
func readPackageEntry(pkgDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(pkgDir, "пакет.json"))
	if err != nil {
		return "", err
	}
	// Минимальный парсер JSON: ищем "точка_входа": "..." вручную,
	// чтобы не тащить сюда зависимость от pkgmgr (избежать цикла).
	type partial struct {
		Entry string `json:"точка_входа"`
	}
	var p partial
	if err := jsonUnmarshal(data, &p); err != nil {
		return "", err
	}
	return p.Entry, nil
}

// evalImportStatement выполняет: приими ИМЯ из "путь.молитва" [как АЛИАС].
// Путь разрешается относительно файла, в котором стоит импорт
// (а не относительно текущего каталога процесса). Создаёт
// изолированное окружение, выполняет файл, собирает экспорты в
// словарь и связывает его с именем модуля в текущем окружении.
func evalImportStatement(node *ast.ImportStatement, env *Environment) Object {
	// Сначала проверяем стандартные модули — они доступны без
	// установки пакета и без файла. Например: импорт бд из "бд".
	if !strings.Contains(node.Path, "/") && !strings.HasSuffix(node.Path, ".молитва") {
		if mod, ok := stdModules[node.Path]; ok {
			env.Set(node.Name.Value, mod)
			return NULL
		}
	}

	path := resolveImportPath(node.Token.Filename, node.Path)
	if perr := CheckPath(path); perr != nil {
		return perr
	}

	content, err := os.ReadFile(path)
	if err != nil {
		// Если импорт похож на пакет (без слешей и без .молитва) — даём
		// специальную подсказку про менеджер пакетов.
		isPackage := !strings.Contains(node.Path, "/") &&
			!strings.HasSuffix(node.Path, ".молитва")
		if isPackage {
			return ErrorWithHint(
				node.Token,
				fmt.Sprintf("пакет «%s» не призван в стан", node.Path),
				fmt.Sprintf("Призови его командой: zavet подключить %s", node.Path),
			)
		}
		return newErrorWithToken(node.Token, "не смог прочитать файл: %s", err.Error())
	}

	l := lexer.NewWithFilename(string(content), path)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		return &Error{
			Message: fmt.Sprintf("❌ ОШИБКА при призывании из '%s':\n  %s", path, p.Errors()[0]),
			Line:    node.Token.Line,
			Column:  node.Token.Column,
		}
	}

	moduleEnv := NewEnvironment()
	moduleEnv.outer = env

	result := Eval(program, moduleEnv)
	if isError(result) {
		return result
	}

	moduleObj := NewHash()

	// Сортируем имена, чтобы порядок ключей в собранном модуле
	// был стабильным.
	exportNames := make([]string, 0, len(moduleEnv.exports))
	for k := range moduleEnv.exports {
		exportNames = append(exportNames, k)
	}
	sort.Strings(exportNames)
	for _, k := range exportNames {
		moduleObj.Set(&String{Value: k}, moduleEnv.exports[k])
	}

	moduleName := node.Name.Value
	if node.Alias != nil {
		moduleName = node.Alias.Value
	}
	env.Set(moduleName, moduleObj)

	return moduleObj
}

// evalExportStatement помечает объявление как экспортируемое.
func evalExportStatement(node *ast.ExportStatement, env *Environment) Object {
	result := Eval(node.Statement, env)
	if isError(result) {
		return result
	}

	switch stmt := node.Statement.(type) {
	case *ast.LetStatement:
		env.Export(stmt.Name.Value)
	case *ast.ExpressionStatement:
		if fn, ok := stmt.Expression.(*ast.FunctionLiteral); ok {
			if fn.Name != nil {
				env.Export(fn.Name.Value)
			}
		}
	}

	return result
}
