//go:build js

package interpreter

// В WASM вызов внешних процессов Python / Node.js не поддерживается.
func registerInteropModules() {}
