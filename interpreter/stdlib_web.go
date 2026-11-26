package interpreter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func init() {
	registerWebFrameworkModule()
}

type webRoute struct {
	method      string
	pattern     string
	segments    []string
	handler     Object
	middlewares []Object
}

type webApp struct {
	routes      []webRoute
	middlewares []Object
	errorFn     Object
	statics     map[string]string
}

func registerWebFrameworkModule() {
	fns := map[string]func(args ...Object) Object{
		"приложение": func(args ...Object) Object {
			app := &webApp{statics: make(map[string]string)}
			return app.toObject("")
		},

		// веб.текст(строка, [статус=200])
		"текст": func(args ...Object) Object {
			if len(args) < 1 {
				return ErrorWithHint(getCurrentCallToken(), "веб.текст(строка, [статус])", "")
			}
			status := int64(200)
			if len(args) >= 2 {
				if s, ok := args[1].(*Integer); ok {
					status = s.Value
				}
			}
			return makeWebResponse(status, "text/plain; charset=utf-8", args[0].Inspect())
		},

		// веб.html(html, [статус=200])
		"html": func(args ...Object) Object {
			if len(args) < 1 {
				return ErrorWithHint(getCurrentCallToken(), "веб.html(html, [статус])", "")
			}
			status := int64(200)
			if len(args) >= 2 {
				if s, ok := args[1].(*Integer); ok {
					status = s.Value
				}
			}
			return makeWebResponse(status, "text/html; charset=utf-8", args[0].Inspect())
		},

		// веб.json(данные, [статус=200])
		"json": func(args ...Object) Object {
			if len(args) < 1 {
				return ErrorWithHint(getCurrentCallToken(), "веб.json(данные, [статус])", "")
			}
			status := int64(200)
			if len(args) >= 2 {
				if s, ok := args[1].(*Integer); ok {
					status = s.Value
				}
			}
			return makeWebResponse(status, "application/json; charset=utf-8", objectToJSON(args[0]))
		},

		// веб.редирект(url, [статус=302])
		"редирект": func(args ...Object) Object {
			if len(args) < 1 {
				return ErrorWithHint(getCurrentCallToken(), "веб.редирект(url, [статус])", "")
			}
			status := int64(302)
			if len(args) >= 2 {
				if s, ok := args[1].(*Integer); ok {
					status = s.Value
				}
			}
			resp := makeWebResponse(status, "text/plain; charset=utf-8", "")
			hdrs := getOrCreateHeaders(resp)
			hdrs.Set(&String{Value: "Location"}, &String{Value: args[0].Inspect()})
			return resp
		},

		// веб.ошибка(статус, сообщение)
		"ошибка": func(args ...Object) Object {
			if len(args) < 2 {
				return ErrorWithHint(getCurrentCallToken(), "веб.ошибка(статус, сообщение)", "")
			}
			status := int64(500)
			if s, ok := args[0].(*Integer); ok {
				status = s.Value
			}
			payload := NewHash()
			payload.Set(&String{Value: "ошибка"}, &String{Value: args[1].Inspect()})
			payload.Set(&String{Value: "статус"}, NewInteger(status))
			return makeWebResponse(status, "application/json; charset=utf-8", objectToJSON(payload))
		},

		// веб.кука(ответ, имя, значение, [опции_словарь]) -> ответ
		"кука": func(args ...Object) Object {
			if len(args) < 3 {
				return ErrorWithHint(getCurrentCallToken(), "веб.кука(ответ, имя, значение, [опции])", "")
			}
			resp, ok := args[0].(*Hash)
			if !ok {
				return builtinErrorWrongArgType("веб.кука", 1, "словарь (ответ)", args[0].Type())
			}
			cookieStr := fmt.Sprintf("%s=%s; Path=/", args[1].Inspect(), args[2].Inspect())
			if len(args) >= 4 {
				if opts, ok := args[3].(*Hash); ok {
					if v, ok := hashGetObj(opts, "http_only"); ok && isTruthy(v) {
						cookieStr += "; HttpOnly"
					}
					if v, ok := hashGetObj(opts, "secure"); ok && isTruthy(v) {
						cookieStr += "; Secure"
					}
					if v, ok := hashGetObj(opts, "max_age"); ok {
						cookieStr += "; Max-Age=" + v.Inspect()
					}
				}
			}
			hdrs := getOrCreateHeaders(resp)
			hdrs.Set(&String{Value: "Set-Cookie"}, &String{Value: cookieStr})
			return resp
		},

		// веб.cors([источник="*"]) -> middleware
		"cors": func(args ...Object) Object {
			origin := "*"
			if len(args) >= 1 {
				origin = args[0].Inspect()
			}
			return &Builtin{Fn: func(mArgs ...Object) Object {
				if len(mArgs) < 2 {
					return NULL
				}
				next := mArgs[1]
				res := ApplyFunctionCallback(next, []Object{})
				if h, ok := res.(*Hash); ok {
					hdrs := getOrCreateHeaders(h)
					hdrs.Set(&String{Value: "Access-Control-Allow-Origin"}, &String{Value: origin})
					hdrs.Set(&String{Value: "Access-Control-Allow-Methods"}, &String{Value: "GET, POST, PUT, PATCH, DELETE, OPTIONS"})
					hdrs.Set(&String{Value: "Access-Control-Allow-Headers"}, &String{Value: "Content-Type, Authorization"})
					return h
				}
				return res
			}}
		},

		// веб.логгер() -> middleware
		"логгер": func(args ...Object) Object {
			return &Builtin{Fn: func(mArgs ...Object) Object {
				if len(mArgs) >= 1 {
					if req, ok := mArgs[0].(*Hash); ok {
						m, _ := hashGetString(req, "метод")
						p, _ := hashGetString(req, "путь")
						fmt.Fprintf(OutputWriter, "[веб] %s %s\n", m, p)
					}
				}
				if len(mArgs) >= 2 {
					return ApplyFunctionCallback(mArgs[1], []Object{})
				}
				return NULL
			}}
		},
	}

	stdModules["веб"] = makeHashFromBuiltins(fns)
}

func makeWebResponse(status int64, contentType, body string) *Hash {
	h := NewHash()
	h.Set(&String{Value: "статус"}, NewInteger(status))
	h.Set(&String{Value: "тело"}, &String{Value: body})
	hdrs := NewHash()
	hdrs.Set(&String{Value: "Content-Type"}, &String{Value: contentType})
	h.Set(&String{Value: "заголовки"}, hdrs)
	return h
}

func getOrCreateHeaders(resp *Hash) *Hash {
	if existing, ok := hashGetObj(resp, "заголовки"); ok {
		if h, ok := existing.(*Hash); ok {
			return h
		}
	}
	h := NewHash()
	resp.Set(&String{Value: "заголовки"}, h)
	return h
}

func joinWebPrefix(prefix, path string) string {
	if prefix == "" {
		if !strings.HasPrefix(path, "/") {
			return "/" + path
		}
		return path
	}
	p := strings.TrimRight(prefix, "/")
	if path == "" || path == "/" {
		return p
	}
	if !strings.HasPrefix(path, "/") {
		return p + "/" + path
	}
	return p + path
}

func matchWebRoute(segments []string, reqPath string) (map[string]string, bool) {
	clean := strings.Trim(reqPath, "/")
	var parts []string
	if clean != "" {
		parts = strings.Split(clean, "/")
	}
	params := map[string]string{}
	for i, seg := range segments {
		if strings.HasPrefix(seg, "*") {
			name := strings.TrimPrefix(seg, "*")
			if name == "" {
				name = "путь"
			}
			if i <= len(parts) {
				params[name] = strings.Join(parts[i:], "/")
			}
			return params, true
		}
		if i >= len(parts) {
			return nil, false
		}
		if strings.HasPrefix(seg, ":") {
			params[strings.TrimPrefix(seg, ":")] = parts[i]
		} else if seg != parts[i] {
			return nil, false
		}
	}
	if len(parts) != len(segments) {
		return nil, false
	}
	return params, true
}

func splitPattern(pattern string) []string {
	clean := strings.Trim(pattern, "/")
	if clean == "" {
		return nil
	}
	return strings.Split(clean, "/")
}

func (app *webApp) toObject(prefix string, groupMW ...Object) *Hash {
	h := NewHash()
	localMW := append([]Object{}, groupMW...)

	addRoute := func(method, path string, handler Object) {
		full := joinWebPrefix(prefix, path)
		app.routes = append(app.routes, webRoute{
			method:      strings.ToUpper(method),
			pattern:     full,
			segments:    splitPattern(full),
			handler:     handler,
			middlewares: append([]Object{}, localMW...),
		})
	}

	methods := map[string]string{
		"получить": "GET",
		"пост":     "POST",
		"положить": "PUT",
		"удалить":  "DELETE",
		"патч":     "PATCH",
	}
	for ruName, httpMethod := range methods {
		m := httpMethod
		name := ruName
		h.Set(&String{Value: name}, &Builtin{Fn: func(args ...Object) Object {
			if len(args) != 2 {
				return ErrorWithHint(getCurrentCallToken(), name+"(путь, обработчик)", "")
			}
			p, ok := args[0].(*String)
			if !ok {
				return ErrorWithHint(getCurrentCallToken(), "путь должен быть строкой", "")
			}
			addRoute(m, p.Value, args[1])
			return h
		}})
	}

	h.Set(&String{Value: "маршрут"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) != 3 {
			return ErrorWithHint(getCurrentCallToken(), "маршрут(метод, путь, обработчик)", "")
		}
		m, ok1 := args[0].(*String)
		p, ok2 := args[1].(*String)
		if !ok1 || !ok2 {
			return ErrorWithHint(getCurrentCallToken(), "метод и путь должны быть строками", "")
		}
		addRoute(m.Value, p.Value, args[2])
		return h
	}})

	h.Set(&String{Value: "группа"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) != 1 {
			return ErrorWithHint(getCurrentCallToken(), "группа(префикс)", "")
		}
		p, ok := args[0].(*String)
		if !ok {
			return ErrorWithHint(getCurrentCallToken(), "префикс должен быть строкой", "")
		}
		return app.toObject(joinWebPrefix(prefix, p.Value), localMW...)
	}})

	h.Set(&String{Value: "использовать"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) != 1 {
			return ErrorWithHint(getCurrentCallToken(), "использовать(middleware)", "")
		}
		if prefix == "" {
			app.middlewares = append(app.middlewares, args[0])
		} else {
			localMW = append(localMW, args[0])
		}
		return h
	}})

	h.Set(&String{Value: "при_ошибке"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) != 1 {
			return ErrorWithHint(getCurrentCallToken(), "при_ошибке(обработчик)", "")
		}
		app.errorFn = args[0]
		return h
	}})

	h.Set(&String{Value: "статика"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) != 2 {
			return ErrorWithHint(getCurrentCallToken(), "статика(url_префикс, папка)", "")
		}
		u, ok1 := args[0].(*String)
		dir, ok2 := args[1].(*String)
		if !ok1 || !ok2 {
			return ErrorWithHint(getCurrentCallToken(), "оба приношения должны быть строками", "")
		}
		app.statics[u.Value] = dir.Value
		return h
	}})

	// обработать(метод, путь, [тело], [заголовки]) -> словарь ответа (для тестов без сети)
	h.Set(&String{Value: "обработать"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) < 2 {
			return ErrorWithHint(getCurrentCallToken(), "обработать(метод, путь, [тело], [заголовки])", "")
		}
		method := strings.ToUpper(args[0].Inspect())
		rawURL := args[1].Inspect()
		body := ""
		if len(args) >= 3 && args[2] != NULL {
			body = args[2].Inspect()
		}
		headers := map[string]string{}
		if len(args) >= 4 {
			if hh, ok := args[3].(*Hash); ok {
				for _, p := range hh.orderedPairs() {
					headers[p.Key.Inspect()] = p.Value.Inspect()
				}
			}
		}
		return app.dispatch(method, rawURL, body, headers)
	}})

	h.Set(&String{Value: "запустить"}, &Builtin{Fn: func(args ...Object) Object {
		if err := denyIfSafe("веб.запустить"); err != nil {
			return err
		}
		if len(args) < 1 {
			return ErrorWithHint(getCurrentCallToken(), "запустить(порт)", "")
		}
		portObj, ok := args[0].(*Integer)
		if !ok {
			return ErrorWithHint(getCurrentCallToken(), "порт должен быть целым числом", "")
		}
		mux := http.NewServeMux()
		for uPrefix, dir := range app.statics {
			p := uPrefix
			if !strings.HasSuffix(p, "/") {
				p += "/"
			}
			mux.Handle(p, http.StripPrefix(p, http.FileServer(http.Dir(dir))))
		}
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			hdrs := map[string]string{}
			for k, vals := range r.Header {
				if len(vals) > 0 {
					hdrs[k] = vals[0]
				}
			}
			resp := app.dispatch(r.Method, r.URL.RequestURI(), string(bodyBytes), hdrs)
			writeWebHTTPResponse(w, resp)
		})
		addr := fmt.Sprintf(":%d", portObj.Value)
		fmt.Fprintf(OutputWriter, "Веб-сервер Завета запущен на http://localhost%s\n", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			return ErrorWithHint(getCurrentCallToken(), "грех сервера: "+err.Error(), "")
		}
		return NULL
	}})

	return h
}

func (app *webApp) dispatch(method, rawURL, body string, headers map[string]string) *Hash {
	parsedURL, _ := url.Parse(rawURL)
	reqPath := rawURL
	query := url.Values{}
	if parsedURL != nil {
		reqPath = parsedURL.Path
		query = parsedURL.Query()
	}

	var matched *webRoute
	var pathParams map[string]string
	for i := range app.routes {
		rt := &app.routes[i]
		if rt.method == method || rt.method == "*" {
			if params, ok := matchWebRoute(rt.segments, reqPath); ok {
				matched = rt
				pathParams = params
				break
			}
		}
	}

	req := buildWebRequest(method, rawURL, reqPath, body, headers, query, pathParams)

	var finalHandler func() Object
	if matched == nil {
		finalHandler = func() Object {
			return makeWebResponse(404, "application/json; charset=utf-8", `{"ошибка":"не найдено","статус":404}`)
		}
	} else {
		handler := matched.handler
		finalHandler = func() Object {
			return ApplyFunctionCallback(handler, []Object{req})
		}
	}

	// Собираем цепочку middleware: глобальные -> групповые -> обработчик
	chain := append([]Object{}, app.middlewares...)
	if matched != nil {
		chain = append(chain, matched.middlewares...)
	}

	run := finalHandler
	for i := len(chain) - 1; i >= 0; i-- {
		mw := chain[i]
		nextFn := run
		run = func() Object {
			nextObj := &Builtin{Fn: func(args ...Object) Object {
				return nextFn()
			}}
			// Если middleware принимает 2 аргумента (запрос, далее) — оборачивает вызов.
			// Если 1 аргумент (запрос) — выполняется до обработчика; непустой ответ прерывает цепочку.
			if fn, ok := mw.(*Function); ok && len(fn.Parameters) == 1 {
				res := ApplyFunctionCallback(mw, []Object{req})
				if isError(res) {
					return res
				}
				if res != NULL && res != TRUE {
					return res
				}
				return nextFn()
			}
			return ApplyFunctionCallback(mw, []Object{req, nextObj})
		}
	}

	res := run()
	if isError(res) {
		if app.errorFn != nil {
			errVal := &String{Value: res.(*Error).Message}
			res = ApplyFunctionCallback(app.errorFn, []Object{errVal, req})
		} else {
			errObj := res.(*Error)
			msg := errObj.RawMessage
			if msg == "" {
				msg = errObj.Message
			}
			return makeWebResponse(500, "application/json; charset=utf-8",
				objectToJSON(makeErrorPayload(500, msg)))
		}
	}
	return normalizeWebResponse(res)
}

func makeErrorPayload(status int64, msg string) *Hash {
	h := NewHash()
	h.Set(&String{Value: "статус"}, NewInteger(status))
	h.Set(&String{Value: "ошибка"}, &String{Value: msg})
	return h
}

func buildWebRequest(method, rawURL, path, body string, headers map[string]string, query url.Values, params map[string]string) *Hash {
	req := NewHash()
	req.Set(&String{Value: "метод"}, &String{Value: method})
	req.Set(&String{Value: "путь"}, &String{Value: path})
	req.Set(&String{Value: "url"}, &String{Value: rawURL})
	req.Set(&String{Value: "тело"}, &String{Value: body})

	pHash := NewHash()
	for k, v := range params {
		pHash.Set(&String{Value: k}, &String{Value: v})
	}
	req.Set(&String{Value: "параметры"}, pHash)

	qHash := NewHash()
	for k, vals := range query {
		if len(vals) > 0 {
			qHash.Set(&String{Value: k}, &String{Value: vals[0]})
		}
	}
	req.Set(&String{Value: "запрос"}, qHash)

	hHash := NewHash()
	for k, v := range headers {
		hHash.Set(&String{Value: k}, &String{Value: v})
	}
	req.Set(&String{Value: "заголовки"}, hHash)

	// Удобные методы на запросе
	req.Set(&String{Value: "парам"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) < 1 {
			return NULL
		}
		if v, ok := params[args[0].Inspect()]; ok {
			return &String{Value: v}
		}
		if len(args) >= 2 {
			return args[1]
		}
		return NULL
	}})
	req.Set(&String{Value: "арг"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) < 1 {
			return NULL
		}
		if vals := query[args[0].Inspect()]; len(vals) > 0 {
			return &String{Value: vals[0]}
		}
		if len(args) >= 2 {
			return args[1]
		}
		return NULL
	}})

	if strings.TrimSpace(body) != "" {
		var parsed any
		if err := json.Unmarshal([]byte(body), &parsed); err == nil {
			req.Set(&String{Value: "json"}, nativeToObject(parsed))
		} else {
			req.Set(&String{Value: "json"}, NULL)
		}
	} else {
		req.Set(&String{Value: "json"}, NULL)
	}
	return req
}

func normalizeWebResponse(res Object) *Hash {
	switch v := res.(type) {
	case *Hash:
		if _, ok := hashGetObj(v, "статус"); ok {
			return v
		}
		return makeWebResponse(200, "application/json; charset=utf-8", objectToJSON(v))
	case *String:
		return makeWebResponse(200, "text/html; charset=utf-8", v.Value)
	case *Null:
		return makeWebResponse(204, "text/plain; charset=utf-8", "")
	default:
		return makeWebResponse(200, "text/plain; charset=utf-8", res.Inspect())
	}
}

func writeWebHTTPResponse(w http.ResponseWriter, resp *Hash) {
	status := 200
	if sObj, ok := hashGetObj(resp, "статус"); ok {
		if si, ok := sObj.(*Integer); ok {
			status = int(si.Value)
		}
	}
	if hObj, ok := hashGetObj(resp, "заголовки"); ok {
		if hh, ok := hObj.(*Hash); ok {
			for _, p := range hh.orderedPairs() {
				w.Header().Set(p.Key.Inspect(), p.Value.Inspect())
			}
		}
	}
	w.WriteHeader(status)
	if bObj, ok := hashGetObj(resp, "тело"); ok {
		_, _ = w.Write([]byte(bObj.Inspect()))
	}
}
