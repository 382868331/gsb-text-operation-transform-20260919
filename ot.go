// Package ot 是纯文本的操作变换（operational transformation）与组合内核。
//
// 一个操作 Op 是若干有序段（Component）的序列，段有三种：
//
//   - Retain(n)：保留源文本接下来的 n 个 Unicode code point（rune）；
//   - Delete(n)：删除源文本接下来的 n 个 code point；
//   - Insert(text)：在当前位置插入 text（按 code point 计数，不按字素簇）。
//
// Retain 与 Delete 的长度之和必须恰好等于源文本的 code point 数。所有长度均按
// Unicode code point 计数，组合字符不做字素簇合并。输入与单个操作的目标长度
// 上限均为 MaxCodePoints 个 code point。
package ot

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// MaxCodePoints 是输入文本与单个操作目标长度的 code point 上限。
const MaxCodePoints = 10000

// 哨兵错误，调用方可用 errors.Is 判断。
var (
	// ErrNegativeLength 表示 Retain/Delete 使用了负长度。
	ErrNegativeLength = errors.New("ot: negative component length")
	// ErrInvalidUTF8 表示插入文本或源文本不是合法 UTF-8。
	ErrInvalidUTF8 = errors.New("ot: invalid UTF-8 input")
	// ErrLengthMismatch 表示操作段没有恰好消耗期望长度（Apply/Compose/Transform）。
	ErrLengthMismatch = errors.New("ot: length mismatch")
	// ErrTooLarge 表示输入或操作目标长度超过 MaxCodePoints。
	ErrTooLarge = errors.New("ot: input or target exceeds 10000 code points")
	// ErrOverflow 表示长度算术溢出。
	ErrOverflow = errors.New("ot: length arithmetic overflow")
	// ErrUnknownKind 表示出现未定义的段类型。
	ErrUnknownKind = errors.New("ot: unknown component kind")
	// ErrEmptyID 表示 Transform 的客户端 ID 为空。
	ErrEmptyID = errors.New("ot: client id must be non-empty")
	// ErrInvalidID 表示 Transform 的客户端 ID 含非 ASCII 字符。
	ErrInvalidID = errors.New("ot: client id must be ASCII")
	// ErrSameID 表示 Transform 的两个客户端 ID 相同。
	ErrSameID = errors.New("ot: client ids must be distinct")
)

// Kind 是段类型。
type Kind uint8

const (
	// RetainKind 是 Retain 段。
	RetainKind Kind = iota + 1
	// DeleteKind 是 Delete 段。
	DeleteKind
	// InsertKind 是 Insert 段。
	InsertKind
)

// Component 是操作的一个构造段。Retain/Delete 使用 Count（code point 数），
// Insert 使用 Text。零长度 Retain/Delete 与空 Insert 会在 New 时被移除。
type Component struct {
	Kind  Kind
	Count int    // RetainKind / DeleteKind 的长度
	Text  string // InsertKind 的文本
}

// Retain 构造一个保留 n 个 code point 的段。
func Retain(n int) Component { return Component{Kind: RetainKind, Count: n} }

// Delete 构造一个删除 n 个 code point 的段。
func Delete(n int) Component { return Component{Kind: DeleteKind, Count: n} }

// Insert 构造一个插入 text 的段。
func Insert(text string) Component { return Component{Kind: InsertKind, Text: text} }

// comp 是 Op 的内部表示：规范化后不存在零长度段，也不存在相邻同类段。
type comp struct {
	kind Kind
	n    int    // Retain/Delete 长度
	s    string // Insert 文本
}

// Op 是一个经过校验和规范化的不可变操作。零值 Op 是空操作（源/目标长度均为 0）。
type Op struct {
	c   []comp
	src int // Retain+Delete 的 code point 数（源长度）
	tgt int // Retain+Insert 的 code point 数（目标长度）
}

// New 校验并规范化一组段，返回不可变 Op：
// 拒绝负长度与非法 UTF-8；移除零长度段；合并相邻同类段（相邻 Insert 拼接）；
// 校验源/目标长度不超过 MaxCodePoints。输入切片不会被逃逸或修改。
func New(components ...Component) (Op, error) {
	cs := make([]comp, 0, len(components))
	for _, c := range components {
		switch c.Kind {
		case RetainKind, DeleteKind:
			if c.Count < 0 {
				return Op{}, fmt.Errorf("%w: %d", ErrNegativeLength, c.Count)
			}
			if c.Count == 0 {
				continue
			}
			cs = append(cs, comp{kind: c.Kind, n: c.Count})
		case InsertKind:
			if !utf8.ValidString(c.Text) {
				return Op{}, ErrInvalidUTF8
			}
			if c.Text == "" {
				continue
			}
			cs = append(cs, comp{kind: InsertKind, s: c.Text})
		default:
			return Op{}, fmt.Errorf("%w: %d", ErrUnknownKind, c.Kind)
		}
	}
	return normalize(cs)
}

// normalize 合并相邻同类段、累计并校验长度。入参段均已通过基础校验。
func normalize(cs []comp) (Op, error) {
	out := make([]comp, 0, len(cs))
	srcLen, tgtLen := 0, 0

	mergeLen := func(cur, add int) (int, error) {
		v, err := addLen(cur, add)
		if err != nil {
			return 0, err
		}
		if v > MaxCodePoints {
			return 0, ErrTooLarge
		}
		return v, nil
	}

	for _, c := range cs {
		switch c.kind {
		case RetainKind, DeleteKind:
			if n := len(out); n > 0 && out[n-1].kind == c.kind {
				v, err := addLen(out[n-1].n, c.n)
				if err != nil {
					return Op{}, err
				}
				out[n-1].n = v
			} else {
				out = append(out, comp{kind: c.kind, n: c.n})
			}
			v, err := mergeLen(srcLen, c.n)
			if err != nil {
				return Op{}, err
			}
			srcLen = v
			if c.kind == RetainKind {
				v, err := mergeLen(tgtLen, c.n)
				if err != nil {
					return Op{}, err
				}
				tgtLen = v
			}
		case InsertKind:
			rn := utf8.RuneCountInString(c.s)
			if n := len(out); n > 0 && out[n-1].kind == InsertKind {
				out[n-1].s += c.s
			} else {
				out = append(out, comp{kind: InsertKind, s: c.s})
			}
			v, err := mergeLen(tgtLen, rn)
			if err != nil {
				return Op{}, err
			}
			tgtLen = v
		default:
			return Op{}, fmt.Errorf("%w: %d", ErrUnknownKind, c.kind)
		}
	}
	return Op{c: out, src: srcLen, tgt: tgtLen}, nil
}

// addLen 做带溢出检测的非负整数加法。
func addLen(x, y int) (int, error) {
	if y > 0 && x > math.MaxInt-y {
		return 0, ErrOverflow
	}
	return x + y, nil
}

// Components 返回操作段的副本，可用于检查或重新构造 Op。
func (op Op) Components() []Component {
	res := make([]Component, len(op.c))
	for i, c := range op.c {
		res[i] = Component{Kind: c.kind, Count: c.n, Text: c.s}
	}
	return res
}

// SourceLen 返回操作消耗的源长度（Retain+Delete 的 code point 数）。
func (op Op) SourceLen() int { return op.src }

// TargetLen 返回操作生成的目标长度（Retain+Insert 的 code point 数）。
func (op Op) TargetLen() int { return op.tgt }

// String 以 `Retain(n), Delete(n), Insert("text")` 的形式打印规范化结果。
func (op Op) String() string {
	var sb strings.Builder
	for i, c := range op.c {
		if i > 0 {
			sb.WriteString(", ")
		}
		switch c.kind {
		case RetainKind:
			fmt.Fprintf(&sb, "Retain(%d)", c.n)
		case DeleteKind:
			fmt.Fprintf(&sb, "Delete(%d)", c.n)
		case InsertKind:
			fmt.Fprintf(&sb, "Insert(%q)", c.s)
		}
	}
	return sb.String()
}
