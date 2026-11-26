//go:build windows

package interpreter

import (
	"fmt"
	"sort"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procUpdateWindow        = user32.NewProc("UpdateWindow")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procValidateRect     = user32.NewProc("ValidateRect")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
	procSetWindowTextW      = user32.NewProc("SetWindowTextW")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procScreenToClient      = user32.NewProc("ScreenToClient")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	procAddFontResourceExW    = gdi32.NewProc("AddFontResourceExW")
	procRemoveFontResourceExW = gdi32.NewProc("RemoveFontResourceExW")

	procStretchDIBits = gdi32.NewProc("StretchDIBits")
	procSetTextColor  = gdi32.NewProc("SetTextColor")
	procSetBkMode     = gdi32.NewProc("SetBkMode")
	procTextOutW      = gdi32.NewProc("TextOutW")
	procCreateFontW   = gdi32.NewProc("CreateFontW")
	procSelectObject  = gdi32.NewProc("SelectObject")
	procDeleteObject  = gdi32.NewProc("DeleteObject")
)

const frPrivate = 0x10

const (
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	cwUseDefault       = 0x80000000
	swShow             = 5
	pmRemove           = 0x0001
	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmPaint            = 0x000F
	wmEraseBkgnd       = 0x0014
	wmQuit             = 0x0012
	wmMouseMove        = 0x0200
	wmLButtonDown      = 0x0201
	wmLButtonUp        = 0x0202
	wmRButtonDown      = 0x0204
	wmRButtonUp        = 0x0205
	wmMouseWheel       = 0x020A
	wmChar             = 0x0102
	wmKeyDown          = 0x0100
	IDC_ARROW          = 32512
	vkLButton          = 0x01
	vkRButton          = 0x02
	vkBack             = 0x08
	vkTab              = 0x09
	vkDelete           = 0x2E
	vkControl          = 0x11
	colorWindow        = 5
	srcCopy            = 0x00CC0020
	biRGB              = 0
	dibRGBColors       = 0
	transparent        = 1
	fwBold             = 700
	defaultCharSet     = 1
	outDefaultPrecis   = 0
	clipDefaultPrecis  = 0
	clearTypeQuality   = 5
	variablePitch      = 2
)

type wndClassExW struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type msgW struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type rectW struct {
	left, top, right, bottom int32
}

type bitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]uint32
}

type textDraw struct {
	x, y   int
	text   string
	r, g, b int
	icon   bool // рисовать шрифтом Font Awesome
}

type pointW struct {
	x, y int32
}

type gameWindow struct {
	hwnd          windows.HWND
	width, height int
	pixels        []byte // BGRA, top-down DIB
	closed        bool
	texts         []textDraw
	className     *uint16
	font          windows.Handle
	iconFont      windows.Handle
	lastTitle     string

	mouseX, mouseY int
	leftDown       bool
	rightDown      bool
	leftClick      bool // edge: pressed since last окно.клик
	rightClick     bool
	wheelDelta     int
	charBuf        []rune
}

var (
	activeWindow *gameWindow
	wndProcCb    = syscall.NewCallback(windowProc)
	classRegDone bool
)

func windowProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	w := activeWindow
	switch msg {
	case wmClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		if w != nil && w.hwnd == hwnd {
			w.closed = true
			w.hwnd = 0
		}
		procPostQuitMessage.Call(0)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		// Валидируем без BeginPaint (структура PAINTSTRUCT легко разъезжается
		// по выравниванию). Рисуем только из окно.кадр() через GetDC.
		procValidateRect.Call(uintptr(hwnd), 0)
		return 0
	case wmMouseMove:
		if w != nil && w.hwnd == hwnd {
			w.mouseX = int(int16(lParam & 0xFFFF))
			w.mouseY = int(int16((lParam >> 16) & 0xFFFF))
		}
		return 0
	case wmLButtonDown:
		if w != nil && w.hwnd == hwnd {
			w.leftDown = true
			w.leftClick = true
			w.mouseX = int(int16(lParam & 0xFFFF))
			w.mouseY = int(int16((lParam >> 16) & 0xFFFF))
		}
		return 0
	case wmLButtonUp:
		if w != nil && w.hwnd == hwnd {
			w.leftDown = false
		}
		return 0
	case wmRButtonDown:
		if w != nil && w.hwnd == hwnd {
			w.rightDown = true
			w.rightClick = true
			w.mouseX = int(int16(lParam & 0xFFFF))
			w.mouseY = int(int16((lParam >> 16) & 0xFFFF))
		}
		return 0
	case wmRButtonUp:
		if w != nil && w.hwnd == hwnd {
			w.rightDown = false
		}
		return 0
	case wmMouseWheel:
		if w != nil && w.hwnd == hwnd {
			delta := int(int16((wParam >> 16) & 0xFFFF))
			w.wheelDelta += delta / 120
		}
		return 0
	case wmChar:
		if w != nil && w.hwnd == hwnd {
			ch := rune(wParam)
			if ch >= 32 && ch != 127 {
				w.charBuf = append(w.charBuf, ch)
			}
		}
		return 0
	case wmKeyDown:
		if w != nil && w.hwnd == hwnd && wParam == vkBack {
			// Помечаем backspace через отдельный sentinel в буфере.
			w.charBuf = append(w.charBuf, '\b')
			return 0
		}
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func syncMouseFromCursor(w *gameWindow) {
	if w == nil || w.hwnd == 0 {
		return
	}
	var pt pointW
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procScreenToClient.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&pt)))
	w.mouseX = int(pt.x)
	w.mouseY = int(pt.y)
	// Подстраховка кнопок через GetAsyncKeyState (фокус не обязателен).
	stL, _, _ := procGetAsyncKeyState.Call(vkLButton)
	stR, _, _ := procGetAsyncKeyState.Call(vkRButton)
	left := stL&0x8000 != 0
	right := stR&0x8000 != 0
	if left && !w.leftDown {
		w.leftClick = true
	}
	if right && !w.rightDown {
		w.rightClick = true
	}
	w.leftDown = left
	w.rightDown = right
}

func pumpWindowMessages(w *gameWindow) bool {
	if w == nil || w.hwnd == 0 || w.closed {
		return false
	}
	var msg msgW
	// Защита от шторма сообщений.
	for i := 0; i < 256; i++ {
		ret, _, _ := procPeekMessageW.Call(
			uintptr(unsafe.Pointer(&msg)), 0, 0, 0, pmRemove,
		)
		if ret == 0 {
			break
		}
		if msg.message == wmQuit {
			w.closed = true
			return false
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		if w.closed {
			return false
		}
	}
	return true
}

func presentWindow(w *gameWindow) {
	if w == nil || w.hwnd == 0 {
		return
	}
	hdc, _, _ := procGetDC.Call(uintptr(w.hwnd))
	if hdc == 0 {
		return
	}
	presentToDC(w, hdc)
	procReleaseDC.Call(uintptr(w.hwnd), hdc)
}

func presentToDC(w *gameWindow, hdc uintptr) {
	if w == nil || len(w.pixels) == 0 || hdc == 0 {
		return
	}

	var bi bitmapInfo
	bi.header.size = uint32(unsafe.Sizeof(bi.header))
	bi.header.width = int32(w.width)
	bi.header.height = -int32(w.height) // top-down
	bi.header.planes = 1
	bi.header.bitCount = 32
	bi.header.compression = biRGB

	procStretchDIBits.Call(
		hdc,
		0, 0, uintptr(w.width), uintptr(w.height),
		0, 0, uintptr(w.width), uintptr(w.height),
		uintptr(unsafe.Pointer(&w.pixels[0])),
		uintptr(unsafe.Pointer(&bi)),
		dibRGBColors,
		srcCopy,
	)

	procSetBkMode.Call(hdc, transparent)
	for i, t := range w.texts {
		if t.icon && w.iconFont != 0 {
			procSelectObject.Call(hdc, uintptr(w.iconFont))
		} else if w.font != 0 {
			procSelectObject.Call(hdc, uintptr(w.font))
		}
		color := uintptr(uint32(t.r) | uint32(t.g)<<8 | uint32(t.b)<<16)
		procSetTextColor.Call(hdc, color)
		utf16, err := windows.UTF16FromString(t.text)
		if err != nil {
			continue
		}
		procTextOutW.Call(
			hdc,
			uintptr(t.x),
			uintptr(t.y),
			uintptr(unsafe.Pointer(&utf16[0])),
			uintptr(len(utf16)-1),
		)
		if i > 0 && i%32 == 0 {
			pumpWindowMessages(w)
		}
	}
}

func createFontFace(height, weight int, face string) windows.Handle {
	name, err := windows.UTF16PtrFromString(face)
	if err != nil {
		return 0
	}
	h, _, _ := procCreateFontW.Call(
		uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1, // DEFAULT_CHARSET — PUA Glyphs Font Awesome
		outDefaultPrecis, clipDefaultPrecis, clearTypeQuality,
		variablePitch, uintptr(unsafe.Pointer(name)),
	)
	return windows.Handle(h)
}

func loadIconFont() windows.Handle {
	path, err := ensureFAFontFile()
	if err != nil || path == "" {
		return 0
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	procAddFontResourceExW.Call(uintptr(unsafe.Pointer(pathPtr)), frPrivate, 0)
	// FA6 Free: Solid = family "Font Awesome 6 Free Solid" или weight 900 у "Font Awesome 6 Free"
	if h := createFontFace(18, 400, "Font Awesome 6 Free Solid"); h != 0 {
		return h
	}
	return createFontFace(18, 900, "Font Awesome 6 Free")
}

func ensureWindowClass() error {
	if classRegDone {
		return nil
	}
	name, err := windows.UTF16PtrFromString("ZavetWindow")
	if err != nil {
		return err
	}
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(IDC_ARROW))

	var wc wndClassExW
	wc.size = uint32(unsafe.Sizeof(wc))
	wc.wndProc = wndProcCb
	wc.instance = windows.Handle(hInstance)
	wc.cursor = windows.Handle(cursor)
	wc.background = windows.Handle(colorWindow + 1)
	wc.className = name

	atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return fmt.Errorf("RegisterClassExW: %v", callErr)
	}
	classRegDone = true
	return nil
}

func closeActiveWindow() {
	if activeWindow == nil {
		return
	}
	if activeWindow.font != 0 {
		procDeleteObject.Call(uintptr(activeWindow.font))
		activeWindow.font = 0
	}
	if activeWindow.iconFont != 0 {
		procDeleteObject.Call(uintptr(activeWindow.iconFont))
		activeWindow.iconFont = 0
	}
	if activeWindow.hwnd != 0 {
		procDestroyWindow.Call(uintptr(activeWindow.hwnd))
		activeWindow.hwnd = 0
	}
	activeWindow.closed = true
}

func registerWindowModule() {
	fns := map[string]func(args ...Object) Object{
		"создать": func(args ...Object) Object {
			if len(args) < 2 || len(args) > 3 {
				return builtinErrorWrongArgCount("окно.создать", 3, len(args))
			}
			wObj, ok := args[0].(*Integer)
			if !ok {
				return builtinErrorWrongArgType("окно.создать", 1, "INTEGER", string(args[0].Type()))
			}
			hObj, ok := args[1].(*Integer)
			if !ok {
				return builtinErrorWrongArgType("окно.создать", 2, "INTEGER", string(args[1].Type()))
			}
			title := "Завет"
			if len(args) == 3 {
				t, ok := args[2].(*String)
				if !ok {
					return builtinErrorWrongArgType("окно.создать", 3, "STRING", string(args[2].Type()))
				}
				title = t.Value
			}
			if wObj.Value < 64 || hObj.Value < 64 || wObj.Value > 4096 || hObj.Value > 4096 {
				return ErrorWithHint(getCurrentCallToken(), "размер окна за пределами 64…4096", "")
			}

			closeActiveWindow()
			if err := ensureWindowClass(); err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог зарегистрировать образ окна: "+err.Error(), "")
			}

			titlePtr, err := windows.UTF16PtrFromString(title)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), err.Error(), "")
			}
			classPtr, _ := windows.UTF16PtrFromString("ZavetWindow")
			hInstance, _, _ := procGetModuleHandleW.Call(0)

			width := int(wObj.Value)
			height := int(hObj.Value)

			// Adjust for non-client area approximately via CreateWindow with desired client size:
			// Create with exact outer size first; good enough for games.
			hwndU, _, callErr := procCreateWindowExW.Call(
				0,
				uintptr(unsafe.Pointer(classPtr)),
				uintptr(unsafe.Pointer(titlePtr)),
				uintptr(wsOverlappedWindow|wsVisible),
				uintptr(cwUseDefault),
				uintptr(cwUseDefault),
				uintptr(width+16),
				uintptr(height+39),
				0, 0, hInstance, 0,
			)
			if hwndU == 0 {
				return ErrorWithHint(getCurrentCallToken(), fmt.Sprintf("CreateWindowExW: %v", callErr), "")
			}
			hwnd := windows.HWND(hwndU)

			fontU, _, _ := procCreateFontW.Call(
				18, 0, 0, 0, 400, 0, 0, 0,
				defaultCharSet, outDefaultPrecis, clipDefaultPrecis, clearTypeQuality,
				variablePitch, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Segoe UI"))),
			)

			gw := &gameWindow{
				hwnd:     hwnd,
				width:    width,
				height:   height,
				pixels:   make([]byte, width*height*4),
				texts:    nil,
				font:     windows.Handle(fontU),
				iconFont: loadIconFont(),
			}
			activeWindow = gw

			procShowWindow.Call(uintptr(hwnd), swShow)
			procUpdateWindow.Call(uintptr(hwnd))
			return NULL
		},

		"очистить": func(args ...Object) Object {
			if activeWindow == nil || activeWindow.closed {
				return ErrorWithHint(getCurrentCallToken(), "окно не создано", "Сначала вызовите окно.создать(...)")
			}
			if len(args) != 3 {
				return builtinErrorWrongArgCount("окно.очистить", 3, len(args))
			}
			r, g, b, err := rgbArgs(args, "окно.очистить")
			if err != nil {
				return err
			}
			pix := activeWindow.pixels
			for i := 0; i < len(pix); i += 4 {
				pix[i+0] = byte(b)
				pix[i+1] = byte(g)
				pix[i+2] = byte(r)
				pix[i+3] = 255
			}
			activeWindow.texts = activeWindow.texts[:0]
			return NULL
		},

		"прямоугольник": func(args ...Object) Object {
			if activeWindow == nil || activeWindow.closed {
				return ErrorWithHint(getCurrentCallToken(), "окно не создано", "Сначала вызовите окно.создать(...)")
			}
			if len(args) != 7 {
				return builtinErrorWrongArgCount("окно.прямоугольник", 7, len(args))
			}
			vals := make([]int, 4)
			for i := 0; i < 4; i++ {
				n, ok := args[i].(*Integer)
				if !ok {
					return builtinErrorWrongArgType("окно.прямоугольник", i+1, "INTEGER", string(args[i].Type()))
				}
				vals[i] = int(n.Value)
			}
			r, g, b, errObj := rgbArgs(args[4:], "окно.прямоугольник")
			if errObj != nil {
				return errObj
			}
			fillRect(activeWindow, vals[0], vals[1], vals[2], vals[3], r, g, b)
			return NULL
		},

		"текст": func(args ...Object) Object {
			if activeWindow == nil || activeWindow.closed {
				return ErrorWithHint(getCurrentCallToken(), "окно не создано", "Сначала вызовите окно.создать(...)")
			}
			if len(args) != 6 {
				return builtinErrorWrongArgCount("окно.текст", 6, len(args))
			}
			x, ok := args[0].(*Integer)
			if !ok {
				return builtinErrorWrongArgType("окно.текст", 1, "INTEGER", string(args[0].Type()))
			}
			y, ok := args[1].(*Integer)
			if !ok {
				return builtinErrorWrongArgType("окно.текст", 2, "INTEGER", string(args[1].Type()))
			}
			s, ok := args[2].(*String)
			if !ok {
				return builtinErrorWrongArgType("окно.текст", 3, "STRING", string(args[2].Type()))
			}
			r, g, b, errObj := rgbArgs(args[3:], "окно.текст")
			if errObj != nil {
				return errObj
			}
			activeWindow.texts = append(activeWindow.texts, textDraw{
				x: int(x.Value), y: int(y.Value), text: s.Value, r: r, g: g, b: b,
			})
			return NULL
		},

		"заголовок": func(args ...Object) Object {
			if activeWindow == nil || activeWindow.hwnd == 0 {
				return ErrorWithHint(getCurrentCallToken(), "окно не создано", "")
			}
			if len(args) != 1 {
				return builtinErrorWrongArgCount("окно.заголовок", 1, len(args))
			}
			s, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("окно.заголовок", 1, "STRING", string(args[0].Type()))
			}
			if s.Value == activeWindow.lastTitle {
				return NULL
			}
			activeWindow.lastTitle = s.Value
			ptr, err := windows.UTF16PtrFromString(s.Value)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), err.Error(), "")
			}
			procSetWindowTextW.Call(uintptr(activeWindow.hwnd), uintptr(unsafe.Pointer(ptr)))
			return NULL
		},

		"клавиша": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("окно.клавиша", 1, len(args))
			}
			s, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("окно.клавиша", 1, "STRING", string(args[0].Type()))
			}
			vk, ok := keyToVK(s.Value)
			if !ok {
				return ErrorWithHint(getCurrentCallToken(), "неизвестная клавиша: "+s.Value,
					"Доступно: влево/вправо/вверх/вниз, WASD, пробел, escape, enter, r, p")
			}
			state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
			if state&0x8000 != 0 {
				return TRUE
			}
			return FALSE
		},

		"кадр": func(args ...Object) Object {
			if activeWindow == nil {
				return FALSE
			}
			if activeWindow.closed || activeWindow.hwnd == 0 {
				return FALSE
			}

			if !pumpWindowMessages(activeWindow) {
				return FALSE
			}
			syncMouseFromCursor(activeWindow)

			// Рисуем напрямую в DC — без InvalidateRect (иначе вместе с
			// SetWindowText получается бесконечный поток WM_PAINT).
			presentWindow(activeWindow)

			if activeWindow.closed {
				return FALSE
			}
			return TRUE
		},

		"иконка": func(args ...Object) Object {
			if activeWindow == nil || activeWindow.closed {
				return ErrorWithHint(getCurrentCallToken(), "окно не создано", "Сначала вызовите окно.создать(...)")
			}
			if len(args) != 6 {
				return builtinErrorWrongArgCount("окно.иконка", 6, len(args))
			}
			x, ok := args[0].(*Integer)
			if !ok {
				return builtinErrorWrongArgType("окно.иконка", 1, "INTEGER", string(args[0].Type()))
			}
			y, ok := args[1].(*Integer)
			if !ok {
				return builtinErrorWrongArgType("окно.иконка", 2, "INTEGER", string(args[1].Type()))
			}
			name, ok := args[2].(*String)
			if !ok {
				return builtinErrorWrongArgType("окно.иконка", 3, "STRING", string(args[2].Type()))
			}
			r, g, b, errObj := rgbArgs(args[3:], "окно.иконка")
			if errObj != nil {
				return errObj
			}
			ch, ok := faIconRune(name.Value)
			if !ok {
				return ErrorWithHint(getCurrentCallToken(), "неизвестная иконка: "+name.Value,
					"См. окно.список_иконок() — Font Awesome Solid")
			}
			activeWindow.texts = append(activeWindow.texts, textDraw{
				x: int(x.Value), y: int(y.Value), text: string(ch), r: r, g: g, b: b, icon: true,
			})
			return NULL
		},
		"список_иконок": func(args ...Object) Object {
			names := make([]string, 0, len(faSolidIcons))
			for n := range faSolidIcons {
				names = append(names, n)
			}
			sort.Strings(names)
			out := make([]Object, len(names))
			for i, n := range names {
				out[i] = &String{Value: n}
			}
			return &Array{Elements: out}
		},

		"мышь_x": func(args ...Object) Object {
			if activeWindow == nil {
				return &Integer{Value: 0}
			}
			return &Integer{Value: int64(activeWindow.mouseX)}
		},
		"мышь_y": func(args ...Object) Object {
			if activeWindow == nil {
				return &Integer{Value: 0}
			}
			return &Integer{Value: int64(activeWindow.mouseY)}
		},
		"мышь": func(args ...Object) Object {
			if activeWindow == nil {
				return FALSE
			}
			btn := "лево"
			if len(args) >= 1 {
				if s, ok := args[0].(*String); ok {
					btn = s.Value
				}
			}
			switch btn {
			case "лево", "left", "лкм":
				if activeWindow.leftDown {
					return TRUE
				}
			case "право", "right", "пкм":
				if activeWindow.rightDown {
					return TRUE
				}
			}
			return FALSE
		},
		"клик": func(args ...Object) Object {
			if activeWindow == nil {
				return FALSE
			}
			btn := "лево"
			if len(args) >= 1 {
				if s, ok := args[0].(*String); ok {
					btn = s.Value
				}
			}
			switch btn {
			case "лево", "left", "лкм":
				if activeWindow.leftClick {
					activeWindow.leftClick = false
					return TRUE
				}
			case "право", "right", "пкм":
				if activeWindow.rightClick {
					activeWindow.rightClick = false
					return TRUE
				}
			}
			return FALSE
		},
		"колесо": func(args ...Object) Object {
			if activeWindow == nil {
				return &Integer{Value: 0}
			}
			d := activeWindow.wheelDelta
			activeWindow.wheelDelta = 0
			return &Integer{Value: int64(d)}
		},
		"символ": func(args ...Object) Object {
			if activeWindow == nil {
				return &String{Value: ""}
			}
			s := string(activeWindow.charBuf)
			activeWindow.charBuf = activeWindow.charBuf[:0]
			return &String{Value: s}
		},

		"задержка": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("окно.задержка", 1, len(args))
			}
			ms, ok := args[0].(*Integer)
			if !ok {
				return builtinErrorWrongArgType("окно.задержка", 1, "INTEGER", string(args[0].Type()))
			}
			if ms.Value < 0 {
				ms.Value = 0
			}
			if ms.Value > 60000 {
				ms.Value = 60000
			}
			// Спим кусками и прокачиваем очередь — иначе Windows
			// помечает окно «Не отвечает» на длинных кадрах.
			left := time.Duration(ms.Value) * time.Millisecond
			for left > 0 {
				chunk := 8 * time.Millisecond
				if chunk > left {
					chunk = left
				}
				time.Sleep(chunk)
				left -= chunk
				if activeWindow != nil {
					pumpWindowMessages(activeWindow)
				}
			}
			return NULL
		},

		"закрыть": func(args ...Object) Object {
			closeActiveWindow()
			return NULL
		},

		"открыто": func(args ...Object) Object {
			if activeWindow != nil && !activeWindow.closed && activeWindow.hwnd != 0 {
				return TRUE
			}
			return FALSE
		},

		"ширина": func(args ...Object) Object {
			if activeWindow == nil {
				return &Integer{Value: 0}
			}
			return &Integer{Value: int64(activeWindow.width)}
		},

		"высота": func(args ...Object) Object {
			if activeWindow == nil {
				return &Integer{Value: 0}
			}
			return &Integer{Value: int64(activeWindow.height)}
		},
	}
	stdModules["окно"] = makeHashFromBuiltins(fns)
}

func rgbArgs(args []Object, fn string) (r, g, b int, errObj *Error) {
	if len(args) < 3 {
		return 0, 0, 0, builtinErrorWrongArgCount(fn, 3, len(args))
	}
	rr, ok := args[0].(*Integer)
	if !ok {
		return 0, 0, 0, builtinErrorWrongArgType(fn, 1, "INTEGER", string(args[0].Type()))
	}
	gg, ok := args[1].(*Integer)
	if !ok {
		return 0, 0, 0, builtinErrorWrongArgType(fn, 2, "INTEGER", string(args[1].Type()))
	}
	bb, ok := args[2].(*Integer)
	if !ok {
		return 0, 0, 0, builtinErrorWrongArgType(fn, 3, "INTEGER", string(args[2].Type()))
	}
	clamp := func(v int64) int {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return int(v)
	}
	return clamp(rr.Value), clamp(gg.Value), clamp(bb.Value), nil
}

func fillRect(w *gameWindow, x, y, rw, rh, r, g, b int) {
	if rw <= 0 || rh <= 0 {
		return
	}
	if x < 0 {
		rw += x
		x = 0
	}
	if y < 0 {
		rh += y
		y = 0
	}
	if x >= w.width || y >= w.height {
		return
	}
	if x+rw > w.width {
		rw = w.width - x
	}
	if y+rh > w.height {
		rh = w.height - y
	}
	pix := w.pixels
	stride := w.width * 4
	for yy := y; yy < y+rh; yy++ {
		row := yy * stride
		for xx := x; xx < x+rw; xx++ {
			i := row + xx*4
			pix[i+0] = byte(b)
			pix[i+1] = byte(g)
			pix[i+2] = byte(r)
			pix[i+3] = 255
		}
	}
}

func keyToVK(name string) (int, bool) {
	switch name {
	case "влево", "left", "a", "A", "ф", "Ф":
		if name == "a" || name == "A" || name == "ф" || name == "Ф" {
			return 0x41, true // A
		}
		return 0x25, true // VK_LEFT
	case "вправо", "right", "d", "D", "в", "В":
		if name == "d" || name == "D" || name == "в" || name == "В" {
			return 0x44, true
		}
		return 0x27, true
	case "вверх", "up", "w", "W", "ц", "Ц":
		if name == "w" || name == "W" || name == "ц" || name == "Ц" {
			return 0x57, true
		}
		return 0x26, true
	case "вниз", "down", "s", "S", "ы", "Ы":
		if name == "s" || name == "S" || name == "ы" || name == "Ы" {
			return 0x53, true
		}
		return 0x28, true
	case "пробел", "space":
		return 0x20, true
	case "escape", "esc", "выход":
		return 0x1B, true
	case "enter", "ввод":
		return 0x0D, true
	case "backspace", "back", "стереть":
		return vkBack, true
	case "tab", "таб":
		return vkTab, true
	case "delete", "del", "удалить":
		return vkDelete, true
	case "ctrl", "control", "контроль":
		return vkControl, true
	case "r", "R", "к", "К":
		return 0x52, true
	case "p", "P", "з", "З":
		return 0x50, true // пауза
	case "l", "L", "д", "Д":
		return 0x4C, true
	case "t", "T", "е", "Е":
		return 0x54, true
	case "n", "N", "т", "Т":
		return 0x4E, true
	case "f5":
		return 0x74, true
	}
	runes := []rune(name)
	if len(runes) == 1 {
		c := runes[0]
		if c >= 'a' && c <= 'z' {
			return int(c - 32), true
		}
		if c >= 'A' && c <= 'Z' {
			return int(c), true
		}
		if c >= '0' && c <= '9' {
			return int(c), true
		}
	}
	return 0, false
}

func init() {
	registerWindowModule()
}
