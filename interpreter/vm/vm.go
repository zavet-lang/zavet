// Package vm — байткод-VM Завета.
// Флаг --vm компилирует поддерживаемые программы в байткод и исполняет их на стековой VM;
// неподдержанные конструкции автоматически откатываются к interpreter.Eval.
package vm

import (
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

// Output — куда печатает OpCallPrint (по умолчанию os.Stdout).
var Output io.Writer = os.Stdout

const maxCallFrames = 1000

type vmUnset struct{}

var unsetSentinel = vmUnset{}

type callFrame struct {
	chunk *Chunk
	ip    int
	bp    int // база локальных переменных на стеке
}

// Machine — стековая VM с кадрами вызовов.
type Machine struct {
	rootChunk    *Chunk
	stack        []any
	globals      []any
	globalsConst []bool
	frames       []callFrame
}

// New создаёт машину для корневого chunk.
func New(chunk *Chunk) *Machine {
	gLen := 0
	if chunk != nil {
		gLen = len(chunk.Names)
	}
	globals := make([]any, gLen)
	for i := range globals {
		globals[i] = unsetSentinel
	}
	return &Machine{
		rootChunk:    chunk,
		stack:        make([]any, 0, 256),
		globals:      globals,
		globalsConst: make([]bool, gLen),
		frames:       make([]callFrame, 0, 32),
	}
}

// Run выполняет байткод.
func (m *Machine) Run() error {
	if m.rootChunk == nil {
		return nil
	}
	m.frames = append(m.frames, callFrame{chunk: m.rootChunk, ip: 0, bp: 0})

	for len(m.frames) > 0 {
		frame := &m.frames[len(m.frames)-1]
		code := frame.chunk.Code
		if frame.ip >= len(code) {
			m.frames = m.frames[:len(m.frames)-1]
			continue
		}
		ins := code[frame.ip]
		frame.ip++

		switch ins.Op {
		case OpNop:
		case OpConstant:
			m.stack = append(m.stack, frame.chunk.Constants[ins.Operand])
		case OpTrue:
			m.stack = append(m.stack, true)
		case OpFalse:
			m.stack = append(m.stack, false)
		case OpNull:
			m.stack = append(m.stack, nil)
		case OpPop:
			m.stack = m.stack[:len(m.stack)-1]
		case OpDup:
			m.stack = append(m.stack, m.stack[len(m.stack)-1])

		case OpAdd:
			n := len(m.stack)
			a, b := m.stack[n-2], m.stack[n-1]
			m.stack = m.stack[:n-1]
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack[n-2] = ia + ib
					continue
				}
			}
			r, err := add(a, b)
			if err != nil {
				return err
			}
			m.stack[n-2] = r

		case OpSub:
			n := len(m.stack)
			a, b := m.stack[n-2], m.stack[n-1]
			m.stack = m.stack[:n-1]
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack[n-2] = ia - ib
					continue
				}
			}
			r, err := sub(a, b)
			if err != nil {
				return err
			}
			m.stack[n-2] = r

		case OpMul:
			n := len(m.stack)
			a, b := m.stack[n-2], m.stack[n-1]
			m.stack = m.stack[:n-1]
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack[n-2] = ia * ib
					continue
				}
			}
			r, err := numOp(a, b, "*", func(x, y float64) float64 { return x * y })
			if err != nil {
				return err
			}
			m.stack[n-2] = r

		case OpDiv:
			b, a := m.pop(), m.pop()
			fa, oka := asFloat(a)
			fb, okb := asFloat(b)
			if !oka || !okb {
				return fmt.Errorf("несоответствие типов для операции /")
			}
			if fb == 0 {
				return fmt.Errorf("сын мой, не сотвори деления на ноль")
			}
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 && ia%ib == 0 {
					m.stack = append(m.stack, ia/ib)
					continue
				}
			}
			m.stack = append(m.stack, fa/fb)

		case OpIntDiv:
			b, a := m.pop(), m.pop()
			fa, oka := asFloat(a)
			fb, okb := asFloat(b)
			if !oka || !okb {
				return fmt.Errorf("несоответствие типов для операции //")
			}
			if fb == 0 {
				return fmt.Errorf("сын мой, не сотвори деления на ноль")
			}
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack = append(m.stack, ia/ib)
					continue
				}
			}
			m.stack = append(m.stack, int64(fa/fb))

		case OpMod:
			b, a := m.pop(), m.pop()
			fa, oka := asFloat(a)
			fb, okb := asFloat(b)
			if !oka || !okb {
				return fmt.Errorf("несоответствие типов для операции %%")
			}
			if int64(fb) == 0 {
				return fmt.Errorf("сын мой, не сотвори деления на ноль")
			}
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack = append(m.stack, ia%ib)
					continue
				}
			}
			m.stack = append(m.stack, float64(int64(fa)%int64(fb)))

		case OpNeg:
			r, err := neg(m.stack[len(m.stack)-1])
			if err != nil {
				return err
			}
			m.stack[len(m.stack)-1] = r

		case OpNot:
			m.stack[len(m.stack)-1] = !truthy(m.stack[len(m.stack)-1])

		case OpToBool:
			m.stack[len(m.stack)-1] = truthy(m.stack[len(m.stack)-1])

		case OpEqual:
			n := len(m.stack)
			a, b := m.stack[n-2], m.stack[n-1]
			m.stack = m.stack[:n-1]
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack[n-2] = ia == ib
					continue
				}
			}
			m.stack[n-2] = eq(a, b)

		case OpNotEqual:
			n := len(m.stack)
			a, b := m.stack[n-2], m.stack[n-1]
			m.stack = m.stack[:n-1]
			m.stack[n-2] = !eq(a, b)

		case OpGreater:
			n := len(m.stack)
			a, b := m.stack[n-2], m.stack[n-1]
			m.stack = m.stack[:n-1]
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack[n-2] = ia > ib
					continue
				}
			}
			c, err := cmp(a, b)
			if err != nil {
				return err
			}
			m.stack[n-2] = c > 0

		case OpGreaterEq:
			n := len(m.stack)
			a, b := m.stack[n-2], m.stack[n-1]
			m.stack = m.stack[:n-1]
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack[n-2] = ia >= ib
					continue
				}
			}
			c, err := cmp(a, b)
			if err != nil {
				return err
			}
			m.stack[n-2] = c >= 0

		case OpLess:
			n := len(m.stack)
			a, b := m.stack[n-2], m.stack[n-1]
			m.stack = m.stack[:n-1]
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack[n-2] = ia < ib
					continue
				}
			}
			c, err := cmp(a, b)
			if err != nil {
				return err
			}
			m.stack[n-2] = c < 0

		case OpLessEq:
			n := len(m.stack)
			a, b := m.stack[n-2], m.stack[n-1]
			m.stack = m.stack[:n-1]
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack[n-2] = ia <= ib
					continue
				}
			}
			c, err := cmp(a, b)
			if err != nil {
				return err
			}
			m.stack[n-2] = c <= 0

		case OpJump:
			frame.ip = ins.Operand
		case OpJumpIfFalse:
			cond := m.pop()
			if !truthy(cond) {
				frame.ip = ins.Operand
			}
		case OpJumpIfTrue:
			cond := m.pop()
			if truthy(cond) {
				frame.ip = ins.Operand
			}

		case OpDefineGlobal:
			m.ensureGlobalSlot(ins.Operand)
			m.globals[ins.Operand] = m.pop()
			m.globalsConst[ins.Operand] = false

		case OpDefineGlobalConst:
			m.ensureGlobalSlot(ins.Operand)
			m.globals[ins.Operand] = m.pop()
			m.globalsConst[ins.Operand] = true

		case OpSetGlobal:
			m.ensureGlobalSlot(ins.Operand)
			if m.globals[ins.Operand] == unsetSentinel {
				return fmt.Errorf("имя «%s» не найдено в книге сотворённого", m.globalName(ins.Operand))
			}
			if m.globalsConst[ins.Operand] {
				return fmt.Errorf("сын мой, «%s» сотворено словом «пусть» и не меняется", m.globalName(ins.Operand))
			}
			m.globals[ins.Operand] = m.pop()

		case OpGetGlobal:
			if ins.Operand >= len(m.globals) || m.globals[ins.Operand] == unsetSentinel {
				return fmt.Errorf("имя «%s» не найдено в книге сотворённого", m.globalName(ins.Operand))
			}
			m.stack = append(m.stack, m.globals[ins.Operand])

		case OpGetLocal:
			m.stack = append(m.stack, m.stack[frame.bp+ins.Operand])

		case OpSetLocal:
			m.stack[frame.bp+ins.Operand] = m.pop()

		case OpBuildArray:
			cnt := ins.Operand
			elems := make([]any, cnt)
			base := len(m.stack) - cnt
			copy(elems, m.stack[base:])
			m.stack = m.stack[:base]
			m.stack = append(m.stack, elems)

		case OpBuildHash:
			cnt := ins.Operand
			base := len(m.stack) - cnt*2
			h := newVMHash(cnt)
			for i := 0; i < cnt; i++ {
				k := m.stack[base+i*2]
				v := m.stack[base+i*2+1]
				if err := h.set(k, v); err != nil {
					return err
				}
			}
			m.stack = m.stack[:base]
			m.stack = append(m.stack, h)

		case OpIndex:
			idxObj := m.pop()
			arrObj := m.pop()
			if h, ok := arrObj.(*vmHash); ok {
				v, err := h.get(idxObj)
				if err != nil {
					return err
				}
				m.stack = append(m.stack, v)
				break
			}
			idx, ok := idxObj.(int64)
			if !ok {
				return fmt.Errorf("индекс стада должен быть целым")
			}
			switch container := arrObj.(type) {
			case []any:
				if idx < 0 {
					idx = int64(len(container)) + idx
				}
				if idx < 0 || int(idx) >= len(container) {
					return fmt.Errorf("индекс %d вне пределов стана (в массиве всего %d)", idx, len(container))
				}
				m.stack = append(m.stack, container[idx])
			case string:
				runes := []rune(container)
				if idx < 0 {
					idx = int64(len(runes)) + idx
				}
				if idx < 0 || int(idx) >= len(runes) {
					return fmt.Errorf("индекс %d вне пределов строки", idx)
				}
				m.stack = append(m.stack, string(runes[idx]))
			default:
				return fmt.Errorf("индексация не сотворима для сего вида")
			}

		case OpSetIndex:
			val := m.pop()
			idxObj := m.pop()
			arrObj := m.pop()
			if h, ok := arrObj.(*vmHash); ok {
				if err := h.set(idxObj, val); err != nil {
					return err
				}
				m.stack = append(m.stack, val)
				break
			}
			arr, ok1 := arrObj.([]any)
			idx, ok2 := idxObj.(int64)
			if !ok1 || !ok2 {
				return fmt.Errorf("присвоение по индексу требует стадо и целое число")
			}
			if idx < 0 {
				idx = int64(len(arr)) + idx
			}
			if idx < 0 || int(idx) >= len(arr) {
				return fmt.Errorf("индекс %d вне пределов стана (в массиве всего %d)", idx, len(arr))
			}
			arr[idx] = val
			m.stack = append(m.stack, val)

		case OpCallLen:
			v := m.pop()
			switch x := v.(type) {
			case []any:
				m.stack = append(m.stack, int64(len(x)))
			case *vmHash:
				m.stack = append(m.stack, int64(len(x.order)))
			case string:
				m.stack = append(m.stack, int64(utf8.RuneCountInString(x)))
			default:
				return fmt.Errorf("слово «длина» не принимает сей вид")
			}

		case OpCallPrint:
			n := ins.Operand
			base := len(m.stack) - n
			for i := 0; i < n; i++ {
				if i > 0 {
					fmt.Fprint(Output, " ")
				}
				fmt.Fprint(Output, format(m.stack[base+i]))
			}
			fmt.Fprintln(Output)
			m.stack = m.stack[:base]
			m.stack = append(m.stack, nil)

		case OpCall:
			argc := ins.Operand
			fnPos := len(m.stack) - argc - 1
			fnObj := m.stack[fnPos]
			cf, ok := fnObj.(*CompiledFunc)
			if !ok {
				return fmt.Errorf("нельзя призвать: это не слово")
			}
			if len(m.frames) >= maxCallFrames {
				return fmt.Errorf("превышена глубина призываний (%d): стан переполнен", maxCallFrames)
			}
			// Локалы начинаются с fnPos+1 (аргументы уже на стеке!).
			bp := fnPos + 1
			// Дополняем стек до NumLocals нулями.
			extra := cf.NumLocals - argc
			for i := 0; i < extra; i++ {
				m.stack = append(m.stack, nil)
			}
			m.frames = append(m.frames, callFrame{
				chunk: cf.Chunk,
				ip:    0,
				bp:    bp,
			})

		case OpReturn:
			retVal := m.pop()
			bp := frame.bp
			m.frames = m.frames[:len(m.frames)-1]
			if len(m.frames) == 0 {
				return nil
			}
			// Срезаем кадр (включая слот самой функции на bp-1) и кладём результат
			m.stack = m.stack[:bp-1]
			m.stack = append(m.stack, retVal)

		case OpCheckInt:
			if len(m.stack) == 0 {
				return fmt.Errorf("сын мой, «повтори … раз» просит целое число")
			}
			if _, ok := m.stack[len(m.stack)-1].(int64); !ok {
				return fmt.Errorf("сын мой, «повтори … раз» просит целое число")
			}

		case OpError:
			msg := "ошибка выполнения"
			if ins.Operand >= 0 && ins.Operand < len(frame.chunk.Constants) {
				if s, ok := frame.chunk.Constants[ins.Operand].(string); ok {
					msg = s
				}
			}
			return fmt.Errorf("%s", msg)

		default:
			return fmt.Errorf("неизвестный опкод %d", ins.Op)
		}
	}
	return nil
}

func (m *Machine) ensureGlobalSlot(idx int) {
	if idx < len(m.globals) {
		return
	}
	oldLen := len(m.globals)
	ng := make([]any, idx+1)
	copy(ng, m.globals)
	for i := oldLen; i < len(ng); i++ {
		ng[i] = unsetSentinel
	}
	m.globals = ng
	nc := make([]bool, idx+1)
	copy(nc, m.globalsConst)
	m.globalsConst = nc
}

func (m *Machine) globalName(idx int) string {
	if m.rootChunk != nil && idx >= 0 && idx < len(m.rootChunk.Names) {
		return m.rootChunk.Names[idx]
	}
	return "?"
}

func (m *Machine) pop() any {
	n := len(m.stack)
	if n == 0 {
		return nil
	}
	v := m.stack[n-1]
	m.stack = m.stack[:n-1]
	return v
}

func truthy(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return true
}

func format(v any) string {
	if v == nil {
		return "ничего"
	}
	switch x := v.(type) {
	case bool:
		if x {
			return "истина"
		}
		return "ложь"
	case []any:
		out := "["
		for i, el := range x {
			if i > 0 {
				out += ", "
			}
			out += format(el)
		}
		return out + "]"
	case *vmHash:
		out := "{"
		for i, k := range x.order {
			if i > 0 {
				out += ", "
			}
			p := x.pairs[k]
			out += format(p.key) + ": " + format(p.val)
		}
		return out + "}"
	default:
		return fmt.Sprint(x)
	}
}

type vmHashKey struct {
	kind byte // 1=string, 2=int64, 3=bool
	s    string
	i    int64
	b    bool
}

type vmHashPair struct {
	key any
	val any
}

type vmHash struct {
	pairs map[vmHashKey]vmHashPair
	order []vmHashKey
}

func newVMHash(capHint int) *vmHash {
	return &vmHash{
		pairs: make(map[vmHashKey]vmHashPair, capHint),
		order: make([]vmHashKey, 0, capHint),
	}
}

func makeVMHashKey(k any) (vmHashKey, error) {
	switch v := k.(type) {
	case string:
		return vmHashKey{kind: 1, s: v}, nil
	case int64:
		return vmHashKey{kind: 2, i: v}, nil
	case bool:
		return vmHashKey{kind: 3, b: v}, nil
	default:
		return vmHashKey{}, fmt.Errorf("недопустимый вид ключа словаря")
	}
}

func (h *vmHash) set(k, v any) error {
	hk, err := makeVMHashKey(k)
	if err != nil {
		return err
	}
	if _, exists := h.pairs[hk]; !exists {
		h.order = append(h.order, hk)
	}
	h.pairs[hk] = vmHashPair{key: k, val: v}
	return nil
}

func (h *vmHash) get(k any) (any, error) {
	hk, err := makeVMHashKey(k)
	if err != nil {
		return nil, err
	}
	if p, ok := h.pairs[hk]; ok {
		return p.val, nil
	}
	return nil, nil
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	default:
		return 0, false
	}
}

func add(a, b any) (any, error) {
	if sa, ok := a.(string); ok {
		return sa + format(b), nil
	}
	if sb, ok := b.(string); ok {
		return format(a) + sb, nil
	}
	return numOp(a, b, "+", func(x, y float64) float64 { return x + y })
}

func sub(a, b any) (any, error) {
	return numOp(a, b, "-", func(x, y float64) float64 { return x - y })
}

func neg(a any) (any, error) {
	if i, ok := a.(int64); ok {
		return -i, nil
	}
	if f, ok := a.(float64); ok {
		return -f, nil
	}
	return nil, fmt.Errorf("унарный минус применим лишь к числам")
}

func numOp(a, b any, op string, fn func(float64, float64) float64) (any, error) {
	fa, oka := asFloat(a)
	fb, okb := asFloat(b)
	if !oka || !okb {
		return nil, fmt.Errorf("несоответствие типов для операции %s", op)
	}
	_, aInt := a.(int64)
	_, bInt := b.(int64)
	r := fn(fa, fb)
	if aInt && bInt {
		return int64(r), nil
	}
	return r, nil
}

func eq(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	// Совместимость с AST: ничего == ложь
	if (a == nil && b == false) || (a == false && b == nil) {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Числовое сравнение 1 == 1.0
	if fa, oka := asFloat(a); oka {
		if fb, okb := asFloat(b); okb {
			return fa == fb
		}
		return false
	}
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !eq(av[i], bv[i]) {
				return false
			}
		}
		return true
	case *vmHash:
		bv, ok := b.(*vmHash)
		if !ok || len(av.pairs) != len(bv.pairs) {
			return false
		}
		for k, ap := range av.pairs {
			bp, exists := bv.pairs[k]
			if !exists || !eq(ap.val, bp.val) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func cmp(a, b any) (int, error) {
	if fa, oka := asFloat(a); oka {
		if fb, okb := asFloat(b); okb {
			if fa < fb {
				return -1, nil
			}
			if fa > fb {
				return 1, nil
			}
			return 0, nil
		}
		return 0, fmt.Errorf("нельзя сравнить число с другим видом")
	}
	if sa, oka := a.(string); oka {
		if sb, okb := b.(string); okb {
			if sa < sb {
				return -1, nil
			}
			if sa > sb {
				return 1, nil
			}
			return 0, nil
		}
		return 0, fmt.Errorf("нельзя сравнить строку с другим видом")
	}
	return 0, fmt.Errorf("сравнение не сотворимо для сих видов")
}
