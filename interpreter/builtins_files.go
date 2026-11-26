package interpreter

import (
	"fmt"
	"os"
)

// Работа с файлами.

func init() {
	builtins["читать_файл"] = &Builtin{
		Fn: func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("читать_файл", 1, len(args))
			}
			if args[0].Type() != "STRING" {
				return builtinErrorWrongArgType("читать_файл", 1, "STRING (строка с путём)", args[0].Type())
			}
			path := args[0].(*String).Value
			if err := CheckPath(path); err != nil {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return ErrorFileNotFound(currentCallToken, path)
			}
			return &String{Value: string(content)}
		},
	}
	builtins["записать_файл"] = &Builtin{
		Fn: func(args ...Object) Object {
			if err := denyIfSafe("записать_файл"); err != nil {
				return err
			}
			if len(args) != 2 {
				return builtinErrorWrongArgCount("записать_файл", 2, len(args))
			}
			if args[0].Type() != "STRING" {
				return builtinErrorWrongArgType("записать_файл", 1, "STRING (путь к файлу)", args[0].Type())
			}
			if args[1].Type() != "STRING" {
				return builtinErrorWrongArgType("записать_файл", 2, "STRING (содержимое)", args[1].Type())
			}
			path := args[0].(*String).Value
			if err := CheckPath(path); err != nil {
				return err
			}
			content := args[1].(*String).Value
			err := os.WriteFile(path, []byte(content), 0644)
			if err != nil {
				return ErrorWithHint(
					currentCallToken,
					fmt.Sprintf("не смог записать файл '%s': %s", path, err.Error()),
					"Вникни, дозволен ли доступ и есть ли папка.",
				)
			}
			return NULL
		},
	}
	builtins["существует_файл"] = &Builtin{
		Fn: func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("существует_файл", 1, len(args))
			}
			if args[0].Type() != "STRING" {
				return builtinErrorWrongArgType("существует_файл", 1, "STRING (путь к файлу)", args[0].Type())
			}
			path := args[0].(*String).Value
			if err := CheckPath(path); err != nil {
				return err
			}
			_, err := os.Stat(path)
			if err == nil {
				return TRUE
			}
			if os.IsNotExist(err) {
				return FALSE
			}
			return ErrorWithHint(
				currentCallToken,
				fmt.Sprintf("не смог проверить файл: %s", err.Error()),
				"Вникни, дозволен ли доступ к файлу.",
			)
		},
	}
}
