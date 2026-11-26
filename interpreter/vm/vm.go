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

const maxCallFrames = 1024

type callFrame struct {
	chunk *Chunk
	ip    int
	bp    int // база локальных переменных на стеке
}

// Machine — стековая VM с кадрами вызовов.
type Machine struct {
	rootChunk *Chunk
	stack     []any
	globals   []any
	frames    []callFrame
}

// New создаёт машину для корневого chunk.
func New(chunk *Chunk) *Machine {
	gLen := 0
	if chunk != nil {
		gLen = len(chunk.Names)
	}
	return &Machine{
		rootChunk: chunk,
		stack:     make([]any, 0, 256),
		globals:   make([]any, gLen),
		frames:    make([]callFrame, 0, 32),
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
			m.stack[n-2] = add(a, b)

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
			m.stack[n-2] = sub(a, b)

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
			m.stack[n-2] = numOp(a, b, func(x, y float64) float64 { return x * y })

		case OpDiv:
			b, a := m.pop(), m.pop()
			if fb, ok := asFloat(b); ok && fb == 0 {
				return fmt.Errorf("сын мой, не сотвори деления на ноль")
			}
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 && ia%ib == 0 {
					m.stack = append(m.stack, ia/ib)
					continue
				}
			}
			fa, _ := asFloat(a)
			fb, _ := asFloat(b)
			m.stack = append(m.stack, fa/fb)

		case OpIntDiv:
			b, a := m.pop(), m.pop()
			if fb, ok := asFloat(b); ok && fb == 0 {
				return fmt.Errorf("сын мой, не сотвори деления на ноль")
			}
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack = append(m.stack, ia/ib)
					continue
				}
			}
			fa, _ := asFloat(a)
			fb, _ := asFloat(b)
			m.stack = append(m.stack, int64(fa/fb))

		case OpMod:
			b, a := m.pop(), m.pop()
			if fb, ok := asFloat(b); ok && int64(fb) == 0 {
				return fmt.Errorf("сын мой, не сотвори деления на ноль")
			}
			if ia, ok1 := a.(int64); ok1 {
				if ib, ok2 := b.(int64); ok2 {
					m.stack = append(m.stack, ia%ib)
					continue
				}
			}
			m.stack = append(m.stack, numOp(a, b, func(x, y float64) float64 { return float64(int64(x) % int64(y)) }))

		case OpNeg:
			m.stack[len(m.stack)-1] = neg(m.stack[len(m.stack)-1])

		case OpNot:
			m.stack[len(m.stack)-1] = !truthy(m.stack[len(m.stack)-1])

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
			m.stack[n-2] = cmp(a, b) > 0

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
			m.stack[n-2] = cmp(a, b) >= 0

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
			m.stack[n-2] = cmp(a, b) < 0

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
			m.stack[n-2] = cmp(a, b) <= 0

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

		case OpDefineGlobal, OpSetGlobal:
			if ins.Operand >= len(m.globals) {
				ng := make([]any, ins.Operand+1)
				copy(ng, m.globals)
				m.globals = ng
			}
			m.globals[ins.Operand] = m.pop()

		case OpGetGlobal:
			if ins.Operand < len(m.globals) {
				m.stack = append(m.stack, m.globals[ins.Operand])
			} else {
				m.stack = append(m.stack, nil)
			}

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

		case OpIndex:
			idxObj := m.pop()
			arrObj := m.pop()
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

		default:
			return fmt.Errorf("неизвестный опкод %d", ins.Op)
		}
	}
	return nil
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
	default:
		return fmt.Sprint(x)
	}
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

func add(a, b any) any {
	if sa, ok := a.(string); ok {
		return sa + format(b)
	}
	if sb, ok := b.(string); ok {
		return format(a) + sb
	}
	return numOp(a, b, func(x, y float64) float64 { return x + y })
}

func sub(a, b any) any {
	return numOp(a, b, func(x, y float64) float64 { return x - y })
}

func neg(a any) any {
	if i, ok := a.(int64); ok {
		return -i
	}
	if f, ok := a.(float64); ok {
		return -f
	}
	return nil
}

func numOp(a, b any, fn func(float64, float64) float64) any {
	fa, oka := asFloat(a)
	fb, okb := asFloat(b)
	if !oka || !okb {
		return nil
	}
	_, aInt := a.(int64)
	_, bInt := b.(int64)
	r := fn(fa, fb)
	if aInt && bInt {
		return int64(r)
	}
	return r
}

func eq(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a == b
}

func cmp(a, b any) int {
	fa, oka := asFloat(a)
	fb, okb := asFloat(b)
	if !oka || !okb {
		return 0
	}
	if fa < fb {
		return -1
	}
	if fa > fb {
		return 1
	}
	return 0
}
