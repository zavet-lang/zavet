//go:build !windows

package interpreter

import (
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

type browserWin struct {
	mu         sync.Mutex
	w, h       int
	pixels     []byte // BGRA
	keys       map[string]bool
	open       bool
	title      string
	addr       string
	mouseX     int
	mouseY     int
	leftDown   bool
	rightDown  bool
	leftClick  bool
	rightClick bool
	wheelDelta int
	charBuf    []rune
}

var bwin *browserWin

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
			if bwin != nil && bwin.open {
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
	for c := 'A'; c <= 'Z'; c++ {
		pixelFont[c] = pixelFont[c-'A'+'a']
	}
}

func browserCreate(args ...Object) Object {
	if len(args) < 2 {
		return ErrorWithHint(currentCallToken, "окно.создать(ширина, высота [, заголовок])", "")
	}
	w, ok1 := args[0].(*Integer)
	h, ok2 := args[1].(*Integer)
	if !ok1 || !ok2 {
		return ErrorWithHint(currentCallToken, "ширина и высота — целые", "")
	}
	title := "Завет"
	if len(args) >= 3 {
		if s, ok := args[2].(*String); ok {
			title = s.Value
		}
	}
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
	go http.Serve(ln, mux)
	url := "http://" + bw.addr + "/"
	_ = openBrowser(url)
	return NULL
}

func (bw *browserWin) serveHTML(w http.ResponseWriter, r *http.Request) {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%s</title>
<style>body{margin:0;background:#111;display:flex;justify-content:center;align-items:center;height:100vh;overflow:hidden}
canvas{image-rendering:pixelated;border:1px solid #333;cursor:crosshair}</style></head>
<body><canvas id="c" width="%d" height="%d" tabindex="0"></canvas>
<script>
const c=document.getElementById('c'),x=c.getContext('2d');
c.focus();
const state={keys:{},mx:0,my:0,left:false,right:false,lclick:false,rclick:false,wheel:0,chars:''};
const mapKey=k=>{
  k=k.toLowerCase();
  if(k===' ')return ' ';
  return k;
};
c.onmousemove=e=>{const r=c.getBoundingClientRect();state.mx=Math.floor((e.clientX-r.left)*c.width/r.width);state.my=Math.floor((e.clientY-r.top)*c.height/r.height)};
c.onmousedown=e=>{if(e.button===0){state.left=true;state.lclick=true}if(e.button===2){state.right=true;state.rclick=true};e.preventDefault()};
c.onmouseup=e=>{if(e.button===0)state.left=false;if(e.button===2)state.right=false};
c.oncontextmenu=e=>e.preventDefault();
c.onwheel=e=>{state.wheel+=e.deltaY>0?-1:1;e.preventDefault()};
window.onkeydown=e=>{
  state.keys[mapKey(e.key)]=true;
  if(e.key==='Backspace'){state.chars+='\b';e.preventDefault();return}
  if(e.key.length===1 && !e.ctrlKey && !e.metaKey){state.chars+=e.key;e.preventDefault()}
  if(['ArrowUp','ArrowDown','ArrowLeft','ArrowRight','Tab',' '].includes(e.key))e.preventDefault();
};
window.onkeyup=e=>{state.keys[mapKey(e.key)]=false};
window.onblur=()=>{for(const k in state.keys)state.keys[k]=false;state.left=false;state.right=false};
async function tick(){
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
    await fetch('/input',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});
  }catch(e){}
  requestAnimationFrame(tick);
}
tick();
</script></body></html>`, htmlEscape(bw.title), bw.w, bw.h)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func (bw *browserWin) servePNG(w http.ResponseWriter, r *http.Request) {
	bw.mu.Lock()
	img := image.NewRGBA(image.Rect(0, 0, bw.w, bw.h))
	for y := 0; y < bw.h; y++ {
		for x := 0; x < bw.w; x++ {
			i := (y*bw.w + x) * 4
			img.Set(x, y, color.RGBA{R: bw.pixels[i+2], G: bw.pixels[i+1], B: bw.pixels[i], A: 255})
		}
	}
	bw.mu.Unlock()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_ = png.Encode(w, img)
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
	bw.mu.Unlock()
	w.WriteHeader(204)
}

func browserClear(args ...Object) Object {
	if bwin == nil {
		return NULL
	}
	r, g, b := int64(0), int64(0), int64(0)
	if len(args) >= 3 {
		if i, ok := args[0].(*Integer); ok {
			r = i.Value
		}
		if i, ok := args[1].(*Integer); ok {
			g = i.Value
		}
		if i, ok := args[2].(*Integer); ok {
			b = i.Value
		}
	}
	bwin.mu.Lock()
	for i := 0; i < len(bwin.pixels); i += 4 {
		bwin.pixels[i] = byte(b)
		bwin.pixels[i+1] = byte(g)
		bwin.pixels[i+2] = byte(r)
		bwin.pixels[i+3] = 255
	}
	bwin.mu.Unlock()
	return NULL
}

func browserRect(args ...Object) Object {
	if bwin == nil || len(args) < 7 {
		return ErrorWithHint(currentCallToken, "прямоугольник(x,y,w,h,r,g,b)", "")
	}
	xi := int(args[0].(*Integer).Value)
	yi := int(args[1].(*Integer).Value)
	ww := int(args[2].(*Integer).Value)
	hh := int(args[3].(*Integer).Value)
	rr := byte(args[4].(*Integer).Value)
	gg := byte(args[5].(*Integer).Value)
	bb := byte(args[6].(*Integer).Value)
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
	// Pixel font 5x7 for ASCII — enough for URL bar on non-Windows.
	if bwin == nil || len(args) < 6 {
		return NULL
	}
	x0 := int(args[0].(*Integer).Value)
	y0 := int(args[1].(*Integer).Value)
	s, ok := args[2].(*String)
	if !ok {
		return NULL
	}
	rr := byte(args[3].(*Integer).Value)
	gg := byte(args[4].(*Integer).Value)
	bb := byte(args[5].(*Integer).Value)
	bwin.mu.Lock()
	cx := x0
	for _, ch := range s.Value {
		glyph := pixelFont[ch]
		if glyph == nil {
			glyph = pixelFont['?']
		}
		for row := 0; row < 7; row++ {
			bits := glyph[row]
			for col := 0; col < 5; col++ {
				if bits&(1<<(4-col)) != 0 {
					px, py := cx+col, y0+row
					if px >= 0 && py >= 0 && px < bwin.w && py < bwin.h {
						i := (py*bwin.w + px) * 4
						bwin.pixels[i] = bb
						bwin.pixels[i+1] = gg
						bwin.pixels[i+2] = rr
						bwin.pixels[i+3] = 255
					}
				}
			}
		}
		cx += 6
	}
	bwin.mu.Unlock()
	return NULL
}

func browserIcon(args ...Object) Object {
	// На non-Windows — заглушка: маленький квадрат вместо FA-глифа.
	if bwin == nil || len(args) < 6 {
		return NULL
	}
	x0, ok0 := args[0].(*Integer)
	y0, ok1 := args[1].(*Integer)
	name, ok2 := args[2].(*String)
	if !ok0 || !ok1 || !ok2 {
		return NULL
	}
	if _, ok := faIconRune(name.Value); !ok {
		return ErrorWithHint(currentCallToken, "неизвестная иконка: "+name.Value,
			"См. окно.список_иконок() — Font Awesome Solid")
	}
	rr := byte(args[3].(*Integer).Value)
	gg := byte(args[4].(*Integer).Value)
	bb := byte(args[5].(*Integer).Value)
	bwin.mu.Lock()
	for dy := 0; dy < 12; dy++ {
		for dx := 0; dx < 12; dx++ {
			if (dx > 1 && dx < 10 && dy > 1 && dy < 10) || dx == 0 || dy == 0 || dx == 11 || dy == 11 {
				px, py := int(x0.Value)+dx, int(y0.Value)+dy
				if px >= 0 && py >= 0 && px < bwin.w && py < bwin.h {
					i := (py*bwin.w + px) * 4
					bwin.pixels[i] = bb
					bwin.pixels[i+1] = gg
					bwin.pixels[i+2] = rr
					bwin.pixels[i+3] = 255
				}
			}
		}
	}
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
	if bwin == nil || len(args) < 1 {
		return NULL
	}
	if s, ok := args[0].(*String); ok {
		bwin.mu.Lock()
		bwin.title = s.Value
		bwin.mu.Unlock()
	}
	return NULL
}

func browserKey(args ...Object) Object {
	if bwin == nil || len(args) < 1 {
		return FALSE
	}
	name, ok := args[0].(*String)
	if !ok {
		return FALSE
	}
	key := normalizeBrowserKey(name.Value)
	bwin.mu.Lock()
	pressed := bwin.keys[key]
	bwin.mu.Unlock()
	if pressed {
		return TRUE
	}
	return FALSE
}

func normalizeBrowserKey(k string) string {
	k = strings.ToLower(k)
	switch k {
	case "влево", "left":
		return "arrowleft"
	case "вправо", "right":
		return "arrowright"
	case "вверх", "up":
		return "arrowup"
	case "вниз", "down":
		return "arrowdown"
	case "пробел", "space":
		return " "
	case "escape", "esc", "выход":
		return "escape"
	case "enter", "ввод":
		return "enter"
	case "backspace", "back", "стереть":
		return "backspace"
	case "tab", "таб":
		return "tab"
	case "delete", "del", "удалить":
		return "delete"
	case "ctrl", "control", "контроль":
		return "control"
	case "f5":
		return "f5"
	default:
		return k
	}
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
	ms := int64(16)
	if len(args) >= 1 {
		if i, ok := args[0].(*Integer); ok {
			ms = i.Value
		}
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return NULL
}

func browserClose(args ...Object) Object {
	if bwin != nil {
		bwin.mu.Lock()
		bwin.open = false
		bwin.mu.Unlock()
	}
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

func openBrowser(url string) error {
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

// Minimal 5x7 glyphs for URL bar / labels on non-Windows.
var pixelFont = map[rune][]byte{
	' ': {0, 0, 0, 0, 0, 0, 0},
	'!': {0x04, 0x04, 0x04, 0x04, 0x00, 0x04, 0x00},
	'.': {0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x00},
	'/': {0x01, 0x02, 0x04, 0x08, 0x10, 0x00, 0x00},
	':': {0x00, 0x04, 0x00, 0x00, 0x04, 0x00, 0x00},
	'?': {0x0E, 0x11, 0x02, 0x04, 0x00, 0x04, 0x00},
	'-': {0x00, 0x00, 0x00, 0x1F, 0x00, 0x00, 0x00},
	'_': {0x00, 0x00, 0x00, 0x00, 0x00, 0x1F, 0x00},
	'0': {0x0E, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0E},
	'1': {0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E},
	'2': {0x0E, 0x11, 0x01, 0x06, 0x08, 0x10, 0x1F},
	'3': {0x1F, 0x02, 0x04, 0x02, 0x01, 0x11, 0x0E},
	'4': {0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02},
	'5': {0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E},
	'6': {0x06, 0x08, 0x10, 0x1E, 0x11, 0x11, 0x0E},
	'7': {0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08},
	'8': {0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E},
	'9': {0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C},
	'a': {0x00, 0x00, 0x0E, 0x01, 0x0F, 0x11, 0x0F},
	'b': {0x10, 0x10, 0x1E, 0x11, 0x11, 0x11, 0x1E},
	'c': {0x00, 0x00, 0x0E, 0x11, 0x10, 0x11, 0x0E},
	'd': {0x01, 0x01, 0x0F, 0x11, 0x11, 0x11, 0x0F},
	'e': {0x00, 0x00, 0x0E, 0x11, 0x1F, 0x10, 0x0E},
	'f': {0x06, 0x09, 0x08, 0x1C, 0x08, 0x08, 0x08},
	'g': {0x00, 0x0F, 0x11, 0x0F, 0x01, 0x11, 0x0E},
	'h': {0x10, 0x10, 0x16, 0x19, 0x11, 0x11, 0x11},
	'i': {0x04, 0x00, 0x0C, 0x04, 0x04, 0x04, 0x0E},
	'j': {0x02, 0x00, 0x06, 0x02, 0x02, 0x12, 0x0C},
	'k': {0x10, 0x10, 0x12, 0x14, 0x18, 0x14, 0x12},
	'l': {0x0C, 0x04, 0x04, 0x04, 0x04, 0x04, 0x0E},
	'm': {0x00, 0x00, 0x1A, 0x15, 0x15, 0x11, 0x11},
	'n': {0x00, 0x00, 0x16, 0x19, 0x11, 0x11, 0x11},
	'o': {0x00, 0x00, 0x0E, 0x11, 0x11, 0x11, 0x0E},
	'p': {0x00, 0x00, 0x1E, 0x11, 0x1E, 0x10, 0x10},
	'q': {0x00, 0x00, 0x0F, 0x11, 0x0F, 0x01, 0x01},
	'r': {0x00, 0x00, 0x16, 0x19, 0x10, 0x10, 0x10},
	's': {0x00, 0x00, 0x0F, 0x10, 0x0E, 0x01, 0x1E},
	't': {0x08, 0x08, 0x1C, 0x08, 0x08, 0x09, 0x06},
	'u': {0x00, 0x00, 0x11, 0x11, 0x11, 0x13, 0x0D},
	'v': {0x00, 0x00, 0x11, 0x11, 0x11, 0x0A, 0x04},
	'w': {0x00, 0x00, 0x11, 0x11, 0x15, 0x15, 0x0A},
	'x': {0x00, 0x00, 0x11, 0x0A, 0x04, 0x0A, 0x11},
	'y': {0x00, 0x00, 0x11, 0x11, 0x0F, 0x01, 0x0E},
	'z': {0x00, 0x00, 0x1F, 0x02, 0x04, 0x08, 0x1F},
}
