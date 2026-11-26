//go:build js

package interpreter

// В WASM графическое окно не поддерживается: регистрируем заглушку с понятной ошибкой.
func registerWindowModule() {
	unsupported := func(args ...Object) Object {
		return ErrorWithHint(
			getCurrentCallToken(),
			"модуль «окно» не поддерживается в WebAssembly",
			"Запускай графические программы через нативный бинарник zavet.",
		)
	}
	fns := map[string]func(args ...Object) Object{
		"создать":       unsupported,
		"очистить":      unsupported,
		"прямоугольник": unsupported,
		"текст":         unsupported,
		"иконка":        unsupported,
		"список_иконок": unsupported,
		"заголовок":     unsupported,
		"клавиша":       unsupported,
		"кадр":          unsupported,
		"задержка":      unsupported,
		"закрыть":       unsupported,
		"мышь_x":        unsupported,
		"мышь_y":        unsupported,
		"мышь":          unsupported,
		"клик":          unsupported,
		"колесо":        unsupported,
		"символ":        unsupported,
		"открыто":       unsupported,
		"ширина":        unsupported,
		"высота":        unsupported,
	}
	stdModules["окно"] = makeHashFromBuiltins(fns)
}

func init() {
	registerWindowModule()
}
