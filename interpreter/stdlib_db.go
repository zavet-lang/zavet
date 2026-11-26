//go:build !js

package interpreter

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	_ "modernc.org/sqlite" // pure Go SQLite — без CGo
)

var (
	reSQLBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reSQLLineComment  = regexp.MustCompile(`--[^\r\n]*`)
	reSQLSingleQuote  = regexp.MustCompile(`'(?:''|[^'])*'`)
	reSQLUnsafeOp     = regexp.MustCompile(`(?i)\b(attach|detach|load_extension)\b`)
)

// checkSafeSQL запрещает в --безопасный SQL-операции, способные выйти
// за пределы песочницы файлов (ATTACH/DETACH DATABASE, load_extension).
func checkSafeSQL(sqlStr string) Object {
	if !SafeMode {
		return nil
	}
	cleaned := reSQLBlockComment.ReplaceAllString(sqlStr, " ")
	cleaned = reSQLLineComment.ReplaceAllString(cleaned, " ")
	cleaned = reSQLSingleQuote.ReplaceAllString(cleaned, "''")
	if m := reSQLUnsafeOp.FindString(cleaned); m != "" {
		return ErrorWithHint(
			getCurrentCallToken(),
			"SQL-операция «"+strings.ToUpper(m)+"» запретна в безопасном стане",
			"Запусти без --безопасный, коли доверяешь сему свитку.",
		)
	}
	return nil
}

// === Модуль "бд" ===
//
// Полноценная работа с реляционными базами данных (SQLite).
//
// Использование:
//   импорт бд из "бд"
//   конст соед = бд.открыть("данные.db")
//   соед.выполнить("CREATE TABLE ...")
//   соед.выполнить("INSERT INTO t VALUES (?, ?)", "Анна", 25)
//   конст ряды = соед.запрос("SELECT * FROM t WHERE x > ?", 18)
//   для ряд в ряды: вывод(ряд["имя"])

// dbExecutor — общий интерфейс для DB и Tx.
type dbExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
}

// objectsToArgs преобразует объекты Завета в значения для драйвера БД.
func objectsToArgs(args []Object) ([]any, *Error) {
	result := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case *Integer:
			result[i] = v.Value
		case *Float:
			result[i] = v.Value
		case *String:
			result[i] = v.Value
		case *Boolean:
			result[i] = v.Value
		case *Null:
			result[i] = nil
		default:
			return nil, ErrorWithHint(
				getCurrentCallToken(),
				fmt.Sprintf("параметр %d: вид %s не принимается (нужно целое, дробное, строка, булево или ничего)", i+1, translateType(string(a.Type()))),
				"",
			)
		}
	}
	return result, nil
}

// goValueToObject преобразует значение из БД в объект Завета.
func goValueToObject(v any) Object {
	if v == nil {
		return NULL
	}
	switch val := v.(type) {
	case int64:
		return NewInteger(val)
	case int:
		return NewInteger(int64(val))
	case float64:
		return &Float{Value: val}
	case string:
		return &String{Value: val}
	case []byte:
		return &String{Value: string(val)}
	case bool:
		if val {
			return TRUE
		}
		return FALSE
	default:
		return &String{Value: fmt.Sprintf("%v", val)}
	}
}

// === Основные операции ===

func dbDoExec(exec dbExecutor, args []Object) Object {
	if len(args) < 1 {
		return ErrorWithHint(getCurrentCallToken(), "выполнить потребен SQL-запрос", "выполнить(\"INSERT ...\", знач1, знач2)")
	}
	sqlStr, ok := args[0].(*String)
	if !ok {
		return ErrorWithHint(getCurrentCallToken(), "первое приношение должно быть строкой SQL", "")
	}
	if errObj := checkSafeSQL(sqlStr.Value); errObj != nil {
		return errObj
	}
	params, errArg := objectsToArgs(args[1:])
	if errArg != nil {
		return errArg
	}
	res, err := exec.Exec(sqlStr.Value, params...)
	if err != nil {
		return ErrorWithHint(getCurrentCallToken(), "грех SQL: "+err.Error(), "Вникни в строение запроса и приношения.")
	}
	insertID, _ := res.LastInsertId()
	affected, _ := res.RowsAffected()
	h := NewHash()
	h.Set(&String{Value: "вставлено_id"}, NewInteger(insertID))
	h.Set(&String{Value: "затронуто"}, NewInteger(affected))
	return h
}

func dbDoQuery(exec dbExecutor, args []Object) Object {
	if len(args) < 1 {
		return ErrorWithHint(getCurrentCallToken(), "запрос потребен с SQL", "")
	}
	sqlStr, ok := args[0].(*String)
	if !ok {
		return ErrorWithHint(getCurrentCallToken(), "первое приношение должно быть строкой SQL", "")
	}
	if errObj := checkSafeSQL(sqlStr.Value); errObj != nil {
		return errObj
	}
	params, errArg := objectsToArgs(args[1:])
	if errArg != nil {
		return errArg
	}
	rows, err := exec.Query(sqlStr.Value, params...)
	if err != nil {
		return ErrorWithHint(getCurrentCallToken(), "грех SQL: "+err.Error(), "")
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return ErrorWithHint(getCurrentCallToken(), "не смог прочесть колонки: "+err.Error(), "")
	}

	result := []Object{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return ErrorWithHint(getCurrentCallToken(), "не смог прочесть строку: "+err.Error(), "")
		}
		row := NewHash()
		for i, col := range cols {
			row.Set(&String{Value: col}, goValueToObject(values[i]))
		}
		result = append(result, row)
	}
	return &Array{Elements: result}
}

func dbDoQueryRow(exec dbExecutor, args []Object) Object {
	res := dbDoQuery(exec, args)
	if isError(res) {
		return res
	}
	arr := res.(*Array)
	if len(arr.Elements) == 0 {
		return NULL
	}
	return arr.Elements[0]
}

func dbDoScalar(exec dbExecutor, args []Object) Object {
	row := dbDoQueryRow(exec, args)
	if isError(row) {
		return row
	}
	if row == NULL {
		return NULL
	}
	hash := row.(*Hash)
	for _, k := range hash.Keys {
		if pair, ok := hash.Pairs[k]; ok {
			return pair.Value
		}
	}
	return NULL
}

// === Prepared Statements ===

func newPreparedStatement(db *sql.DB, sqlStr string) Object {
	if errObj := checkSafeSQL(sqlStr); errObj != nil {
		return errObj
	}
	stmt, err := db.Prepare(sqlStr)
	if err != nil {
		return ErrorWithHint(getCurrentCallToken(), "грех подготовки: "+err.Error(), "")
	}

	h := NewHash()
	h.Set(&String{Value: "__sql__"}, &String{Value: sqlStr})

	// выполнить(знач1, знач2, ...) — для INSERT/UPDATE/DELETE
	h.Set(&String{Value: "выполнить"}, &Builtin{Fn: func(args ...Object) Object {
		params, errArg := objectsToArgs(args)
		if errArg != nil {
			return errArg
		}
		res, err := stmt.Exec(params...)
		if err != nil {
			return ErrorWithHint(getCurrentCallToken(), "грех выполнения: "+err.Error(), "")
		}
		insertID, _ := res.LastInsertId()
		affected, _ := res.RowsAffected()
		rh := NewHash()
		rh.Set(&String{Value: "вставлено_id"}, NewInteger(insertID))
		rh.Set(&String{Value: "затронуто"}, NewInteger(affected))
		return rh
	}})

	// запрос(знач1, знач2, ...) — для SELECT
	h.Set(&String{Value: "запрос"}, &Builtin{Fn: func(args ...Object) Object {
		params, errArg := objectsToArgs(args)
		if errArg != nil {
			return errArg
		}
		rows, err := stmt.Query(params...)
		if err != nil {
			return ErrorWithHint(getCurrentCallToken(), "не смог выполнить запрос: "+err.Error(), "")
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return ErrorWithHint(getCurrentCallToken(), "грех колонок: "+err.Error(), "")
		}
		result := []Object{}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог прочесть: "+err.Error(), "")
			}
			row := NewHash()
			for i, col := range cols {
				row.Set(&String{Value: col}, goValueToObject(values[i]))
			}
			result = append(result, row)
		}
		return &Array{Elements: result}
	}})

	// закрыть() — освободить ресурсы
	h.Set(&String{Value: "закрыть"}, &Builtin{Fn: func(args ...Object) Object {
		stmt.Close()
		return NULL
	}})

	return h
}

// === Пакетная вставка ===

func dbBatchInsert(db *sql.DB, args []Object) Object {
	if len(args) < 3 {
		return ErrorWithHint(getCurrentCallToken(), "вставить_много(таблица, колонки, данные)", "вставить_много(\"люди\", [\"имя\", \"возраст\"], [[\"Анна\", 25], [\"Иван\", 30]])")
	}
	tableName, ok := args[0].(*String)
	if !ok {
		return ErrorWithHint(getCurrentCallToken(), "первое приношение — имя таблицы (строка)", "")
	}
	colsArr, ok := args[1].(*Array)
	if !ok {
		return ErrorWithHint(getCurrentCallToken(), "второе приношение — стадо имён колонок", "")
	}
	dataArr, ok := args[2].(*Array)
	if !ok {
		return ErrorWithHint(getCurrentCallToken(), "третье приношение — стадо стад значений", "")
	}

	cols := make([]string, len(colsArr.Elements))
	for i, c := range colsArr.Elements {
		s, ok := c.(*String)
		if !ok {
			return ErrorWithHint(getCurrentCallToken(), fmt.Sprintf("колонка %d должна быть строкой", i+1), "")
		}
		cols[i] = s.Value
	}

	placeholders := "(" + strings.Repeat("?, ", len(cols)-1) + "?)"
	sqlStr := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
		tableName.Value,
		strings.Join(cols, ", "),
		placeholders,
	)

	stmt, err := db.Prepare(sqlStr)
	if err != nil {
		return ErrorWithHint(getCurrentCallToken(), "грех подготовки: "+err.Error(), "")
	}
	defer stmt.Close()

	var totalAffected int64
	for rowIdx, rowObj := range dataArr.Elements {
		rowArr, ok := rowObj.(*Array)
		if !ok {
			return ErrorWithHint(getCurrentCallToken(), fmt.Sprintf("строка %d должна быть стадом", rowIdx+1), "")
		}
		params, errArg := objectsToArgs(rowArr.Elements)
		if errArg != nil {
			return errArg
		}
		res, err := stmt.Exec(params...)
		if err != nil {
			return ErrorWithHint(getCurrentCallToken(), fmt.Sprintf("грех вставки строки %d: %s", rowIdx+1, err.Error()), "")
		}
		n, _ := res.RowsAffected()
		totalAffected += n
	}

	h := NewHash()
	h.Set(&String{Value: "затронуто"}, NewInteger(totalAffected))
	return h
}

// === Интроспекция ===

func dbTables(db *sql.DB) Object {
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return ErrorWithHint(getCurrentCallToken(), "грех: "+err.Error(), "")
	}
	defer rows.Close()
	result := []Object{}
	for rows.Next() {
		var name string
		rows.Scan(&name)
		result = append(result, &String{Value: name})
	}
	return &Array{Elements: result}
}

func dbColumns(db *sql.DB, args []Object) Object {
	if len(args) != 1 {
		return ErrorWithHint(getCurrentCallToken(), "колонки(таблица) — укажи имя таблицы", "")
	}
	tableName, ok := args[0].(*String)
	if !ok {
		return ErrorWithHint(getCurrentCallToken(), "приношение должно быть строкой", "")
	}
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName.Value))
	if err != nil {
		return ErrorWithHint(getCurrentCallToken(), "грех: "+err.Error(), "")
	}
	defer rows.Close()
	result := []Object{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk)
		col := NewHash()
		col.Set(&String{Value: "имя"}, &String{Value: name})
		col.Set(&String{Value: "тип"}, &String{Value: colType})
		col.Set(&String{Value: "обязательное"}, nativeBoolToBoolean(notNull == 1))
		col.Set(&String{Value: "первичный_ключ"}, nativeBoolToBoolean(pk == 1))
		if dflt.Valid {
			col.Set(&String{Value: "по_умолчанию"}, &String{Value: dflt.String})
		} else {
			col.Set(&String{Value: "по_умолчанию"}, NULL)
		}
		result = append(result, col)
	}
	return &Array{Elements: result}
}

func nativeBoolToBoolean(b bool) *Boolean {
	if b {
		return TRUE
	}
	return FALSE
}

// === Миграции ===

func dbMigrate(db *sql.DB, args []Object) Object {
	// Создаём таблицу миграций если нет
	db.Exec("CREATE TABLE IF NOT EXISTS __миграции__ (версия INTEGER PRIMARY KEY, применена TEXT DEFAULT (datetime('now')))")

	if len(args) != 2 {
		return ErrorWithHint(getCurrentCallToken(), "мигрировать(версия, sql)", "мигрировать(1, \"CREATE TABLE ...\")")
	}
	versionObj, ok := args[0].(*Integer)
	if !ok {
		return ErrorWithHint(getCurrentCallToken(), "версия должна быть целым числом", "")
	}
	sqlStr, ok := args[1].(*String)
	if !ok {
		return ErrorWithHint(getCurrentCallToken(), "второе приношение — SQL-запрос (строка)", "")
	}
	if errObj := checkSafeSQL(sqlStr.Value); errObj != nil {
		return errObj
	}

	// Проверяем применена ли уже
	var count int
	row := db.QueryRow("SELECT COUNT(*) FROM __миграции__ WHERE версия = ?", versionObj.Value)
	row.Scan(&count)
	if count > 0 {
		return FALSE // уже применена
	}

	// Применяем
	_, err := db.Exec(sqlStr.Value)
	if err != nil {
		return ErrorWithHint(getCurrentCallToken(), fmt.Sprintf("грех миграции %d: %s", versionObj.Value, err.Error()), "")
	}
	db.Exec("INSERT INTO __миграции__ (версия) VALUES (?)", versionObj.Value)
	return TRUE // применена
}

func dbVersion(db *sql.DB) Object {
	db.Exec("CREATE TABLE IF NOT EXISTS __миграции__ (версия INTEGER PRIMARY KEY, применена TEXT DEFAULT (datetime('now')))")
	var version sql.NullInt64
	row := db.QueryRow("SELECT MAX(версия) FROM __миграции__")
	row.Scan(&version)
	if !version.Valid {
		return NewInteger(0)
	}
	return NewInteger(version.Int64)
}

// === Сборка соединения ===

func makeExecutorMethods(exec dbExecutor) *Hash {
	h := NewHash()
	h.Set(&String{Value: "выполнить"}, &Builtin{Fn: func(args ...Object) Object {
		return dbDoExec(exec, args)
	}})
	h.Set(&String{Value: "запрос"}, &Builtin{Fn: func(args ...Object) Object {
		return dbDoQuery(exec, args)
	}})
	h.Set(&String{Value: "строка"}, &Builtin{Fn: func(args ...Object) Object {
		return dbDoQueryRow(exec, args)
	}})
	h.Set(&String{Value: "значение"}, &Builtin{Fn: func(args ...Object) Object {
		return dbDoScalar(exec, args)
	}})
	return h
}

func newDBConnection(db *sql.DB, path string) *Hash {
	h := makeExecutorMethods(db)
	h.Set(&String{Value: "__путь__"}, &String{Value: path})

	// закрыть()
	closed := false
	h.Set(&String{Value: "закрыть"}, &Builtin{Fn: func(args ...Object) Object {
		if !closed {
			db.Close()
			closed = true
		}
		return NULL
	}})

	// транзакция(функция(тх) ...)
	h.Set(&String{Value: "транзакция"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) != 1 || (args[0].Type() != "FUNCTION" && args[0].Type() != "BUILTIN") {
			return ErrorWithHint(getCurrentCallToken(), "транзакция потребна одной функции", "транзакция(функция(тх) ... конец)")
		}
		tx, err := db.Begin()
		if err != nil {
			return ErrorWithHint(getCurrentCallToken(), "не смог начать транзакцию: "+err.Error(), "")
		}
		txHash := makeExecutorMethods(tx)
		result := ApplyFunctionCallback(args[0], []Object{txHash})
		if isError(result) {
			tx.Rollback()
			return result
		}
		if err := tx.Commit(); err != nil {
			return ErrorWithHint(getCurrentCallToken(), "не смог зафиксировать: "+err.Error(), "")
		}
		return result
	}})

	// подготовить(sql) → prepared statement
	h.Set(&String{Value: "подготовить"}, &Builtin{Fn: func(args ...Object) Object {
		if len(args) != 1 {
			return ErrorWithHint(getCurrentCallToken(), "подготовить(sql)", "")
		}
		s, ok := args[0].(*String)
		if !ok {
			return ErrorWithHint(getCurrentCallToken(), "приношение должно быть строкой SQL", "")
		}
		return newPreparedStatement(db, s.Value)
	}})

	// вставить_много(таблица, колонки, данные)
	h.Set(&String{Value: "вставить_много"}, &Builtin{Fn: func(args ...Object) Object {
		return dbBatchInsert(db, args)
	}})

	// таблицы() → массив имён таблиц
	h.Set(&String{Value: "таблицы"}, &Builtin{Fn: func(args ...Object) Object {
		return dbTables(db)
	}})

	// колонки(таблица) → массив описаний колонок
	h.Set(&String{Value: "колонки"}, &Builtin{Fn: func(args ...Object) Object {
		return dbColumns(db, args)
	}})

	// мигрировать(версия, sql) → да/нет (применена ли)
	h.Set(&String{Value: "мигрировать"}, &Builtin{Fn: func(args ...Object) Object {
		return dbMigrate(db, args)
	}})

	// версия() → текущая версия схемы
	h.Set(&String{Value: "версия"}, &Builtin{Fn: func(args ...Object) Object {
		return dbVersion(db)
	}})

	return h
}

func registerDBModule() {
	fns := map[string]func(args ...Object) Object{
		"открыть": func(args ...Object) Object {
			if len(args) != 1 {
				return ErrorWithHint(getCurrentCallToken(), "открыть потребен путь к файлу или ':memory:'", "бд.открыть(\"данные.db\")")
			}
			path, ok := args[0].(*String)
			if !ok {
				return ErrorWithHint(getCurrentCallToken(), "путь должен быть строкой", "")
			}
			if path.Value != ":memory:" {
				if SafeMode {
					trimmed := strings.ToLower(strings.TrimSpace(path.Value))
					if strings.HasPrefix(trimmed, "file:") || strings.Contains(path.Value, "?") {
						return ErrorWithHint(
							getCurrentCallToken(),
							"SQLite URI «"+path.Value+"» запретен в безопасном стане",
							"Указывай обычный путь внутри песочницы или ':memory:'.",
						)
					}
				}
				if err := CheckPath(path.Value); err != nil {
					return err
				}
			}
			db, err := sql.Open("sqlite", path.Value)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог открыть БД: "+err.Error(), "")
			}
			if err := db.Ping(); err != nil {
				db.Close()
				return ErrorWithHint(getCurrentCallToken(), "не смог подключиться: "+err.Error(), "")
			}
			// WAL mode для лучшей производительности при параллельных чтениях
			db.Exec("PRAGMA journal_mode=WAL")
			db.Exec("PRAGMA foreign_keys=ON")
			if SafeMode {
				db.Exec("PRAGMA trusted_schema=OFF")
			}
			return newDBConnection(db, path.Value)
		},
	}
	stdModules["бд"] = makeHashFromBuiltins(fns)
}
