package interpreter

import (
	"zavet/bible"
)

func init() {
	стихFn := func(args ...Object) Object {
		if len(args) != 3 {
			return builtinErrorWrongArgCount("стих", 3, len(args))
		}
		книга, ok := args[0].(*String)
		if !ok {
			return builtinErrorWrongArgType("стих", 1, "STRING", args[0].Type())
		}
		глава, ok := args[1].(*Integer)
		if !ok {
			return builtinErrorWrongArgType("стих", 2, "INTEGER", args[1].Type())
		}
		номер, ok := args[2].(*Integer)
		if !ok {
			return builtinErrorWrongArgType("стих", 3, "INTEGER", args[2].Type())
		}
		п, err := bible.Глобальное()
		if err != nil {
			return newBuiltinError("%s", err.Error())
		}
		текст, err := п.Стих(книга.Value, глава.Value, номер.Value)
		if err != nil {
			return newBuiltinError("%s", err.Error())
		}
		return &String{Value: текст}
	}

	builtins["стих"] = &Builtin{Fn: стихFn}
	builtins["писание"] = &Builtin{Fn: стихFn}
}
