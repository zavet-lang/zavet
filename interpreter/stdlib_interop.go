//go:build !js

package interpreter

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

func init() {
	registerInteropModules()
}

// registerInteropModules регистрирует модули «питон» и «js»
// для вызова Python и Node.js с прозрачной передачей данных через JSON.
func registerInteropModules() {
	pythonBin := func() string {
		for _, bin := range []string{"python3", "python"} {
			if p, err := exec.LookPath(bin); err == nil {
				return p
			}
		}
		return ""
	}
	nodeBin := func() string {
		if p, err := exec.LookPath("node"); err == nil {
			return p
		}
		return ""
	}

	runBridge := func(modName, bin string, cmdArgs []string, stdinData string) Object {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, cmdArgs...)
		if stdinData != "" {
			cmd.Stdin = strings.NewReader(stdinData)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errText := strings.TrimSpace(stderr.String())
			if errText == "" {
				errText = err.Error()
			}
			return ErrorWithHint(currentCallToken, modName+": "+errText, "")
		}
		outStr := strings.TrimSpace(stdout.String())
		if outStr == "" {
			return NULL
		}
		var parsed any
		if err := json.Unmarshal([]byte(outStr), &parsed); err == nil {
			return nativeToObject(parsed)
		}
		return &String{Value: outStr}
	}

	// Модуль «питон»
	pyFns := map[string]func(args ...Object) Object{
		"доступен": func(args ...Object) Object {
			return nativeBoolToBooleanObject(pythonBin() != "")
		},
		// питон.выражение(код, [данные]) -> значение Завета
		"выражение": func(args ...Object) Object {
			if err := denyIfSafe("питон.выражение"); err != nil {
				return err
			}
			if len(args) < 1 || len(args) > 2 {
				return ErrorWithHint(currentCallToken, "питон.выражение(код, [данные])", "")
			}
			code, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("питон.выражение", 1, "строка", args[0].Type())
			}
			bin := pythonBin()
			if bin == "" {
				return ErrorWithHint(currentCallToken, "Python не найден в PATH", "Установи Python 3 и добавь в PATH.")
			}
			inJSON := "null"
			if len(args) == 2 {
				inJSON = objectToJSON(args[1])
			}
			wrapper := "import sys, json\nданные = json.loads(sys.stdin.read())\ndata = данные\n_res = eval(" + strconvQuote(code.Value) + ", {'данные': данные, 'data': data})\nprint(json.dumps(_res, ensure_ascii=False))\n"
			return runBridge("питон", bin, []string{"-c", wrapper}, inJSON)
		},
		// питон.выполнить(код, [данные]) -> stdout или JSON-значение (если задана переменная результат/result)
		"выполнить": func(args ...Object) Object {
			if err := denyIfSafe("питон.выполнить"); err != nil {
				return err
			}
			if len(args) < 1 || len(args) > 2 {
				return ErrorWithHint(currentCallToken, "питон.выполнить(код, [данные])", "")
			}
			code, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("питон.выполнить", 1, "строка", args[0].Type())
			}
			bin := pythonBin()
			if bin == "" {
				return ErrorWithHint(currentCallToken, "Python не найден в PATH", "Установи Python 3 и добавь в PATH.")
			}
			inJSON := "null"
			if len(args) == 2 {
				inJSON = objectToJSON(args[1])
			}
			wrapper := "import sys, json\nданные = json.loads(sys.stdin.read())\ndata = данные\n_ns = {'данные': данные, 'data': data}\nexec(" + strconvQuote(code.Value) + ", _ns)\nif 'результат' in _ns:\n    print(json.dumps(_ns['результат'], ensure_ascii=False))\nelif 'result' in _ns:\n    print(json.dumps(_ns['result'], ensure_ascii=False))\n"
			return runBridge("питон", bin, []string{"-c", wrapper}, inJSON)
		},
		// питон.файл(путь, [аргументы_массив]) -> stdout или JSON
		"файл": func(args ...Object) Object {
			if err := denyIfSafe("питон.файл"); err != nil {
				return err
			}
			if len(args) < 1 || len(args) > 2 {
				return ErrorWithHint(currentCallToken, "питон.файл(путь, [аргументы])", "")
			}
			p, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("питон.файл", 1, "строка", args[0].Type())
			}
			bin := pythonBin()
			if bin == "" {
				return ErrorWithHint(currentCallToken, "Python не найден в PATH", "Установи Python 3 и добавь в PATH.")
			}
			cmdArgs := []string{p.Value}
			if len(args) == 2 {
				if arr, ok := args[1].(*Array); ok {
					for _, el := range arr.Elements {
						cmdArgs = append(cmdArgs, el.Inspect())
					}
				}
			}
			return runBridge("питон", bin, cmdArgs, "")
		},
	}
	stdModules["питон"] = makeHashFromBuiltins(pyFns)

	// Модуль «js» (Node.js)
	jsFns := map[string]func(args ...Object) Object{
		"доступен": func(args ...Object) Object {
			return nativeBoolToBooleanObject(nodeBin() != "")
		},
		// js.выражение(код, [данные]) -> значение Завета
		"выражение": func(args ...Object) Object {
			if err := denyIfSafe("js.выражение"); err != nil {
				return err
			}
			if len(args) < 1 || len(args) > 2 {
				return ErrorWithHint(currentCallToken, "js.выражение(код, [данные])", "")
			}
			code, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("js.выражение", 1, "строка", args[0].Type())
			}
			bin := nodeBin()
			if bin == "" {
				return ErrorWithHint(currentCallToken, "Node.js не найден в PATH", "Установи Node.js и добавь в PATH.")
			}
			inJSON := "null"
			if len(args) == 2 {
				inJSON = objectToJSON(args[1])
			}
			wrapper := "const fs = require('fs'); const данные = JSON.parse(fs.readFileSync(0, 'utf8')); const data = данные; const _res = (" + code.Value + "); process.stdout.write(JSON.stringify(_res === undefined ? null : _res));"
			return runBridge("js", bin, []string{"-e", wrapper}, inJSON)
		},
		// js.выполнить(код, [данные]) -> stdout или JSON
		"выполнить": func(args ...Object) Object {
			if err := denyIfSafe("js.выполнить"); err != nil {
				return err
			}
			if len(args) < 1 || len(args) > 2 {
				return ErrorWithHint(currentCallToken, "js.выполнить(код, [данные])", "")
			}
			code, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("js.выполнить", 1, "строка", args[0].Type())
			}
			bin := nodeBin()
			if bin == "" {
				return ErrorWithHint(currentCallToken, "Node.js не найден в PATH", "Установи Node.js и добавь в PATH.")
			}
			inJSON := "null"
			if len(args) == 2 {
				inJSON = objectToJSON(args[1])
			}
			wrapper := "const fs = require('fs'); const данные = JSON.parse(fs.readFileSync(0, 'utf8')); const data = данные;\n" + code.Value
			return runBridge("js", bin, []string{"-e", wrapper}, inJSON)
		},
		// js.файл(путь, [аргументы_массив]) -> stdout или JSON
		"файл": func(args ...Object) Object {
			if err := denyIfSafe("js.файл"); err != nil {
				return err
			}
			if len(args) < 1 || len(args) > 2 {
				return ErrorWithHint(currentCallToken, "js.файл(путь, [аргументы])", "")
			}
			p, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("js.файл", 1, "строка", args[0].Type())
			}
			bin := nodeBin()
			if bin == "" {
				return ErrorWithHint(currentCallToken, "Node.js не найден в PATH", "Установи Node.js и добавь в PATH.")
			}
			cmdArgs := []string{p.Value}
			if len(args) == 2 {
				if arr, ok := args[1].(*Array); ok {
					for _, el := range arr.Elements {
						cmdArgs = append(cmdArgs, el.Inspect())
					}
				}
			}
			return runBridge("js", bin, cmdArgs, "")
		},
	}
	stdModules["js"] = makeHashFromBuiltins(jsFns)
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
