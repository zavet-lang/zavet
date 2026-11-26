package vm

// Opcode — код операции байткод-VM Завета.
type Opcode byte

const (
	OpNop Opcode = iota
	OpConstant
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpIntDiv
	OpMod
	OpNeg
	OpNot
	OpPop
	OpDup
	OpTrue
	OpFalse
	OpNull
	OpEqual
	OpNotEqual
	OpGreater
	OpGreaterEq
	OpLess
	OpLessEq
	OpJump
	OpJumpIfFalse
	OpJumpIfTrue
	OpCallPrint // возгласи: pop N args, print
	OpCallLen   // длина(x)
	OpCall      // вызов скомпилированного слова: Operand = argc
	OpReturn
	OpGetGlobal
	OpSetGlobal
	OpDefineGlobal
	OpDefineGlobalConst
	OpGetLocal
	OpSetLocal
	OpBuildArray // Operand = count
	OpBuildHash  // Operand = pair count (2*count values on stack: k1, v1, ...)
	OpIndex      // stack: arr/hash, idx -> elem
	OpSetIndex   // stack: arr/hash, idx, val -> val
	OpCheckInt   // проверяет что вершина стека — int64
	OpToBool     // приводит вершину стека к bool через truthy
	OpError      // возвращает ошибку с текстом Constants[Operand]
)

// Instruction — одна инструкция байткода.
type Instruction struct {
	Op      Opcode
	Operand int
}

// CompiledFunc — скомпилированное слово (функция).
type CompiledFunc struct {
	Name      string
	NumParams int
	NumLocals int
	Chunk     *Chunk
}

// Chunk — скомпилированный байткод.
type Chunk struct {
	Code       []Instruction
	Constants  []any
	Names      []string // имена глобальных
	SourceFile string
}

func (c *Chunk) emit(op Opcode, operand int) int {
	c.Code = append(c.Code, Instruction{Op: op, Operand: operand})
	return len(c.Code) - 1
}

func (c *Chunk) addConstant(v any) int {
	c.Constants = append(c.Constants, v)
	return len(c.Constants) - 1
}

func (c *Chunk) nameIndex(name string) int {
	for i, n := range c.Names {
		if n == name {
			return i
		}
	}
	c.Names = append(c.Names, name)
	return len(c.Names) - 1
}

func (c *Chunk) patch(idx, operand int) {
	c.Code[idx].Operand = operand
}
