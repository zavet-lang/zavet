// Package bible загружает Синодальный перевод (1876, public domain)
// из data/bible/synodal.json и отдаёт стихи по книге/главе/номеру.
package bible

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type root struct {
	Name  string `json:"name"`
	Books []book `json:"books"`
}

type book struct {
	Book        string    `json:"book"`
	EnglishName string    `json:"englishName"`
	Chapters    []chapter `json:"chapters"`
}

type chapter struct {
	Chapter int     `json:"chapter"`
	Verses  []verse `json:"verses"`
}

type verse struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

// Писание — индекс Синодального перевода.
type Писание struct {
	Название string
	aliases  map[string]string                       // нормализованный алиас → OSIS
	display  map[string]string                       // OSIS → русское имя
	books    map[string]map[int]map[int]string       // OSIS → глава → стих → текст
}

var (
	once sync.Once
	inst *Писание
	loadErr error
)

// Глобальное возвращает загруженное Писание (ленивая загрузка).
func Глобальное() (*Писание, error) {
	once.Do(func() {
		inst, loadErr = загрузить()
	})
	return inst, loadErr
}

func загрузить() (*Писание, error) {
	for _, p := range кандидаты() {
		if data, err := os.ReadFile(p); err == nil {
			return изJSON(data)
		}
	}
	return nil, fmt.Errorf("не найден свиток data/bible/synodal.json (Синодальный перевод)")
}

func кандидаты() []string {
	var out []string
	if cwd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(cwd, "data", "bible", "synodal.json"))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		out = append(out,
			filepath.Join(dir, "data", "bible", "synodal.json"),
			filepath.Join(dir, "..", "data", "bible", "synodal.json"),
			filepath.Join(dir, "..", "..", "data", "bible", "synodal.json"),
		)
	}
	_, file, _, ok := runtime.Caller(0)
	if ok {
		out = append(out, filepath.Join(filepath.Dir(file), "..", "data", "bible", "synodal.json"))
	}
	return out
}

func изJSON(data []byte) (*Писание, error) {
	var r root
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("грех JSON Писания: %w", err)
	}
	п := &Писание{
		Название: r.Name,
		aliases:  map[string]string{},
		display:  map[string]string{},
		books:    map[string]map[int]map[int]string{},
	}
	for _, b := range r.Books {
		ru := русскоеИмя(b.Book)
		if ru == "" {
			ru = b.EnglishName
		}
		п.display[b.Book] = ru
		for _, a := range алиасы(b.Book, ru, b.EnglishName) {
			п.aliases[нормализовать(a)] = b.Book
		}
		chapters := map[int]map[int]string{}
		for _, ch := range b.Chapters {
			verses := map[int]string{}
			for _, v := range ch.Verses {
				verses[v.Number] = v.Text
			}
			chapters[ch.Chapter] = verses
		}
		п.books[b.Book] = chapters
	}
	return п, nil
}

// Стих возвращает «Книга глава:стих — текст».
func (п *Писание) Стих(книга string, глава, номер int64) (string, error) {
	текст, имя, err := п.найти(книга, глава, номер)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %d:%d — %s", имя, глава, номер, текст), nil
}

// Текст возвращает только текст стиха.
func (п *Писание) Текст(книга string, глава, номер int64) (string, error) {
	текст, _, err := п.найти(книга, глава, номер)
	return текст, err
}

func (п *Писание) найти(книга string, глава, номер int64) (string, string, error) {
	if глава <= 0 || номер <= 0 {
		return "", "", fmt.Errorf("глава и стих должны быть положительными")
	}
	osis, ok := п.aliases[нормализовать(книга)]
	if !ok {
		return "", "", fmt.Errorf("нет такой книги в Писании: «%s»", книга)
	}
	имя := п.display[osis]
	ch, ok := п.books[osis][int(глава)]
	if !ok {
		return "", "", fmt.Errorf("нет такой главы: %s %d", имя, глава)
	}
	текст, ok := ch[int(номер)]
	if !ok {
		return "", "", fmt.Errorf("нет такого стиха: %s %d:%d", имя, глава, номер)
	}
	return текст, имя, nil
}

func нормализовать(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "ё", "е")
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '.' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func русскоеИмя(osis string) string {
	m := map[string]string{
		"Gen": "Бытие", "Exod": "Исход", "Lev": "Левит", "Num": "Числа", "Deut": "Второзаконие",
		"Josh": "Иисус Навин", "Judg": "Судьи", "Ruth": "Руфь",
		"1Sam": "1 Царств", "2Sam": "2 Царств", "1Kgs": "3 Царств", "2Kgs": "4 Царств",
		"1Chr": "1 Паралипоменон", "2Chr": "2 Паралипоменон",
		"Ezra": "Ездра", "Neh": "Неемия", "Esth": "Есфирь", "Job": "Иов",
		"Ps": "Псалтирь", "Prov": "Притчи", "Eccl": "Екклесиаст", "Song": "Песнь Песней",
		"Isa": "Исаия", "Jer": "Иеремия", "Lam": "Плач Иеремии", "Ezek": "Иезекииль", "Dan": "Даниил",
		"Hos": "Осия", "Joel": "Иоиль", "Amos": "Амос", "Obad": "Авдий", "Jonah": "Иона",
		"Mic": "Михей", "Nah": "Наум", "Hab": "Аввакум", "Zeph": "Софония", "Hag": "Аггей",
		"Zech": "Захария", "Mal": "Малахия",
		"Matt": "Матфей", "Mark": "Марк", "Luke": "Лука", "John": "Иоанн", "Acts": "Деяния",
		"Rom": "Римлянам", "1Cor": "1 Коринфянам", "2Cor": "2 Коринфянам",
		"Gal": "Галатам", "Eph": "Ефесянам", "Phil": "Филиппийцам", "Col": "Колоссянам",
		"1Thess": "1 Фессалоникийцам", "2Thess": "2 Фессалоникийцам",
		"1Tim": "1 Тимофею", "2Tim": "2 Тимофею", "Titus": "Титу", "Phlm": "Филимону",
		"Heb": "Евреям", "Jas": "Иакова", "1Pet": "1 Петра", "2Pet": "2 Петра",
		"1John": "1 Иоанна", "2John": "2 Иоанна", "3John": "3 Иоанна", "Jude": "Иуды", "Rev": "Откровение",
	}
	return m[osis]
}

func алиасы(osis, ru, en string) []string {
	a := []string{osis, ru, en}
	extra := map[string][]string{
		"Gen":  {"Быт", "Бытие"},
		"Exod": {"Исх", "Исход"},
		"Ps":   {"Пс", "Псалом", "Псалмы"},
		"Matt": {"Мф", "Матф", "От Матфея"},
		"Mark": {"Мк", "От Марка"},
		"Luke": {"Лк", "От Луки"},
		"John": {"Ин", "Ин.", "Иоанн", "От Иоанна"},
		"Acts": {"Деян", "Деяния"},
		"Rom":  {"Рим", "Римлянам"},
		"Rev":  {"Откр", "Апокалипсис"},
		"Prov": {"Притч", "Притчи"},
		"Isa":  {"Ис", "Исаия"},
	}
	a = append(a, extra[osis]...)
	return a
}
