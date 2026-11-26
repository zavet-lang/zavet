package interpreter

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
)

func init() {
	registerEmailModule()
}

// registerEmailModule регистрирует стандартный модуль «почта» (SMTP / MIME).
func registerEmailModule() {
	fns := map[string]func(args ...Object) Object{
		// почта.проверить_адрес(адрес) -> истина/ложь
		"проверить_адрес": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("почта.проверить_адрес", 1, len(args))
			}
			s, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("почта.проверить_адрес", 1, "строка", args[0].Type())
			}
			addr, err := mail.ParseAddress(strings.TrimSpace(s.Value))
			if err != nil || !strings.Contains(addr.Address, "@") {
				return FALSE
			}
			parts := strings.Split(addr.Address, "@")
			if len(parts) != 2 || parts[0] == "" || !strings.Contains(parts[1], ".") {
				return FALSE
			}
			return TRUE
		},

		// почта.собрать(от, кому, тема, тело, [html]) -> строка RFC 5322
		"собрать": func(args ...Object) Object {
			if len(args) < 4 || len(args) > 5 {
				return ErrorWithHint(getCurrentCallToken(),
					"почта.собрать(от, кому, тема, тело, [html])",
					"Употребляй: почта.собрать(\"a@b.ru\", \"c@d.ru\", \"Тема\", \"Текст\")")
			}
			from, ok1 := args[0].(*String)
			to, ok2 := args[1].(*String)
			subj, ok3 := args[2].(*String)
			body, ok4 := args[3].(*String)
			if !ok1 || !ok2 || !ok3 || !ok4 {
				return ErrorWithHint(getCurrentCallToken(), "все приношения должны быть строками", "")
			}
			isHTML := false
			if len(args) == 5 {
				isHTML = isTruthy(args[4])
			}
			return &String{Value: buildEmailMIME(from.Value, to.Value, subj.Value, body.Value, isHTML)}
		},

		// почта.отправить(параметры_словарь) -> истина
		// Ключи: хост, порт (по умолчанию 587), пользователь, пароль, от, кому, тема, тело, html (опц.), tls (опц.)
		"отправить": func(args ...Object) Object {
			if len(args) != 1 {
				return ErrorWithHint(getCurrentCallToken(),
					"почта.отправить(настройки)",
					"Принеси словарь с ключами: хост, порт, от, кому, тема, тело (и опц. пользователь, пароль, html, tls).")
			}
			cfg, ok := args[0].(*Hash)
			if !ok {
				return builtinErrorWrongArgType("почта.отправить", 1, "словарь", args[0].Type())
			}

			getStr := func(k string) string {
				if v, ok := hashGetString(cfg, k); ok {
					return v
				}
				return ""
			}
			host := getStr("хост")
			if host == "" {
				return ErrorWithHint(getCurrentCallToken(), "в настройках почты не указан «хост»", "")
			}
			if err := CheckHost("smtp://" + host); err != nil {
				return err
			}

			port := int64(587)
			if pObj, ok := hashGetObj(cfg, "порт"); ok {
				if pi, ok := pObj.(*Integer); ok {
					port = pi.Value
				}
			}
			user := getStr("пользователь")
			pass := getStr("пароль")
			from := getStr("от")
			if from == "" {
				from = user
			}
			toStr := getStr("кому")
			if from == "" || toStr == "" {
				return ErrorWithHint(getCurrentCallToken(), "в настройках почты потребны поля «от» и «кому»", "")
			}
			subj := getStr("тема")
			body := getStr("тело")
			isHTML := false
			if hObj, ok := hashGetObj(cfg, "html"); ok {
				isHTML = isTruthy(hObj)
			}
			useImplicitTLS := port == 465
			if tObj, ok := hashGetObj(cfg, "tls"); ok {
				useImplicitTLS = isTruthy(tObj)
			}

			recipients := []string{}
			for _, r := range strings.Split(toStr, ",") {
				if r = strings.TrimSpace(r); r != "" {
					recipients = append(recipients, r)
				}
			}

			msg := []byte(buildEmailMIME(from, toStr, subj, body, isHTML))
			addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

			var auth smtp.Auth
			if user != "" {
				auth = smtp.PlainAuth("", user, pass, host)
			}

			if useImplicitTLS {
				conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
				if err != nil {
					return ErrorWithHint(getCurrentCallToken(), "не смог подключиться по TLS к SMTP: "+err.Error(), "")
				}
				client, err := smtp.NewClient(conn, host)
				if err != nil {
					conn.Close()
					return ErrorWithHint(getCurrentCallToken(), "грех SMTP: "+err.Error(), "")
				}
				defer client.Close()
				if auth != nil {
					if err := client.Auth(auth); err != nil {
						return ErrorWithHint(getCurrentCallToken(), "грех аутентификации SMTP: "+err.Error(), "")
					}
				}
				if err := client.Mail(from); err != nil {
					return ErrorWithHint(getCurrentCallToken(), "грех SMTP MAIL: "+err.Error(), "")
				}
				for _, rcpt := range recipients {
					if err := client.Rcpt(rcpt); err != nil {
						return ErrorWithHint(getCurrentCallToken(), "грех SMTP RCPT: "+err.Error(), "")
					}
				}
				w, err := client.Data()
				if err != nil {
					return ErrorWithHint(getCurrentCallToken(), "грех SMTP DATA: "+err.Error(), "")
				}
				if _, err := w.Write(msg); err != nil {
					return ErrorWithHint(getCurrentCallToken(), "не смог отправить письмо: "+err.Error(), "")
				}
				if err := w.Close(); err != nil {
					return ErrorWithHint(getCurrentCallToken(), "не смог завершить отправку письма: "+err.Error(), "")
				}
				_ = client.Quit()
				return TRUE
			}

			if err := smtp.SendMail(addr, auth, from, recipients, msg); err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог отправить письмо: "+err.Error(), "Вникни в хост, порт и учётные данные SMTP.")
			}
			return TRUE
		},
	}

	stdModules["почта"] = makeHashFromBuiltins(fns)
}

func buildEmailMIME(from, to, subject, body string, isHTML bool) string {
	ct := "text/plain; charset=UTF-8"
	if isHTML {
		ct = "text/html; charset=UTF-8"
	}
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: " + ct + "\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return sb.String()
}

func hashGetObj(h *Hash, key string) (Object, bool) {
	k := &String{Value: key}
	if pair, ok := h.Pairs[k.HashKey()]; ok {
		return pair.Value, true
	}
	return nil, false
}

func hashGetString(h *Hash, key string) (string, bool) {
	if obj, ok := hashGetObj(h, key); ok {
		if s, ok := obj.(*String); ok {
			return s.Value, true
		}
	}
	return "", false
}
