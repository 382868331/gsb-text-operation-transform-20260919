// Package ot 实现纯文本编辑的操作变换（Operational Transformation）内核。
//
// 操作由 Retain(n)、Delete(n)、Insert(text) 三种段组成，长度按 Unicode
// code point 计数（不按字素簇合并组合字符）。提供：
//
//	Apply(source, op)      将操作应用到源文本
//	Invert(source, op)     构造逆操作，Apply(Apply(s, op), Invert(s, op)) == s
//	Compose(a, b)          组合为等价于先 a 后 b 的单个操作
//	Transform(a, b, idA, idB) 对同源并发操作做变换，保证两端收敛
//
// 所有导出的函数都不修改输入；长度算术做溢出检测；输入文本与单操作
// 目标长度上限为 MaxLen 个 code point。
package ot

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// MaxLen 是输入源文本与单操作源/目标长度的上限（code point 数）。
const MaxLen = 10000

var (
	ErrNegativeLength = errors.New("ot: negative segment length")
	ErrInvalidUTF8    = errors.New("ot: invalid UTF-8")
	ErrLengthMismatch = errors.New("ot: length mismatch")
	ErrOverflow       = errors.New("ot: length arithmetic overflow")
	ErrTooLong        = errors.New("ot: length exceeds MaxLen")
	ErrBadID          = errors.New("ot: transform IDs must be distinct non-empty ASCII strings")
	ErrBadSegment     = errors.New("ot: unknown segment kind")
	ErrUnbalanced     = errors.New("ot: internal error: unbalanced operation pair")
)

// Kind 是段的类型。
type Kind int

const (
	Retain Kind = iota // 保留源文本 n 个 code point
	Delete             // 删除源文本 n 个 code point
	Insert             // 插入文本（不消耗源）
)

func (k Kind) String() string {
	switch k {
	case Retain:
		return "Retain"
	case Delete:
		return "Delete"
	case Insert:
		return "Insert"
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Segment 是操作的一段。Retain/Delete 使用 N（code point 数），Insert 使用 Text。
type Segment struct {
	Kind Kind
	N    int
	Text string
}

// Op 是一次编辑操作，由若干段顺序组成。
type Op []Segment

// R、D、I 是构造段的便捷函数。
func R(n int) Segment       { return Segment{Kind: Retain, N: n} }
func D(n int) Segment       { return Segment{Kind: Delete, N: n} }
func I(text string) Segment { return Segment{Kind: Insert, Text: text} }

func (op Op) String() string {
	var b strings.Builder
	b.WriteByte('[')
	for i, seg := range op {
		if i > 0 {
			b.WriteByte(' ')
		}
		switch seg.Kind {
		case Retain:
			fmt.Fprintf(&b, "Retain(%d)", seg.N)
		case Delete:
			fmt.Fprintf(&b, "Delete(%d)", seg.N)
		case Insert:
			fmt.Fprintf(&b, "Insert(%q)", seg.Text)
		default:
			fmt.Fprintf(&b, "Kind(%d)", int(seg.Kind))
		}
	}
	b.WriteByte(']')
	return b.String()
}

// checkedAdd 做带溢出检测的非负整数加法。
func checkedAdd(a, b int) (int, error) {
	if a < 0 || b < 0 {
		return 0, ErrNegativeLength
	}
	if a > math.MaxInt-b {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// emit 向操作末尾追加一段，相邻同类段合并；合并时检测长度溢出。
func emit(op Op, seg Segment) (Op, error) {
	if len(op) > 0 {
		last := &op[len(op)-1]
		if last.Kind == seg.Kind {
			switch seg.Kind {
			case Retain, Delete:
				n, err := checkedAdd(last.N, seg.N)
				if err != nil {
					return nil, err
				}
				last.N = n
				return op, nil
			case Insert:
				last.Text += seg.Text
				return op, nil
			}
		}
	}
	return append(op, seg), nil
}

// Normalize 校验并规范化操作：拒绝负长度与非法 UTF-8，移除零长度段，
// 合并相邻同类段。输入 op 不被修改，返回新切片。
func Normalize(op Op) (Op, error) {
	out := make(Op, 0, len(op))
	var err error
	for _, seg := range op {
		switch seg.Kind {
		case Retain, Delete:
			if seg.N < 0 {
				return nil, fmt.Errorf("%w: %s(%d)", ErrNegativeLength, seg.Kind, seg.N)
			}
			if seg.N == 0 {
				continue
			}
			out, err = emit(out, Segment{Kind: seg.Kind, N: seg.N})
		case Insert:
			if !utf8.ValidString(seg.Text) {
				return nil, fmt.Errorf("%w in Insert text", ErrInvalidUTF8)
			}
			if seg.Text == "" {
				continue
			}
			out, err = emit(out, Segment{Kind: Insert, Text: seg.Text})
		default:
			return nil, fmt.Errorf("%w: %d", ErrBadSegment, int(seg.Kind))
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SourceLen 返回操作消耗的源文本长度（code point 数），带溢出检测。
func SourceLen(op Op) (int, error) {
	total := 0
	for _, seg := range op {
		if seg.Kind == Retain || seg.Kind == Delete {
			var err error
			total, err = checkedAdd(total, seg.N)
			if err != nil {
				return 0, err
			}
		}
	}
	return total, nil
}

// TargetLen 返回操作产生的目标文本长度（code point 数），带溢出检测。
func TargetLen(op Op) (int, error) {
	total := 0
	for _, seg := range op {
		var n int
		switch seg.Kind {
		case Retain:
			n = seg.N
		case Insert:
			n = utf8.RuneCountInString(seg.Text)
		default:
			continue
		}
		var err error
		total, err = checkedAdd(total, n)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func checkLimit(what string, n int) error {
	if n > MaxLen {
		return fmt.Errorf("%w: %s is %d code points, max %d", ErrTooLong, what, n, MaxLen)
	}
	return nil
}

// prepare 校验并规范化操作，返回规范化结果及源/目标长度（均已检查上限）。
func prepare(op Op) (norm Op, srcLen, tgtLen int, err error) {
	norm, err = Normalize(op)
	if err != nil {
		return nil, 0, 0, err
	}
	srcLen, err = SourceLen(norm)
	if err != nil {
		return nil, 0, 0, err
	}
	if err = checkLimit("op source length", srcLen); err != nil {
		return nil, 0, 0, err
	}
	tgtLen, err = TargetLen(norm)
	if err != nil {
		return nil, 0, 0, err
	}
	if err = checkLimit("op target length", tgtLen); err != nil {
		return nil, 0, 0, err
	}
	return norm, srcLen, tgtLen, nil
}

// validSource 校验源文本：合法 UTF-8 且长度不超限，返回其 code point 切片。
func validSource(source string) ([]rune, error) {
	if !utf8.ValidString(source) {
		return nil, fmt.Errorf("%w in source", ErrInvalidUTF8)
	}
	src := []rune(source)
	if err := checkLimit("source length", len(src)); err != nil {
		return nil, err
	}
	return src, nil
}

// Apply 将操作应用到源文本。操作必须恰好消耗源长度，否则返回
// ErrLengthMismatch。输入均不被修改。
func Apply(source string, op Op) (string, error) {
	src, err := validSource(source)
	if err != nil {
		return "", err
	}
	nop, srcLen, _, err := prepare(op)
	if err != nil {
		return "", err
	}
	if srcLen != len(src) {
		return "", fmt.Errorf("%w: op consumes %d code points, source has %d",
			ErrLengthMismatch, srcLen, len(src))
	}
	var b strings.Builder
	pos := 0
	for _, seg := range nop {
		switch seg.Kind {
		case Retain:
			b.WriteString(string(src[pos : pos+seg.N]))
			pos += seg.N
		case Delete:
			pos += seg.N
		case Insert:
			b.WriteString(seg.Text)
		}
	}
	return b.String(), nil
}

// Invert 构造 op 相对 source 的逆操作：
// Apply(Apply(source, op), Invert(source, op)) == source。
func Invert(source string, op Op) (Op, error) {
	src, err := validSource(source)
	if err != nil {
		return nil, err
	}
	nop, srcLen, _, err := prepare(op)
	if err != nil {
		return nil, err
	}
	if srcLen != len(src) {
		return nil, fmt.Errorf("%w: op consumes %d code points, source has %d",
			ErrLengthMismatch, srcLen, len(src))
	}
	out := make(Op, 0, len(nop))
	pos := 0
	for _, seg := range nop {
		switch seg.Kind {
		case Retain:
			out, err = emit(out, R(seg.N))
			pos += seg.N
		case Delete:
			out, err = emit(out, I(string(src[pos:pos+seg.N])))
			pos += seg.N
		case Insert:
			out, err = emit(out, D(utf8.RuneCountInString(seg.Text)))
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// splitRunes 在第 k 个 code point 处切分字符串（k 必须不超过 s 的 code point 数）。
func splitRunes(s string, k int) (head, tail string) {
	idx := 0
	for c := 0; c < k; c++ {
		_, size := utf8.DecodeRuneInString(s[idx:])
		idx += size
	}
	return s[:idx], s[idx:]
}

// Compose 组合两个操作为等价于“先 a 后 b”的单个操作。
// 要求 a 的目标长度等于 b 的源长度，否则返回 ErrLengthMismatch。
// 不借助源文本，输入均不被修改。
func Compose(a, b Op) (Op, error) {
	na, _, ta, err := prepare(a)
	if err != nil {
		return nil, err
	}
	nb, sb, _, err := prepare(b)
	if err != nil {
		return nil, err
	}
	if ta != sb {
		return nil, fmt.Errorf("%w: first op target length %d, second op source length %d",
			ErrLengthMismatch, ta, sb)
	}
	out := make(Op, 0, len(na)+len(nb))
	i, j := 0, 0
	var curA, curB Segment
	haveA, haveB := false, false
	for {
		if !haveA && i < len(na) {
			curA = na[i]
			i++
			haveA = true
		}
		if !haveB && j < len(nb) {
			curB = nb[j]
			j++
			haveB = true
		}
		if !haveA && !haveB {
			break
		}
		// b 的插入不消耗 a 的输出，直接透传。
		if haveB && curB.Kind == Insert {
			out, err = emit(out, curB)
			if err != nil {
				return nil, err
			}
			haveB = false
			continue
		}
		// a 的删除不被 b 修改，直接透传。
		if haveA && curA.Kind == Delete {
			out, err = emit(out, curA)
			if err != nil {
				return nil, err
			}
			haveA = false
			continue
		}
		if !haveA || !haveB {
			return nil, ErrUnbalanced
		}
		// curA 为 Retain/Insert，curB 为 Retain/Delete。
		aN := curA.N
		if curA.Kind == Insert {
			aN = utf8.RuneCountInString(curA.Text)
		}
		n := min(aN, curB.N)
		switch {
		case curA.Kind == Retain && curB.Kind == Retain:
			out, err = emit(out, R(n))
		case curA.Kind == Retain && curB.Kind == Delete:
			out, err = emit(out, D(n))
		case curA.Kind == Insert && curB.Kind == Retain:
			head, tail := splitRunes(curA.Text, n)
			out, err = emit(out, I(head))
			curA.Text = tail
		default: // a 插入的内容随即被 b 删除，两侧都不产出
			_, tail := splitRunes(curA.Text, n)
			curA.Text = tail
		}
		if err != nil {
			return nil, err
		}
		if curA.Kind == Insert {
			if curA.Text == "" {
				haveA = false
			}
		} else {
			curA.N -= n
			if curA.N == 0 {
				haveA = false
			}
		}
		curB.N -= n
		if curB.N == 0 {
			haveB = false
		}
	}
	return out, nil
}

// checkIDs 校验变换双方 ID：非空、纯 ASCII、互不相同。
func checkIDs(idA, idB string) error {
	if idA == "" || idB == "" || idA == idB {
		return ErrBadID
	}
	for i := 0; i < len(idA); i++ {
		if idA[i] > 127 {
			return ErrBadID
		}
	}
	for i := 0; i < len(idB); i++ {
		if idB[i] > 127 {
			return ErrBadID
		}
	}
	return nil
}

// Transform 对两个基于同一源文本的并发操作做变换，返回
//
//	aAfterB：a 相对 b 变换后的操作，作用于 Apply(s, b) 的结果
//	bAfterA：b 相对 a 变换后的操作，作用于 Apply(s, a) 的结果
//
// 保证 Apply(Apply(s,a),bAfterA) == Apply(Apply(s,b),aAfterB)。
// 同源位置的并发插入按 ID 字典序排序，较小者先插入；删除只作用于
// 原字符，不吞并并发插入；重叠删除只生效一次。
func Transform(a, b Op, idA, idB string) (aAfterB, bAfterA Op, err error) {
	if err := checkIDs(idA, idB); err != nil {
		return nil, nil, err
	}
	na, sa, _, err := prepare(a)
	if err != nil {
		return nil, nil, err
	}
	nb, sb, _, err := prepare(b)
	if err != nil {
		return nil, nil, err
	}
	if sa != sb {
		return nil, nil, fmt.Errorf("%w: source lengths %d vs %d", ErrLengthMismatch, sa, sb)
	}
	aAfterB = make(Op, 0, len(na))
	bAfterA = make(Op, 0, len(nb))
	i, j := 0, 0
	var curA, curB Segment
	haveA, haveB := false, false
	for {
		if !haveA && i < len(na) {
			curA = na[i]
			i++
			haveA = true
		}
		if !haveB && j < len(nb) {
			curB = nb[j]
			j++
			haveB = true
		}
		if !haveA && !haveB {
			break
		}
		aIns := haveA && curA.Kind == Insert
		bIns := haveB && curB.Kind == Insert
		// 双方同时在同源位置插入时，ID 字典序较小者先插入。
		if aIns && (!bIns || idA < idB) {
			aAfterB, err = emit(aAfterB, I(curA.Text))
			if err != nil {
				return nil, nil, err
			}
			bAfterA, err = emit(bAfterA, R(utf8.RuneCountInString(curA.Text)))
			if err != nil {
				return nil, nil, err
			}
			haveA = false
			continue
		}
		if bIns {
			aAfterB, err = emit(aAfterB, R(utf8.RuneCountInString(curB.Text)))
			if err != nil {
				return nil, nil, err
			}
			bAfterA, err = emit(bAfterA, I(curB.Text))
			if err != nil {
				return nil, nil, err
			}
			haveB = false
			continue
		}
		if !haveA || !haveB {
			return nil, nil, ErrUnbalanced
		}
		n := min(curA.N, curB.N)
		switch {
		case curA.Kind == Delete && curB.Kind == Delete:
			// 重叠删除只生效一次：两侧都不再产出。
		case curA.Kind == Delete:
			aAfterB, err = emit(aAfterB, D(n))
		case curB.Kind == Delete:
			bAfterA, err = emit(bAfterA, D(n))
		default:
			aAfterB, err = emit(aAfterB, R(n))
			bAfterA, err = emit(bAfterA, R(n))
		}
		if err != nil {
			return nil, nil, err
		}
		curA.N -= n
		curB.N -= n
		if curA.N == 0 {
			haveA = false
		}
		if curB.N == 0 {
			haveB = false
		}
	}
	return aAfterB, bAfterA, nil
}
