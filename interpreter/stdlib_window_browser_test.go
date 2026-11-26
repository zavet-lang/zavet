//go:build !windows && !js

package interpreter

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserWindowLifecycleAndDrawing(t *testing.T) {
	prevOpen := openBrowser
	openBrowser = func(string) error { return nil }
	defer func() {
		openBrowser = prevOpen
		browserCloseActive()
	}()

	// Валидация размеров
	if res := browserCreate(&Integer{Value: 10}, &Integer{Value: 100}); !isError(res) {
		t.Fatalf("ожидалась ошибка при ширине < 64")
	}
	if res := browserCreate(&Integer{Value: 200}, &Integer{Value: 9999}); !isError(res) {
		t.Fatalf("ожидалась ошибка при высоте > 4096")
	}
	if res := browserCreate(&String{Value: "bad"}, &Integer{Value: 100}); !isError(res) {
		t.Fatalf("ожидалась ошибка при неверном типе ширины")
	}

	// Рисование до создания окна — ошибка, без паники
	if res := browserRect(&Integer{Value: 0}, &Integer{Value: 0}, &Integer{Value: 10}, &Integer{Value: 10},
		&Integer{Value: 255}, &Integer{Value: 0}, &Integer{Value: 0}); !isError(res) {
		t.Fatalf("ожидалась ошибка при рисовании до создания окна")
	}

	// Создание окна
	if res := browserCreate(&Integer{Value: 160}, &Integer{Value: 120}, &String{Value: "Окно Тест"}); isError(res) {
		t.Fatalf("ошибка создания окна: %s", res.Inspect())
	}
	if browserFrame() != TRUE {
		t.Fatalf("после создания окно.кадр() должен возвращать TRUE")
	}

	// Очистка с выходом RGB за пределы (clamping)
	if res := browserClear(&Integer{Value: -10}, &Integer{Value: 300}, &Integer{Value: 15}); isError(res) {
		t.Fatalf("ошибка очистки: %s", res.Inspect())
	}

	// Неверные типы аргументов не паникуют
	if res := browserRect(&String{Value: "x"}, &Integer{Value: 0}, &Integer{Value: 10}, &Integer{Value: 10},
		&Integer{Value: 1}, &Integer{Value: 2}, &Integer{Value: 3}); !isError(res) {
		t.Fatalf("ожидалась ошибка при неверном типе аргумента в browserRect")
	}
	if res := browserText(&Integer{Value: 10}, &String{Value: "y"}, &String{Value: "Привет"},
		&Integer{Value: 255}, &Integer{Value: 255}, &Integer{Value: 255}); !isError(res) {
		t.Fatalf("ожидалась ошибка при неверном типе аргумента в browserText")
	}

	// Рисование прямоугольника, текста с кириллицей и иконки поверх
	if res := browserText(&Integer{Value: 10}, &Integer{Value: 10}, &String{Value: "Привет, Завет!"},
		&Integer{Value: 255}, &Integer{Value: 255}, &Integer{Value: 255}); isError(res) {
		t.Fatalf("ошибка browserText: %s", res.Inspect())
	}
	if res := browserRect(&Integer{Value: 8}, &Integer{Value: 8}, &Integer{Value: 60}, &Integer{Value: 20},
		&Integer{Value: 0}, &Integer{Value: 0}, &Integer{Value: 100}); isError(res) {
		t.Fatalf("ошибка browserRect: %s", res.Inspect())
	}
	if res := browserIcon(&Integer{Value: 100}, &Integer{Value: 10}, &String{Value: "house"},
		&Integer{Value: 255}, &Integer{Value: 200}, &Integer{Value: 0}); isError(res) {
		t.Fatalf("ошибка browserIcon: %s", res.Inspect())
	}
	if res := browserIcon(&Integer{Value: 100}, &Integer{Value: 10}, &String{Value: "no-such-icon"},
		&Integer{Value: 255}, &Integer{Value: 200}, &Integer{Value: 0}); !isError(res) {
		t.Fatalf("ожидалась ошибка при неизвестной иконке")
	}

	// Проверка /frame.png: валидный PNG 160x120 и текст наложен поверх синего прямоугольника
	rec := httptest.NewRecorder()
	bwin.servePNG(rec, httptest.NewRequest(http.MethodGet, "/frame.png", nil))
	if rec.Code != 200 {
		t.Fatalf("/frame.png вернул %d", rec.Code)
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	if img.Bounds().Dx() != 160 || img.Bounds().Dy() != 120 {
		t.Fatalf("ожидался 160x120, получено %v", img.Bounds())
	}
	// В точке (10,10) буква 'П' рисуется белым поверх синего прямоугольника (0,0,100)
	r, g, b, _ := img.At(10, 10).RGBA()
	if r>>8 != 255 || g>>8 != 255 || b>>8 != 255 {
		t.Fatalf("текст не наложился поверх прямоугольника: (%d,%d,%d)", r>>8, g>>8, b>>8)
	}

	// Проверка / и наличия beforeunload + e.code
	recHTML := httptest.NewRecorder()
	bwin.serveHTML(recHTML, httptest.NewRequest(http.MethodGet, "/", nil))
	htmlBody := recHTML.Body.String()
	if !strings.Contains(htmlBody, "beforeunload") || !strings.Contains(htmlBody, "sendBeacon('/close')") {
		t.Fatalf("HTML должен уведомлять /close при закрытии вкладки")
	}

	// Проверка /input (мышь, клики, колесо, символы, клавиши)
	payload, _ := json.Marshal(browserInputPayload{
		Keys:   []string{"a", "arrowleft"},
		MX:     42,
		MY:     77,
		Left:   true,
		LClick: true,
		Wheel:  2,
		Chars:  "аб",
	})
	recIn := httptest.NewRecorder()
	bwin.serveInput(recIn, httptest.NewRequest(http.MethodPost, "/input", bytes.NewReader(payload)))
	if recIn.Code != 200 {
		t.Fatalf("/input вернул %d", recIn.Code)
	}
	if browserMouseX().(*Integer).Value != 42 || browserMouseY().(*Integer).Value != 77 {
		t.Fatalf("координаты мыши не обновились")
	}
	if browserKey(&String{Value: "ф"}) != TRUE || browserKey(&String{Value: "влево"}) != TRUE {
		t.Fatalf("клавиши ф/влево не сработали")
	}
	if res := browserKey(&String{Value: "несуществующая_клавиша"}); !isError(res) {
		t.Fatalf("ожидалась ошибка на неизвестной клавише")
	}
	if browserClick() != TRUE || browserClick() != FALSE {
		t.Fatalf("одноразовый клик должен сбрасываться после чтения")
	}
	if browserWheel().(*Integer).Value != 2 || browserWheel().(*Integer).Value != 0 {
		t.Fatalf("колесо должно сбрасываться после чтения")
	}
	if browserChars().(*String).Value != "аб" || browserChars().(*String).Value != "" {
		t.Fatalf("буфер символов должен очищаться после чтения")
	}

	// Проверка задержка с неверным типом и clamp
	if res := browserDelay(&String{Value: "x"}); !isError(res) {
		t.Fatalf("ожидалась ошибка при неверном типе задержки")
	}
	if res := browserDelay(&Integer{Value: 0}); isError(res) {
		t.Fatalf("ошибка browserDelay(0): %s", res.Inspect())
	}

	// Закрытие окна
	browserClose()
	if browserFrame() != FALSE {
		t.Fatalf("после закрытия окно.кадр() должен возвращать FALSE")
	}
}
