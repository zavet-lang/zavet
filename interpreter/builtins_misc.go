package interpreter

import (
	"fmt"
)

// Прочее: ошибки, импорт, тесты, async.

func init() {
	builtins["ошибка"] = &Builtin{
		Fn: func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("ошибка", 1, len(args))
			}
			return ErrorWithHint(
				currentCallToken,
				args[0].Inspect(),
				"Сия ошибка возложена явно словом 'ошибка()'.",
			)
		},
	}
	builtins["загрузить"] = &Builtin{
		Fn: func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("загрузить", 1, len(args))
			}
			if args[0].Type() != "STRING" {
				return builtinErrorWrongArgType("загрузить", 1, "STRING (строка)", args[0].Type())
			}
			// Эта функция будет вызываться из eval.go с доступом к env
   return ErrorWithHint(currentCallToken, "внутренний грех: загрузить() призвано неверно", "Сей грех в самом толкователе, а не в тебе.")
		},
	}
	builtins["установить"] = &Builtin{
		Fn: func(args ...Object) Object {
			if len(args) != 3 {
				return builtinErrorWrongArgCount("установить", 3, len(args))
			}
			if args[0].Type() != "INSTANCE" {
    return ErrorWithHint(currentCallToken, "первое приношение должно быть экземпляром образа", "Принеси объект образа.")
			}
			if args[1].Type() != "STRING" {
    return ErrorWithHint(currentCallToken, "второе приношение должно быть строкой", "Принеси строку вторым приношением.")
			}
			
			inst := args[0].(*Instance)
			key := args[1].(*String).Value
			inst.Properties[key] = args[2]
			
			return NULL
		},
	}
	builtins["__тест__"] = &Builtin{
		Fn: func(args ...Object) Object {
			if len(args) != 2 {
				return builtinErrorWrongArgCount("__тест__", 2, len(args))
			}
			name, _ := args[0].(*String)
			fn := args[1]
			result := ApplyFunctionCallback(fn, []Object{})
			if result != nil && result.Type() == "ERROR" {
				fmt.Fprintf(OutputWriter, "  ✗ %s\n    %s\n", name.Value, result.(*Error).Message)
				testFailures++
			} else {
				fmt.Fprintf(OutputWriter, "  ✓ %s\n", name.Value)
				testPasses++
			}
			return NULL
		},
	}
	builtins["__проверить__"] = &Builtin{
		Fn: func(args ...Object) Object {
			if len(args) < 1 {
				return ErrorWithHint(currentCallToken, "«проверить» потребно условие", "")
			}
			cond := args[0]
			if cond.Type() == "BOOLEAN" && !cond.(*Boolean).Value {
				return ErrorWithHint(currentCallToken, "испытание не пройдено", "")
			}
			if cond.Type() == "NULL" {
				return ErrorWithHint(currentCallToken, "испытание не пройдено: значение пусто", "")
			}
			return NULL
		},
	}
	builtins["все_ждать"] = &Builtin{
		Fn: func(args ...Object) Object {
			if len(args) != 1 || args[0].Type() != "ARRAY" {
				return builtinErrorWrongArgType("все_ждать", 1, "массив futures", args[0].Type())
			}
			arr := args[0].(*Array)
			results := make([]Object, len(arr.Elements))
			for i, el := range arr.Elements {
				if fut, ok := el.(*Future); ok {
					results[i] = fut.Wait()
				} else {
					results[i] = el
				}
			}
			return &Array{Elements: results}
		},
	}
	builtins["готово"] = &Builtin{
		Fn: func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("готово", 1, len(args))
			}
			if fut, ok := args[0].(*Future); ok {
				if fut.Ready() {
					return TRUE
				}
				return FALSE
			}
			return TRUE
		},
	}
}
