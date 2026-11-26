//go:build !windows && !js

package interpreter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Браузерный backend «окно»: HTTP + canvas. Работает на Linux/macOS
// без CGO. Windows использует нативный Win32 (stdlib_window_windows.go).

type browserTextDraw struct {
	x, y     int
	text     string
	r, g, b  byte
	icon     bool
	iconName string
}

type browserWin struct {
	mu         sync.Mutex
	w, h       int
	pixels     []byte // BGRA
	texts      []browserTextDraw
	keys       map[string]bool
	open       bool
	title      string
	addr       string
	ln         net.Listener
	mouseX     int
	mouseY     int
	leftDown   bool
	rightDown  bool
	leftClick  bool
	rightClick bool
	wheelDelta int
	charBuf    []rune
}

var (
	bwin        *browserWin
	openBrowser = defaultOpenBrowser
)

func registerWindowModule() {
	fns := map[string]func(args ...Object) Object{
		"создать":       browserCreate,
		"очистить":      browserClear,
		"прямоугольник": browserRect,
		"текст":         browserText,
		"иконка":        browserIcon,
		"список_иконок": browserIconList,
		"заголовок":     browserTitle,
		"клавиша":       browserKey,
		"кадр":          browserFrame,
		"задержка":      browserDelay,
		"закрыть":       browserClose,
		"мышь_x":        browserMouseX,
		"мышь_y":        browserMouseY,
		"мышь":          browserMouseBtn,
		"клик":          browserClick,
		"колесо":        browserWheel,
		"символ":        browserChars,
		"открыто": func(args ...Object) Object {
			if bwin == nil {
				return FALSE
			}
			bwin.mu.Lock()
			open := bwin.open
			bwin.mu.Unlock()
			if open {
				return TRUE
			}
			return FALSE
		},
		"ширина": func(args ...Object) Object {
			if bwin == nil {
				return &Integer{Value: 0}
			}
			return &Integer{Value: int64(bwin.w)}
		},
		"высота": func(args ...Object) Object {
			if bwin == nil {
				return &Integer{Value: 0}
			}
			return &Integer{Value: int64(bwin.h)}
		},
	}
	stdModules["окно"] = makeHashFromBuiltins(fns)
}

func init() {
	registerWindowModule()
}

func browserCloseActive() {
	if bwin == nil {
		return
	}
	bwin.mu.Lock()
	bwin.open = false
	ln := bwin.ln
	bwin.ln = nil
	bwin.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

func browserRGBArgs(args []Object, fn string) (byte, byte, byte, *Error) {
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
	clamp := func(v int64) byte {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return byte(v)
	}
	return clamp(rr.Value), clamp(gg.Value), clamp(bb.Value), nil
}

func browserCreate(args ...Object) Object {
	if len(args) < 2 || len(args) > 3 {
		return ErrorWithHint(currentCallToken, "окно.создать(ширина, высота [, заголовок])", "")
	}
	w, ok1 := args[0].(*Integer)
	h, ok2 := args[1].(*Integer)
	if !ok1 || !ok2 {
		return ErrorWithHint(currentCallToken, "ширина и высота — целые числа", "")
	}
	if w.Value < 64 || w.Value > 4096 || h.Value < 64 || h.Value > 4096 {
		return ErrorWithHint(currentCallToken, "неверный размер окна",
			"Ширина и высота должны быть в пределах 64…4096")
	}
	title := "Завет"
	if len(args) == 3 {
		s, ok := args[2].(*String)
		if !ok {
			return builtinErrorWrongArgType("окно.создать", 3, "STRING", string(args[2].Type()))
		}
		title = s.Value
	}

	browserCloseActive()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return ErrorWithHint(currentCallToken, "не смог открыть порт: "+err.Error(), "")
	}
	bw := &browserWin{
		w: int(w.Value), h: int(h.Value),
		pixels: make([]byte, int(w.Value)*int(h.Value)*4),
		keys:   make(map[string]bool),
		open:   true,
		title:  title,
		addr:   ln.Addr().String(),
		ln:     ln,
	}
	// Непрозрачный чёрный фон по умолчанию (alpha = 255)
	for i := 3; i < len(bw.pixels); i += 4 {
		bw.pixels[i] = 255
	}
	bwin = bw

	mux := http.NewServeMux()
	mux.HandleFunc("/", bw.serveHTML)
	mux.HandleFunc("/frame.png", bw.servePNG)
	mux.HandleFunc("/input", bw.serveInput)
	mux.HandleFunc("/close", func(w http.ResponseWriter, r *http.Request) {
		bw.mu.Lock()
		bw.open = false
		bw.mu.Unlock()
		w.WriteHeader(204)
	})
	go func() {
		_ = http.Serve(ln, mux)
	}()
	url := "http://" + bw.addr + "/"
	_ = openBrowser(url)
	return NULL
}

func (bw *browserWin) serveHTML(w http.ResponseWriter, r *http.Request) {
	bw.mu.Lock()
	title := bw.title
	bwWidth, bwHeight := bw.w, bw.h
	bw.mu.Unlock()

	html := fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%s</title>
<style>body{margin:0;background:#111;display:flex;justify-content:center;align-items:center;height:100vh;overflow:hidden}
canvas{image-rendering:pixelated;border:1px solid #333;cursor:crosshair}</style></head>
<body><canvas id="c" width="%d" height="%d" tabindex="0"></canvas>
<script>
const c=document.getElementById('c'),x=c.getContext('2d');
c.focus();
const state={keys:{},mx:0,my:0,left:false,right:false,lclick:false,rclick:false,wheel:0,chars:''};
const mapKey=e=>{
  if(e.code && e.code.startsWith('Key') && e.code.length===4) return e.code[3].toLowerCase();
  if(e.code && e.code.startsWith('Digit') && e.code.length===6) return e.code[5];
  const k=(e.key||'').toLowerCase();
  if(k===' ')return ' ';
  return k;
};
c.onmousemove=e=>{const r=c.getBoundingClientRect();state.mx=Math.floor((e.clientX-r.left)*c.width/r.width);state.my=Math.floor((e.clientY-r.top)*c.height/r.height)};
c.onmousedown=e=>{if(e.button===0){state.left=true;state.lclick=true}if(e.button===2){state.right=true;state.rclick=true};e.preventDefault()};
c.onmouseup=e=>{if(e.button===0)state.left=false;if(e.button===2)state.right=false};
c.oncontextmenu=e=>e.preventDefault();
c.onwheel=e=>{state.wheel+=e.deltaY>0?-1:1;e.preventDefault()};
window.onkeydown=e=>{
  const mk=mapKey(e);
  if(mk)state.keys[mk]=true;
  if(e.key==='Backspace'){state.chars+='\b';e.preventDefault();return}
  if(e.key && e.key.length===1 && !e.ctrlKey && !e.metaKey){state.chars+=e.key;e.preventDefault()}
  if(['ArrowUp','ArrowDown','ArrowLeft','ArrowRight','Tab',' '].includes(e.key))e.preventDefault();
};
window.onkeyup=e=>{
  const mk=mapKey(e);
  if(mk)state.keys[mk]=false;
};
window.onblur=()=>{for(const k in state.keys)state.keys[k]=false;state.left=false;state.right=false};
window.addEventListener('beforeunload',()=>{if(navigator.sendBeacon)navigator.sendBeacon('/close');});
let closed=false;
async function tick(){
  if(closed)return;
  try{
    const img=new Image();
    img.onload=()=>x.drawImage(img,0,0);
    img.src='/frame.png?t='+Date.now();
    const payload={
      keys:Object.keys(state.keys).filter(k=>state.keys[k]),
      mx:state.mx,my:state.my,left:state.left,right:state.right,
      lclick:state.lclick,rclick:state.rclick,wheel:state.wheel,chars:state.chars
    };
    state.lclick=false;state.rclick=false;state.wheel=0;state.chars='';
    const res=await fetch('/input',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});
    if(res.ok){
      const info=await res.json();
      if(info.title && document.title!==info.title)document.title=info.title;
      if(info.open===false){closed=true;return;}
    }
  }catch(e){}
  requestAnimationFrame(tick);
}
tick();
</script></body></html>`, htmlEscape(title), bwWidth, bwHeight)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// renderFrameBytes рисует PNG текущего кадра (буфер + наложенные текст и иконки).
func (bw *browserWin) renderFrameBytes() ([]byte, error) {
	bw.mu.Lock()
	img := image.NewRGBA(image.Rect(0, 0, bw.w, bw.h))
	for y := 0; y < bw.h; y++ {
		for x := 0; x < bw.w; x++ {
			i := (y*bw.w + x) * 4
			img.SetRGBA(x, y, color.RGBA{R: bw.pixels[i+2], G: bw.pixels[i+1], B: bw.pixels[i], A: 255})
		}
	}
	texts := append([]browserTextDraw(nil), bw.texts...)
	w, h := bw.w, bw.h
	bw.mu.Unlock()

	for _, td := range texts {
		col := color.RGBA{R: td.r, G: td.g, B: td.b, A: 255}
		if td.icon {
			drawBrowserIcon(img, w, h, td.x, td.y, td.iconName, col)
		} else {
			drawBrowserText(img, w, h, td.x, td.y, td.text, col)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawBrowserText(img *image.RGBA, w, h, x0, y0 int, text string, col color.RGBA) {
	cx := x0
	for _, ch := range text {
		glyph := pixelFont[ch]
		if glyph == nil {
			glyph = pixelFont['?']
		}
		for row := 0; row < 7 && row < len(glyph); row++ {
			bits := glyph[row]
			for colIdx := 0; colIdx < 5; colIdx++ {
				if bits&(1<<(4-colIdx)) != 0 {
					px, py := cx+colIdx, y0+row
					if px >= 0 && py >= 0 && px < w && py < h {
						img.SetRGBA(px, py, col)
					}
				}
			}
		}
		cx += 6
	}
}

func drawBrowserIcon(img *image.RGBA, w, h, x0, y0 int, iconName string, col color.RGBA) {
	if mask, ok := pixelIcons12[iconName]; ok {
		for dy := 0; dy < 12; dy++ {
			row := mask[dy]
			for dx := 0; dx < 12; dx++ {
				if row&(1<<(11-dx)) != 0 {
					px, py := x0+dx, y0+dy
					if px >= 0 && py >= 0 && px < w && py < h {
						img.SetRGBA(px, py, col)
					}
				}
			}
		}
		return
	}
	// Fallback рамка для редких иконок
	for dy := 0; dy < 12; dy++ {
		for dx := 0; dx < 12; dx++ {
			if (dx > 2 && dx < 9 && dy > 2 && dy < 9) || dx == 0 || dy == 0 || dx == 11 || dy == 11 {
				px, py := x0+dx, y0+dy
				if px >= 0 && py >= 0 && px < w && py < h {
					img.SetRGBA(px, py, col)
				}
			}
		}
	}
}

func (bw *browserWin) servePNG(w http.ResponseWriter, r *http.Request) {
	data, err := bw.renderFrameBytes()
	if err != nil {
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

type browserInputPayload struct {
	Keys   []string `json:"keys"`
	MX     int      `json:"mx"`
	MY     int      `json:"my"`
	Left   bool     `json:"left"`
	Right  bool     `json:"right"`
	LClick bool     `json:"lclick"`
	RClick bool     `json:"rclick"`
	Wheel  int      `json:"wheel"`
	Chars  string   `json:"chars"`
}

func (bw *browserWin) serveInput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	var p browserInputPayload
	if err := json.Unmarshal(body, &p); err != nil {
		w.WriteHeader(400)
		return
	}
	bw.mu.Lock()
	bw.keys = make(map[string]bool, len(p.Keys))
	for _, k := range p.Keys {
		bw.keys[strings.ToLower(k)] = true
	}
	bw.mouseX, bw.mouseY = p.MX, p.MY
	bw.leftDown, bw.rightDown = p.Left, p.Right
	if p.LClick {
		bw.leftClick = true
	}
	if p.RClick {
		bw.rightClick = true
	}
	bw.wheelDelta += p.Wheel
	for _, ch := range p.Chars {
		bw.charBuf = append(bw.charBuf, ch)
	}
	resp := struct {
		Open  bool   `json:"open"`
		Title string `json:"title"`
	}{Open: bw.open, Title: bw.title}
	bw.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func browserClear(args ...Object) Object {
	if bwin == nil || !bwin.open {
		return ErrorWithHint(currentCallToken, "окно не создано", "Сначала вызовите окно.создать(...)")
	}
	if len(args) != 3 {
		return builtinErrorWrongArgCount("окно.очистить", 3, len(args))
	}
	r, g, b, errObj := browserRGBArgs(args, "окно.очистить")
	if errObj != nil {
		return errObj
	}
	bwin.mu.Lock()
	for i := 0; i < len(bwin.pixels); i += 4 {
		bwin.pixels[i] = b
		bwin.pixels[i+1] = g
		bwin.pixels[i+2] = r
		bwin.pixels[i+3] = 255
	}
	bwin.texts = bwin.texts[:0]
	bwin.mu.Unlock()
	return NULL
}

func browserRect(args ...Object) Object {
	if bwin == nil || !bwin.open {
		return ErrorWithHint(currentCallToken, "окно не создано", "Сначала вызовите окно.создать(...)")
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
	rr, gg, bb, errObj := browserRGBArgs(args[4:], "окно.прямоугольник")
	if errObj != nil {
		return errObj
	}
	xi, yi, ww, hh := vals[0], vals[1], vals[2], vals[3]
	if ww <= 0 || hh <= 0 {
		return NULL
	}
	bwin.mu.Lock()
	for y := yi; y < yi+hh && y < bwin.h; y++ {
		if y < 0 {
			continue
		}
		for x := xi; x < xi+ww && x < bwin.w; x++ {
			if x < 0 {
				continue
			}
			i := (y*bwin.w + x) * 4
			bwin.pixels[i] = bb
			bwin.pixels[i+1] = gg
			bwin.pixels[i+2] = rr
			bwin.pixels[i+3] = 255
		}
	}
	bwin.mu.Unlock()
	return NULL
}

func browserText(args ...Object) Object {
	if bwin == nil || !bwin.open {
		return ErrorWithHint(currentCallToken, "окно не создано", "Сначала вызовите окно.создать(...)")
	}
	if len(args) != 6 {
		return builtinErrorWrongArgCount("окно.текст", 6, len(args))
	}
	x0, ok := args[0].(*Integer)
	if !ok {
		return builtinErrorWrongArgType("окно.текст", 1, "INTEGER", string(args[0].Type()))
	}
	y0, ok := args[1].(*Integer)
	if !ok {
		return builtinErrorWrongArgType("окно.текст", 2, "INTEGER", string(args[1].Type()))
	}
	s, ok := args[2].(*String)
	if !ok {
		return builtinErrorWrongArgType("окно.текст", 3, "STRING", string(args[2].Type()))
	}
	rr, gg, bb, errObj := browserRGBArgs(args[3:], "окно.текст")
	if errObj != nil {
		return errObj
	}
	bwin.mu.Lock()
	bwin.texts = append(bwin.texts, browserTextDraw{
		x: int(x0.Value), y: int(y0.Value), text: s.Value,
		r: rr, g: gg, b: bb,
	})
	bwin.mu.Unlock()
	return NULL
}

func browserIcon(args ...Object) Object {
	if bwin == nil || !bwin.open {
		return ErrorWithHint(currentCallToken, "окно не создано", "Сначала вызовите окно.создать(...)")
	}
	if len(args) != 6 {
		return builtinErrorWrongArgCount("окно.иконка", 6, len(args))
	}
	x0, ok := args[0].(*Integer)
	if !ok {
		return builtinErrorWrongArgType("окно.иконка", 1, "INTEGER", string(args[0].Type()))
	}
	y0, ok := args[1].(*Integer)
	if !ok {
		return builtinErrorWrongArgType("окно.иконка", 2, "INTEGER", string(args[1].Type()))
	}
	name, ok := args[2].(*String)
	if !ok {
		return builtinErrorWrongArgType("окно.иконка", 3, "STRING", string(args[2].Type()))
	}
	rr, gg, bb, errObj := browserRGBArgs(args[3:], "окно.иконка")
	if errObj != nil {
		return errObj
	}
	if _, ok := faIconRune(name.Value); !ok {
		return ErrorWithHint(currentCallToken, "неизвестная иконка: "+name.Value,
			"См. окно.список_иконок() — Font Awesome Solid")
	}
	bwin.mu.Lock()
	bwin.texts = append(bwin.texts, browserTextDraw{
		x: int(x0.Value), y: int(y0.Value), icon: true, iconName: name.Value,
		r: rr, g: gg, b: bb,
	})
	bwin.mu.Unlock()
	return NULL
}

func browserIconList(args ...Object) Object {
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
}

func browserTitle(args ...Object) Object {
	if bwin == nil || !bwin.open {
		return ErrorWithHint(currentCallToken, "окно не создано", "")
	}
	if len(args) != 1 {
		return builtinErrorWrongArgCount("окно.заголовок", 1, len(args))
	}
	s, ok := args[0].(*String)
	if !ok {
		return builtinErrorWrongArgType("окно.заголовок", 1, "STRING", string(args[0].Type()))
	}
	bwin.mu.Lock()
	bwin.title = s.Value
	bwin.mu.Unlock()
	return NULL
}

func browserKey(args ...Object) Object {
	if len(args) != 1 {
		return builtinErrorWrongArgCount("окно.клавиша", 1, len(args))
	}
	name, ok := args[0].(*String)
	if !ok {
		return builtinErrorWrongArgType("окно.клавиша", 1, "STRING", string(args[0].Type()))
	}
	key, valid := normalizeBrowserKey(name.Value)
	if !valid {
		return ErrorWithHint(currentCallToken, "неизвестная клавиша: "+name.Value,
			"Доступно: влево/вправо/вверх/вниз, WASD, пробел, escape, enter, r, p")
	}
	if bwin == nil {
		return FALSE
	}
	bwin.mu.Lock()
	pressed := bwin.keys[key]
	bwin.mu.Unlock()
	if pressed {
		return TRUE
	}
	return FALSE
}

func normalizeBrowserKey(name string) (string, bool) {
	switch name {
	case "влево", "left":
		return "arrowleft", true
	case "вправо", "right":
		return "arrowright", true
	case "вверх", "up":
		return "arrowup", true
	case "вниз", "down":
		return "arrowdown", true
	case "a", "A", "ф", "Ф":
		return "a", true
	case "d", "D", "в", "В":
		return "d", true
	case "w", "W", "ц", "Ц":
		return "w", true
	case "s", "S", "ы", "Ы":
		return "s", true
	case "пробел", "space":
		return " ", true
	case "escape", "esc", "выход":
		return "escape", true
	case "enter", "ввод":
		return "enter", true
	case "backspace", "back", "стереть":
		return "backspace", true
	case "tab", "таб":
		return "tab", true
	case "delete", "del", "удалить":
		return "delete", true
	case "ctrl", "control", "контроль":
		return "control", true
	case "r", "R", "к", "К":
		return "r", true
	case "p", "P", "з", "З":
		return "p", true
	case "l", "L", "д", "Д":
		return "l", true
	case "t", "T", "е", "Е":
		return "t", true
	case "n", "N", "т", "Т":
		return "n", true
	case "f5":
		return "f5", true
	}
	runes := []rune(name)
	if len(runes) == 1 {
		c := runes[0]
		if c >= 'a' && c <= 'z' {
			return string(c), true
		}
		if c >= 'A' && c <= 'Z' {
			return string(c - 'A' + 'a'), true
		}
		if c >= '0' && c <= '9' {
			return string(c), true
		}
	}
	return "", false
}

func browserFrame(args ...Object) Object {
	if bwin == nil {
		return FALSE
	}
	bwin.mu.Lock()
	open := bwin.open
	bwin.mu.Unlock()
	if open {
		return TRUE
	}
	return FALSE
}

func browserDelay(args ...Object) Object {
	if len(args) != 1 {
		return builtinErrorWrongArgCount("окно.задержка", 1, len(args))
	}
	ms, ok := args[0].(*Integer)
	if !ok {
		return builtinErrorWrongArgType("окно.задержка", 1, "INTEGER", string(args[0].Type()))
	}
	val := ms.Value
	if val < 0 {
		val = 0
	}
	if val > 60000 {
		val = 60000
	}
	time.Sleep(time.Duration(val) * time.Millisecond)
	return NULL
}

func browserClose(args ...Object) Object {
	browserCloseActive()
	return NULL
}

func browserMouseX(args ...Object) Object {
	if bwin == nil {
		return &Integer{Value: 0}
	}
	bwin.mu.Lock()
	defer bwin.mu.Unlock()
	return &Integer{Value: int64(bwin.mouseX)}
}

func browserMouseY(args ...Object) Object {
	if bwin == nil {
		return &Integer{Value: 0}
	}
	bwin.mu.Lock()
	defer bwin.mu.Unlock()
	return &Integer{Value: int64(bwin.mouseY)}
}

func browserMouseBtn(args ...Object) Object {
	if bwin == nil {
		return FALSE
	}
	btn := "лево"
	if len(args) >= 1 {
		if s, ok := args[0].(*String); ok {
			btn = s.Value
		}
	}
	bwin.mu.Lock()
	defer bwin.mu.Unlock()
	switch btn {
	case "лево", "left", "лкм":
		if bwin.leftDown {
			return TRUE
		}
	case "право", "right", "пкм":
		if bwin.rightDown {
			return TRUE
		}
	}
	return FALSE
}

func browserClick(args ...Object) Object {
	if bwin == nil {
		return FALSE
	}
	btn := "лево"
	if len(args) >= 1 {
		if s, ok := args[0].(*String); ok {
			btn = s.Value
		}
	}
	bwin.mu.Lock()
	defer bwin.mu.Unlock()
	switch btn {
	case "лево", "left", "лкм":
		if bwin.leftClick {
			bwin.leftClick = false
			return TRUE
		}
	case "право", "right", "пкм":
		if bwin.rightClick {
			bwin.rightClick = false
			return TRUE
		}
	}
	return FALSE
}

func browserWheel(args ...Object) Object {
	if bwin == nil {
		return &Integer{Value: 0}
	}
	bwin.mu.Lock()
	d := bwin.wheelDelta
	bwin.wheelDelta = 0
	bwin.mu.Unlock()
	return &Integer{Value: int64(d)}
}

func browserChars(args ...Object) Object {
	if bwin == nil {
		return &String{Value: ""}
	}
	bwin.mu.Lock()
	s := string(bwin.charBuf)
	bwin.charBuf = bwin.charBuf[:0]
	bwin.mu.Unlock()
	return &String{Value: s}
}

func defaultOpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
