package ot

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Apply 把 op 应用到 source，返回结果文本。
// source 必须是合法 UTF-8，且 op 必须恰好消耗 source（否则返回 ErrLengthMismatch）。
// 成功时结果长度等于 op.TargetLen()；source 与 op 均不会被修改。
func Apply(source string, op Op) (string, error) {
	srcLen, ok := validRuneLen(source)
	if !ok {
		return "", ErrInvalidUTF8
	}
	if srcLen > MaxCodePoints {
		return "", ErrTooLarge
	}
	if op.SourceLen() != srcLen {
		return "", fmt.Errorf("%w: source %d code points, op consumes %d",
			ErrLengthMismatch, srcLen, op.SourceLen())
	}

	runes := runeSlice(source)
	var sb strings.Builder
	sb.Grow(len(source))
	pos := 0
	for _, c := range op.c {
		switch c.kind {
		case RetainKind:
			if pos+c.n > srcLen {
				return "", ErrLengthMismatch
			}
			sb.WriteString(string(runes[pos : pos+c.n]))
			pos += c.n
		case DeleteKind:
			if pos+c.n > srcLen {
				return "", ErrLengthMismatch
			}
			pos += c.n
		case InsertKind:
			sb.WriteString(c.s)
		default:
			return "", fmt.Errorf("%w: %d", ErrUnknownKind, c.kind)
		}
	}
	if pos != srcLen {
		return "", fmt.Errorf("%w: consumed %d of %d", ErrLengthMismatch, pos, srcLen)
	}
	return sb.String(), nil
}

// Invert 返回 op 相对于 source 的逆操作：对 Apply(source, op) 的结果再应用逆操作
// 可还原 source。Insert 反转为 Delete，Delete 反转为插回被删原文，Retain 保持不变。
// source 必须合法且 op 恰好消耗它。
func Invert(source string, op Op) (Op, error) {
	srcLen, ok := validRuneLen(source)
	if !ok {
		return Op{}, ErrInvalidUTF8
	}
	if srcLen > MaxCodePoints {
		return Op{}, ErrTooLarge
	}
	if op.SourceLen() != srcLen {
		return Op{}, fmt.Errorf("%w: source %d code points, op consumes %d",
			ErrLengthMismatch, srcLen, op.SourceLen())
	}

	runes := runeSlice(source)
	inv := make([]comp, 0, len(op.c))
	pos := 0
	for _, c := range op.c {
		switch c.kind {
		case RetainKind:
			inv = append(inv, comp{kind: RetainKind, n: c.n})
			pos += c.n
		case DeleteKind:
			if pos+c.n > srcLen {
				return Op{}, ErrLengthMismatch
			}
			// 逆操作插回被删除的原始文本。
			inv = append(inv, comp{kind: InsertKind, s: string(runes[pos : pos+c.n])})
			pos += c.n
		case InsertKind:
			inv = append(inv, comp{kind: DeleteKind, n: utf8.RuneCountInString(c.s)})
		default:
			return Op{}, fmt.Errorf("%w: %d", ErrUnknownKind, c.kind)
		}
	}
	if pos != srcLen {
		return Op{}, fmt.Errorf("%w: consumed %d of %d", ErrLengthMismatch, pos, srcLen)
	}
	return normalize(inv)
}
