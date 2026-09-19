package ot

import (
	"unicode/utf8"
)

// runeLen 返回合法 UTF-8 字符串的 code point 数。
func runeLen(s string) int { return utf8.RuneCountInString(s) }

// runeSlice 将合法 UTF-8 字符串切为 []rune（按 code point）。
func runeSlice(s string) []rune { return []rune(s) }

// validRuneLen 校验 UTF-8 并返回 code point 数。
func validRuneLen(s string) (int, bool) {
	if !utf8.ValidString(s) {
		return 0, false
	}
	return utf8.RuneCountInString(s), true
}

// takePrefixRunes 返回 s 的前 m 个 rune；m >= runeLen(s) 时返回整个 s。
func takePrefixRunes(s string, m int) string {
	if m <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == m {
			return s[:i]
		}
		count++
	}
	return s
}

// dropRunes 返回 s 去掉前 m 个 rune 后的部分；m >= runeLen(s) 时返回空串。
func dropRunes(s string, m int) string {
	if m <= 0 {
		return s
	}
	count := 0
	for i := range s {
		if count == m {
			return s[i:]
		}
		count++
	}
	return ""
}
