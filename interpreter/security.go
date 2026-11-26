package interpreter

import (
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// SafeMode запрещает опасные операции: процессы, произвольную
// загрузку кода, запись файлов, сетевые серверы.
// Включается флагом CLI --безопасный.
var SafeMode bool

// SafeRoot — корень песочницы файлов (по умолчанию cwd при --безопасный).
// Чтение/запись вне корня запрещены.
var SafeRoot string

// SafeHosts — разрешённые хосты для исходящего HTTP (пусто = сеть запрещена).
var SafeHosts []string

func denyIfSafe(op string) Object {
	if !SafeMode {
		return nil
	}
	return ErrorWithHint(
		currentCallToken,
		"сия операция «"+op+"» запретна в безопасном стане",
		"Запусти без --безопасный, коли доверяешь сему свитку.",
	)
}

// guardDeny оборачивает перечисленные слова модуля: в безопасном режиме они
// запрещены целиком. prefix — имя модуля с точкой (для сообщения об ошибке).
func guardDeny(fns map[string]func(args ...Object) Object, prefix string, names ...string) {
	for _, n := range names {
		inner, ok := fns[n]
		if !ok {
			continue
		}
		op := prefix + n
		fns[n] = func(args ...Object) Object {
			if err := denyIfSafe(op); err != nil {
				return err
			}
			return inner(args...)
		}
	}
}

// guardPath оборачивает перечисленные слова модуля: первый аргумент-строка
// считается путём и в безопасном режиме должен лежать внутри песочницы.
func guardPath(fns map[string]func(args ...Object) Object, names ...string) {
	for _, n := range names {
		inner, ok := fns[n]
		if !ok {
			continue
		}
		fns[n] = func(args ...Object) Object {
			if len(args) > 0 {
				if s, isStr := args[0].(*String); isStr {
					if err := CheckPath(s.Value); err != nil {
						return err
					}
				}
			}
			return inner(args...)
		}
	}
}

// InitSafeMode включает песочницу: корень = текущая директория.
func InitSafeMode() {
	SafeMode = true
	if SafeRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			SafeRoot = wd
		}
	}
}

// CheckPath разрешает путь только внутри SafeRoot (в SafeMode).
func CheckPath(path string) Object {
	if !SafeMode || SafeRoot == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ErrorWithHint(currentCallToken, "неверный путь: "+path, "")
	}
	root, err := filepath.Abs(SafeRoot)
	if err != nil {
		return ErrorWithHint(currentCallToken, "неверный корень песочницы", "")
	}
	abs = filepath.Clean(abs)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ErrorWithHint(
			currentCallToken,
			"путь за пределами песочницы: "+path,
			"В --безопасный доступны лишь файлы внутри "+root,
		)
	}
	return nil
}

// CheckHost разрешает исходящий HTTP только к SafeHosts.
func CheckHost(rawURL string) Object {
	if !SafeMode {
		return nil
	}
	if len(SafeHosts) == 0 {
		return ErrorWithHint(
			currentCallToken,
			"сеть запретна в безопасном стане",
			"Укажите --разрешить-хост=example.com",
		)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		// Может быть просто host:port
		host := rawURL
		if h, _, e := net.SplitHostPort(rawURL); e == nil {
			host = h
		}
		return checkHostName(host)
	}
	host := u.Hostname()
	return checkHostName(host)
}

func checkHostName(host string) Object {
	host = strings.ToLower(host)
	for _, allowed := range SafeHosts {
		a := strings.ToLower(strings.TrimSpace(allowed))
		if a == "" {
			continue
		}
		if host == a || strings.HasSuffix(host, "."+a) {
			return nil
		}
	}
	return ErrorWithHint(
		currentCallToken,
		"хост «"+host+"» не в числе дозволенных",
		"Прибавь --разрешить-хост="+host,
	)
}
