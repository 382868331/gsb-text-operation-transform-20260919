package ot

import (
	"fmt"
)

// cursor 顺序读取一个 Op 的段，并把 Insert 文本折算为 n 个 code point，
// 从而让两个操作可以按字符流对齐消费。
type cursor struct {
	cs   []comp
	i    int
	kind Kind
	n    int
	s    string
}

func (c *cursor) load() {
	for c.n == 0 && c.i < len(c.cs) {
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
	// 耗尽后清除状态，避免残留的 kind 被上层分支误判（否则会空转）。
	if c.n == 0 && c.i >= len(c.cs) {
		c.kind = 0
		c.s = ""
	}
}

func (c *cursor) done() bool { return c.n == 0 && c.i >= len(c.cs) }

// Compose 返回组合操作 c，语义等价于“先执行 a，再执行 b”：
//
//	Apply(Apply(s, a), b) == Apply(s, c)
//
// 要求 a.TargetLen() == b.SourceLen()，否则返回 ErrLengthMismatch。
// 组合过程不需要任何源文本；a、b 均保持不变。内部长度算术带溢出检测。
func Compose(a, b Op) (Op, error) {
	if a.TargetLen() != b.SourceLen() {
		return Op{}, fmt.Errorf("%w: a target %d code points != b source %d",
			ErrLengthMismatch, a.TargetLen(), b.SourceLen())
	}

	ca := &cursor{cs: a.c}
	cb := &cursor{cs: b.c}
	var out []comp

	emit := func(c comp) error {
		if n := len(out); n > 0 && out[n-1].kind == c.kind {
			if c.kind == InsertKind {
				out[n-1].s += c.s
				return nil
			}
			v, err := addLen(out[n-1].n, c.n)
			if err != nil {
				return err
			}
			if v > MaxCodePoints {
				return ErrTooLarge
			}
			out[n-1].n = v
			return nil
		}
		out = append(out, c)
		return nil
	}

	ca.load()
	cb.load()
	for !ca.done() || !cb.done() {
		switch {
		// 两侧在当前点都插入：目标文本中 b 的插入位于 a 的插入之前。
		case ca.kind == InsertKind && cb.kind == InsertKind:
			if err := emit(comp{kind: InsertKind, s: cb.s}); err != nil {
				return Op{}, err
			}
			cb.n = 0
			cb.load()

		// a 插入的字符被 b 删除：丢弃 a 插入文本的相应部分（可能只删一部分）。
		case ca.kind == InsertKind && cb.kind == DeleteKind:
			m := ca.n
			if cb.n < m {
				m = cb.n
			}
			ca.s = dropRunes(ca.s, m)
			ca.n -= m
			cb.n -= m
			if ca.n == 0 {
				ca.load()
			}
			if cb.n == 0 {
				cb.load()
			}

		// a 插入的字符被 b 保留：成为组合操作的插入；b 同步消耗这些目标字符。
		case ca.kind == InsertKind:
			m := ca.n
			if !cb.done() && cb.n < m {
				m = cb.n
			}
			text := ca.s
			if m < ca.n {
				text = takePrefixRunes(ca.s, m)
				ca.s = dropRunes(ca.s, m)
			} else {
				ca.s = ""
			}
			if err := emit(comp{kind: InsertKind, s: text}); err != nil {
				return Op{}, err
			}
			ca.n -= m
			if !cb.done() {
				cb.n -= m
				if cb.n == 0 {
					cb.load()
				}
			}
			if ca.n == 0 {
				ca.load()
			}

		// b 在当前点插入（a 此处不是插入）。
		case cb.kind == InsertKind:
			if err := emit(comp{kind: InsertKind, s: cb.s}); err != nil {
				return Op{}, err
			}
			cb.n = 0
			cb.load()

		// a 删除的源字符在 a 的目标中已不存在，b 不会（也不能）经过它：只推进 a。
		case ca.kind == DeleteKind:
			if err := emit(comp{kind: DeleteKind, n: ca.n}); err != nil {
				return Op{}, err
			}
			ca.n = 0
			ca.load()

		default:
			// 其余情况两侧都落在目标字符上：(a Retain, b Retain/Delete) 配对。
			if ca.done() || cb.done() {
				return Op{}, fmt.Errorf("%w: compose streams desynchronized", ErrLengthMismatch)
			}
			m := ca.n
			if cb.n < m {
				m = cb.n
			}
			if cb.kind == DeleteKind {
				if err := emit(comp{kind: DeleteKind, n: m}); err != nil {
					return Op{}, err
				}
			} else {
				if err := emit(comp{kind: RetainKind, n: m}); err != nil {
					return Op{}, err
				}
			}
			ca.n -= m
			cb.n -= m
			if ca.n == 0 {
				ca.load()
			}
			if cb.n == 0 {
				cb.load()
			}
		}
	}

	res, err := normalize(out)
	if err != nil {
		return Op{}, err
	}
	// 防御性自检：组合操作源长度等于 a 的源长度，目标长度等于 b 的目标长度。
	if res.SourceLen() != a.SourceLen() || res.TargetLen() != b.TargetLen() {
		return Op{}, fmt.Errorf("%w: composed lengths %d->%d, want %d->%d",
			ErrLengthMismatch, res.SourceLen(), res.TargetLen(), a.SourceLen(), b.TargetLen())
	}
	return res, nil
}
