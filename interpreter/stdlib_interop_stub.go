//go:build js

package interpreter

// В WASM вызов внешних процессов Python / Node.js не поддерживается:
// регистрируем заглушки с понятными сообщениями об ошибке.
func registerInteropModules() {
	mkUnsupported := func(mod string) func(args ...Object) Object {
		return func(args ...Object) Object {
			return ErrorWithHint(
				getCurrentCallToken(),
				"модуль «"+mod+"» не поддерживается в WebAssembly",
				"Запускай программы с внешними процессами через нативный бинарник zavet.",
			)
		}
	}
	stdModules["питон"] = makeHashFromBuiltins(map[string]func(args ...Object) Object{
		"выражение": mkUnsupported("питон"),
		"выполнить": mkUnsupported("питон"),
		"файл":      mkUnsupported("питон"),
		"версия":    mkUnsupported("питон"),
	})
	stdModules["js"] = makeHashFromBuiltins(map[string]func(args ...Object) Object{
		"выражение": mkUnsupported("js"),
		"выполнить": mkUnsupported("js"),
		"файл":      mkUnsupported("js"),
		"версия":    mkUnsupported("js"),
	})
}
