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
const RegistryURL = "https://raw.githubusercontent.com/zavet-lang/zavet/main/registry/registry.json"

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

// DefaultRegistry возвращает встроенный каталог проверенных пакетов Завета.
// Используется как база и как резерв при отсутствии сети.
func DefaultRegistry() *Registry {
	return &Registry{
		Version: 2,
		Packages: map[string]*RegistryEntry{
			"валидатор": {URL: "https://github.com/zavet-lang/zavet-validator", Description: "Проверка email, URL, диапазонов, строк и схем словарей", Category: "данные"},
			"дата":      {URL: "https://github.com/zavet-lang/zavet-date", Description: "Форматирование дат на русском, склонение интервалов и календарь", Category: "время"},
			"тг":        {URL: "https://github.com/zavet-lang/zavet-tg", Description: "Клиент Telegram Bot API (опрос, команды, клавиатуры, вебхуки)", Category: "боты"},
			"цвета":     {URL: "https://github.com/zavet-lang/zavet-colors", Description: "ANSI-цвета, стили текста и таблицы в терминале", Category: "cli"},
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
