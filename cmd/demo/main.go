// demo 演示 ot 库：同位置插入、重叠删除、组合与逆操作，
// 以及一个实际触发的失败（Compose 长度不匹配）。
package main

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/382868331/gsb-text-operation-transform-20260919/ot"
)

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func showOp(name string, op ot.Op) {
	norm, err := ot.Normalize(op)
	if err != nil {
		fmt.Printf("  %s: 非法操作: %v\n", name, err)
		return
	}
	sl, _ := ot.SourceLen(norm)
	tl, _ := ot.TargetLen(norm)
	fmt.Printf("  %s = %v  (源长度 %d, 目标长度 %d)\n", name, norm, sl, tl)
}

func mustApply(source string, op ot.Op) string {
	out, err := ot.Apply(source, op)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Apply(%q, %v) 失败: %v\n", source, op, err)
		os.Exit(1)
	}
	return out
}

func main() {
	failed := false

	fmt.Println("== 1. 同位置并发插入（ID 字典序较小者先插入） ==")
	s1 := "ab"
	a1 := ot.Op{ot.R(1), ot.I("X"), ot.R(1)}
	b1 := ot.Op{ot.R(1), ot.I("Y"), ot.R(1)}
	fmt.Printf("  源文本: %q\n", s1)
	showOp("a (alice)", a1)
	showOp("b (bob)  ", b1)
	aAfterB, bAfterA, err := ot.Transform(a1, b1, "alice", "bob")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Transform 失败: %v\n", err)
		os.Exit(1)
	}
	showOp("aAfterB", aAfterB)
	showOp("bAfterA", bAfterA)
	left := mustApply(mustApply(s1, a1), bAfterA)
	right := mustApply(mustApply(s1, b1), aAfterB)
	fmt.Printf("  两端收敛: %q == %q -> %v\n", left, right, left == right)

	fmt.Println()
	fmt.Println("== 2. 重叠删除（只生效一次）与删除内部的并发插入 ==")
	s2 := "abcdef"
	a2 := ot.Op{ot.D(2), ot.R(4)}          // 删 ab
	b2 := ot.Op{ot.R(1), ot.D(2), ot.R(3)} // 删 bc
	fmt.Printf("  源文本: %q\n", s2)
	showOp("a", a2)
	showOp("b", b2)
	aAfterB2, bAfterA2, err := ot.Transform(a2, b2, "alice", "bob")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Transform 失败: %v\n", err)
		os.Exit(1)
	}
	showOp("aAfterB", aAfterB2)
	showOp("bAfterA", bAfterA2)
	fmt.Printf("  合并结果: %q\n", mustApply(mustApply(s2, a2), bAfterA2))

	s2b := "hello"
	a2b := ot.Op{ot.D(5)}
	b2b := ot.Op{ot.R(2), ot.I("X"), ot.R(3)}
	fmt.Printf("  删除内部的并发插入: 源 %q, a 删整段, b 在中间插入 \"X\"\n", s2b)
	_, bAfterA2b, err := ot.Transform(a2b, b2b, "alice", "bob")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Transform 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  合并结果: %q（删除不吞并发插入）\n", mustApply(mustApply(s2b, a2b), bAfterA2b))

	fmt.Println()
	fmt.Println("== 3. Compose 与 Invert ==")
	s3 := "hello world"
	a3 := ot.Op{ot.R(6), ot.D(5), ot.I("gopher")}
	b3 := ot.Op{ot.R(6), ot.I("!"), ot.D(1), ot.R(5)}
	fmt.Printf("  源文本: %q\n", s3)
	showOp("a", a3)
	showOp("b", b3)
	comp, err := ot.Compose(a3, b3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Compose 失败: %v\n", err)
		os.Exit(1)
	}
	showOp("Compose(a,b)", comp)
	twoStep := mustApply(mustApply(s3, a3), b3)
	oneStep := mustApply(s3, comp)
	fmt.Printf("  组合与两次 Apply 等价: %q == %q -> %v\n", oneStep, twoStep, oneStep == twoStep)
	inv, err := ot.Invert(s3, a3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invert 失败: %v\n", err)
		os.Exit(1)
	}
	showOp("Invert(a)", inv)
	fmt.Printf("  逆操作恢复源: %q\n", mustApply(mustApply(s3, a3), inv))

	fmt.Println()
	fmt.Println("== 4. 固定种子随机一致性检查（200 组小样本） ==")
	r := rand.New(rand.NewSource(20260919))
	alphabet := []string{"a", "b", "c", "é", "🙂", "́", "好"}
	randSrc := func() string {
		var sb strings.Builder
		for i, n := 0, r.Intn(13); i < n; i++ {
			sb.WriteString(alphabet[r.Intn(len(alphabet))])
		}
		return sb.String()
	}
	randOp := func(srcLen int) ot.Op {
		var op ot.Op
		for pos := 0; pos < srcLen; {
			switch r.Intn(3) {
			case 0:
				n := 1 + r.Intn(srcLen-pos)
				op = append(op, ot.R(n))
				pos += n
			case 1:
				n := 1 + r.Intn(srcLen-pos)
				op = append(op, ot.D(n))
				pos += n
			default:
				n := 1 + r.Intn(4)
				var sb strings.Builder
				for i := 0; i < n; i++ {
					sb.WriteString(alphabet[r.Intn(len(alphabet))])
				}
				op = append(op, ot.I(sb.String()))
			}
		}
		return op
	}
	const iters = 200
	for i := 0; i < iters; i++ {
		s := randSrc()
		a := randOp(runeLen(s))
		b := randOp(runeLen(s))
		aB, bA, err := ot.Transform(a, b, "alice", "bob")
		if err != nil {
			fmt.Fprintf(os.Stderr, "第 %d 组 Transform 失败: %v\n", i, err)
			os.Exit(1)
		}
		l := mustApply(mustApply(s, a), bA)
		rr := mustApply(mustApply(s, b), aB)
		if l != rr {
			fmt.Fprintf(os.Stderr, "第 %d 组不收敛: %q vs %q\n", i, l, rr)
			os.Exit(1)
		}
	}
	fmt.Printf("  %d/%d 组并发操作全部收敛\n", iters, iters)

	fmt.Println()
	fmt.Println("== 5. 实际触发的失败：Compose 长度不匹配 ==")
	bad1 := ot.Op{ot.R(2), ot.I("xy")} // 目标长度 4
	bad2 := ot.Op{ot.R(3)}             // 源长度 3
	showOp("a", bad1)
	showOp("b", bad2)
	if _, err := ot.Compose(bad1, bad2); err != nil {
		fmt.Printf("  Compose 按预期拒绝: %v\n", err)
		failed = true
	}
	if _, _, err := ot.Transform(ot.Op{ot.R(1)}, ot.Op{ot.R(1)}, "same", "same"); err != nil {
		fmt.Printf("  Transform 相同 ID 按预期拒绝: %v\n", err)
	}
	if !failed {
		fmt.Fprintln(os.Stderr, "演示异常：预期的失败没有发生")
		os.Exit(1)
	}
	fmt.Println()
	fmt.Println("演示完成：正常结果全部计算通过，失败用例按预期触发。")
}
