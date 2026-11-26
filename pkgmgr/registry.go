package pkgmgr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// RegistryURL — URL официального реестра коротких имён пакетов Завета.
const RegistryURL = "https://raw.githubusercontent.com/zavet-lang/zavet-registry/main/registry.json"

// RegistryEntry представляет один пакет в реестре.
type RegistryEntry struct {
	URL         string `json:"url"`
	Description string `json:"описание"`
	Category    string `json:"категория,omitempty"`
}

// Registry — содержимое файла registry.json реестра.
type Registry struct {
	Version  int                       `json:"версия_реестра"`
	Packages map[string]*RegistryEntry `json:"пакеты"`
}

// DefaultRegistry возвращает встроенный каталог пакетов Завета.
// Используется как база и как резерв при отсутствии сети.
func DefaultRegistry() *Registry {
	return &Registry{
		Version: 2,
		Packages: map[string]*RegistryEntry{
			"веб":       {URL: "https://github.com/zavet-lang/zavet-web", Description: "Веб-фреймворк с маршрутизацией, группами, middleware и шаблонами", Category: "веб"},
			"валидатор": {URL: "https://github.com/zavet-lang/zavet-validator", Description: "Проверка схем данных, email, URL, диапазонов и форм", Category: "данные"},
			"логгер":    {URL: "https://github.com/zavet-lang/zavet-logger", Description: "Структурированное логирование с уровнями и JSON-выводом", Category: "инструменты"},
			"конфиг":    {URL: "https://github.com/zavet-lang/zavet-config", Description: "Чтение конфигурации из .env, JSON и YAML", Category: "инструменты"},
			"кэш":       {URL: "https://github.com/zavet-lang/zavet-cache", Description: "In-memory и LRU-кэш с TTL", Category: "данные"},
			"jwt":       {URL: "https://github.com/zavet-lang/zavet-jwt", Description: "Утилиты аутентификации и middleware на базе JWT", Category: "безопасность"},
			"почта":     {URL: "https://github.com/zavet-lang/zavet-mail", Description: "Шаблоны писем и очередь рассылки поверх модуля почта", Category: "сеть"},
			"маркдаун":  {URL: "https://github.com/zavet-lang/zavet-markdown", Description: "Преобразование Markdown в HTML и оглавление", Category: "текст"},
			"uuid":      {URL: "https://github.com/zavet-lang/zavet-uuid", Description: "Генерация и проверка UUID v4/v7", Category: "данные"},
			"цвета":     {URL: "https://github.com/zavet-lang/zavet-colors", Description: "ANSI-цвета и таблицы в терминале", Category: "cli"},
			"миграции":  {URL: "https://github.com/zavet-lang/zavet-migrations", Description: "Управление миграциями SQLite и PostgreSQL", Category: "бд"},
			"тесты":     {URL: "https://github.com/zavet-lang/zavet-assert", Description: "Расширенные проверки, моки и отчёты для тестов", Category: "тестирование"},
			"дата":      {URL: "https://github.com/zavet-lang/zavet-date", Description: "Форматирование дат на русском, интервалы и часовые пояса", Category: "время"},
			"очередь":   {URL: "https://github.com/zavet-lang/zavet-queue", Description: "Асинхронная очередь задач и планировщик cron", Category: "асинхронность"},
			"тг":        {URL: "https://github.com/zavet-lang/zavet-tg", Description: "Клиент Telegram Bot API (команды, клавиатуры, вебхуки)", Category: "боты"},
		},
	}
}

// FetchRegistry загружает реестр коротких имён с GitHub и объединяет с
// встроенным каталогом. Если сеть недоступна — возвращает встроенный каталог.
func FetchRegistry() (*Registry, error) {
	base := DefaultRegistry()

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", RegistryURL, nil)
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	resp, err := client.Do(req)
	if err != nil {
		return base, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return base, nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return base, fmt.Errorf("не смог прочесть ответ реестра: %w", err)
	}

	var remote Registry
	if err := json.Unmarshal(data, &remote); err != nil {
		return base, nil
	}
	for name, entry := range remote.Packages {
		base.Packages[name] = entry
	}
	return base, nil
}

// Search ищет пакеты в реестре по имени, описанию или категории.
// Пустой запрос возвращает все пакеты в алфавитном порядке.
func (r *Registry) Search(query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	var names []string
	for name, entry := range r.Packages {
		if q == "" ||
			strings.Contains(strings.ToLower(name), q) ||
			strings.Contains(strings.ToLower(entry.Description), q) ||
			strings.Contains(strings.ToLower(entry.Category), q) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// ResolveName ищет короткое имя в реестре. Возвращает URL пакета
// или пустую строку, если имя не найдено.
func (r *Registry) ResolveName(name string) string {
	if r.Packages == nil {
		return ""
	}
	if entry, ok := r.Packages[name]; ok {
		return entry.URL
	}
	return ""
}

// IsShortName определяет, выглядит ли строка как короткое имя
// (не URL): нет слешей, точек, не начинается с http.
func IsShortName(s string) bool {
	if i := strings.LastIndex(s, "@"); i > 0 {
		s = s[:i]
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return false
	}
	if strings.Contains(s, "/") {
		return false
	}
	if strings.Contains(s, ".") {
		return false
	}
	return s != ""
}
