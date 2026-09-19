package ot

import (
	"fmt"
)

// Transform 对基于同一源文本的两个并发操作 a、b 做变换，返回
// (aAfterB, bAfterA)：
//
//	Apply(Apply(s, a), bAfterA) == Apply(Apply(s, b), aAfterB)
//
// 要求 a.SourceLen() == b.SourceLen()，否则返回 ErrLengthMismatch。
// idA、idB 必须是不同的非空 ASCII 字符串；二者在同一源位置并发插入时，
// 字典序较小的 ID 的插入排在前面。删除只作用于原始字符：不吞并对方的并发
// 插入，两个操作重叠删除的字符只删除一次。a、b 与 id 均不会被修改。
func Transform(a, b Op, idA, idB string) (Op, Op, error) {
	if err := checkID(idA); err != nil {
		return Op{}, Op{}, fmt.Errorf("ot: idA invalid: %w", err)
	}
	if err := checkID(idB); err != nil {
		return Op{}, Op{}, fmt.Errorf("ot: idB invalid: %w", err)
	}
	if idA == idB {
		return Op{}, Op{}, fmt.Errorf("%w: %q", ErrSameID, idA)
	}
	if a.SourceLen() != b.SourceLen() {
		return Op{}, Op{}, fmt.Errorf("%w: a source %d code points != b source %d",
			ErrLengthMismatch, a.SourceLen(), b.SourceLen())
	}

	ca := &tCursor{cs: a.c}
	cb := &tCursor{cs: b.c}
	var pa, pb []comp // aAfterB、bAfterA 的原始输出段

	emitA := func(c comp) { pa = append(pa, c) }
	emitB := func(c comp) { pb = append(pb, c) }

	ca.next()
	cb.next()
	for !ca.done() || !cb.done() {
		switch {
		// 同一源位置双方都插入：ID 字典序较小者在前。
		case ca.kind == InsertKind && cb.kind == InsertKind:
			la, lb := runeLen(ca.s), runeLen(cb.s)
			if idA < idB {
				// 收敛顺序：a 的插入在前。
				// aAfterB 作用于 b 的结果：先插 a 文本，再越过 b 文本；
				// bAfterA 作用于 a 的结果：先越过 a 文本，再插 b 文本。
				emitA(comp{kind: InsertKind, s: ca.s})
				emitA(comp{kind: RetainKind, n: lb})
				emitB(comp{kind: RetainKind, n: la})
				emitB(comp{kind: InsertKind, s: cb.s})
			} else {
				emitA(comp{kind: RetainKind, n: lb})
				emitA(comp{kind: InsertKind, s: ca.s})
				emitB(comp{kind: InsertKind, s: cb.s})
				emitB(comp{kind: RetainKind, n: la})
			}
			ca.advance()
			cb.advance()

		// a 单独插入：a' 保留该插入，b' 需越过这段新插入的字符（不删除它）。
		case ca.kind == InsertKind:
			emitA(comp{kind: InsertKind, s: ca.s})
			emitB(comp{kind: RetainKind, n: runeLen(ca.s)})
			ca.advance()

		// b 单独插入：对称处理。
		case cb.kind == InsertKind:
			emitA(comp{kind: RetainKind, n: runeLen(cb.s)})
			emitB(comp{kind: InsertKind, s: cb.s})
			cb.advance()

		default:
			// 两侧都作用于原始字符（Retain/Delete），按最短段对齐。
			if ca.done() || cb.done() {
				return Op{}, Op{}, fmt.Errorf("%w: transform streams desynchronized", ErrLengthMismatch)
			}
			m := ca.n
			if cb.n < m {
				m = cb.n
			}
			switch {
			case ca.kind == RetainKind && cb.kind == RetainKind:
				emitA(comp{kind: RetainKind, n: m})
				emitB(comp{kind: RetainKind, n: m})
			case ca.kind == DeleteKind && cb.kind == RetainKind:
				// a 删除的字符：a' 仍删除；b' 无需经过它们（a 已删）。
				emitA(comp{kind: DeleteKind, n: m})
			case ca.kind == RetainKind && cb.kind == DeleteKind:
				emitB(comp{kind: DeleteKind, n: m})
			default:
				// 双方都删除同一段原始字符：重叠删除只生效一次，两个变换结果都不再删。
			}
			ca.n -= m
			cb.n -= m
			if ca.n == 0 {
				ca.advance()
			}
			if cb.n == 0 {
				cb.advance()
			}
		}
	}

	aAfterB, err := normalize(pa)
	if err != nil {
		return Op{}, Op{}, err
	}
	bAfterA, err := normalize(pb)
	if err != nil {
		return Op{}, Op{}, err
	}
	// 防御性自检：aAfterB 作用在 b 的目标上，bAfterA 作用在 a 的目标上，
	// 两者目标长度一致。
	if aAfterB.SourceLen() != b.TargetLen() || bAfterA.SourceLen() != a.TargetLen() ||
		aAfterB.TargetLen() != bAfterA.TargetLen() {
		return Op{}, Op{}, fmt.Errorf("%w: transformed lengths inconsistent", ErrLengthMismatch)
	}
	return aAfterB, bAfterA, nil
}

// tCursor 是 Transform 用的顺序段游标；Insert 段保留文本，Retain/Delete 保留计数。
type tCursor struct {
	cs   []comp
	i    int
	kind Kind
	n    int
	s    string
}

func (c *tCursor) next() {
	if c.i >= len(c.cs) {
		c.kind, c.n, c.s = 0, 0, ""
		return
	}
	x := c.cs[c.i]
	c.i++
	c.kind = x.kind
	if x.kind == InsertKind {
		c.s = x.s
		c.n = runeLen(x.s)
	} else {
		c.s = ""
		c.n = x.n
	}
}

// advance 跳过当前 Insert（整段消费）；Retain/Delete 由调用方扣减 n，
// 归零后再调用 next。
func (c *tCursor) advance() { c.next() }

func (c *tCursor) done() bool { return c.i >= len(c.cs) && c.n == 0 }

// checkID 校验客户端 ID：非空且全部为 ASCII 字符。
func checkID(id string) error {
	if id == "" {
		return ErrEmptyID
	}
	for i := 0; i < len(id); i++ {
		if id[i] >= 0x80 {
			return fmt.Errorf("%w: %q", ErrInvalidID, id)
		}
	}
	return nil
}
