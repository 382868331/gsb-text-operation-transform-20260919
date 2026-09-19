// Command demo 展示文本操作变换内核：规范化、Apply/Invert/Compose、
// 同位置并发插入与重叠删除的 Transform，并用固定种子做少量随机代数验证；
// 最后展示一个由代码实际触发的失败（相同客户端 ID 被拒绝）。
package main

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	ot "github.com/382868331/gsb-text-operation-transform-20260919"
)

func main() {
	start := time.Now()
	fmt.Println("=== 文本操作变换（OT）内核演示 ===")
	fmt.Println()

	demoNormalize()
	demoApplyInvert()
	demoCompose()
	demoSamePositionInsert()
	demoOverlappingDelete()
	demoRandomized()
	demoFailure()

	fmt.Printf("\n演示完成，耗时 %s（远小于 8 秒预算）。\n", time.Since(start).Round(time.Millisecond))
}

func demoNormalize() {
	fmt.Println("--- 1. 规范化：零长度段移除、相邻同类段合并 ---")
	op, err := ot.New(
		ot.Retain(0), ot.Retain(2), ot.Retain(1),
		ot.Insert("He"), ot.Insert("llo"),
		ot.Delete(0), ot.Delete(2),
		ot.Retain(2),
	)
	must(err)
	fmt.Printf("规范化操作: %s\n", op)
	fmt.Printf("源长度(Retain+Delete) = %d code points\n", op.SourceLen())
	fmt.Printf("目标长度(Retain+Insert) = %d code points\n", op.TargetLen())
	fmt.Println()
}

func demoApplyInvert() {
	fmt.Println("--- 2. Apply 与 Invert ---")
	source := "Hello, OT!"
	op, err := ot.New(ot.Retain(7), ot.Delete(2), ot.Insert("操作变换"), ot.Retain(1))
	must(err)
	mid, err := ot.Apply(source, op)
	must(err)
	fmt.Printf("源文本:   %q（%d code points）\n", source, len([]rune(source)))
	fmt.Printf("操作:     %s\n", op)
	fmt.Printf("Apply:    %q（%d code points）\n", mid, len([]rune(mid)))

	inv, err := ot.Invert(source, op)
	must(err)
	back, err := ot.Apply(mid, inv)
	must(err)
	fmt.Printf("逆操作:   %s\n", inv)
	fmt.Printf("再 Apply: %q（应等于源文本：%v）\n", back, back == source)
	fmt.Println()
}

func demoCompose() {
	fmt.Println("--- 3. Compose：等价“先 a 后 b”，且不需要源文本 ---")
	source := "abcdef"
	a, err := ot.New(ot.Retain(1), ot.Insert("XY"), ot.Retain(2), ot.Delete(1), ot.Retain(2))
	must(err)
	b, err := ot.New(ot.Retain(3), ot.Delete(2), ot.Insert("Z"), ot.Retain(2))
	must(err)
	c, err := ot.Compose(a, b)
	must(err)

	twoStep1, err := ot.Apply(source, a)
	must(err)
	twoStep, err := ot.Apply(twoStep1, b)
	must(err)
	oneStep, err := ot.Apply(source, c)
	must(err)
	fmt.Printf("源文本: %q\n", source)
	fmt.Printf("a:      %s\n", a)
	fmt.Printf("b:      %s\n", b)
	fmt.Printf("a∘b:    %s\n", c)
	fmt.Printf("Apply(Apply(s,a),b) = %q\n", twoStep)
	fmt.Printf("Apply(s, a∘b)       = %q（相等：%v）\n", oneStep, twoStep == oneStep)
	fmt.Println()
}

func demoSamePositionInsert() {
	fmt.Println("--- 4. Transform：同一源位置并发插入，ID 字典序小者在前 ---")
	source := "AB"
	a, err := ot.New(ot.Retain(1), ot.Insert("X"), ot.Retain(1))
	must(err)
	b, err := ot.New(ot.Retain(1), ot.Insert("y"), ot.Retain(1))
	must(err)

	for _, pair := range [][2]string{{"alpha", "beta"}, {"beta", "alpha"}} {
		aAfterB, bAfterA, err := ot.Transform(a, b, pair[0], pair[1])
		must(err)
		merged := mustConverge(source, a, b, aAfterB, bAfterA)
		fmt.Printf("idA=%-5q idB=%-5q -> 收敛文本 %q（aAfterB: %s）\n",
			pair[0], pair[1], merged, aAfterB)
	}
	fmt.Println()
}

func demoOverlappingDelete() {
	fmt.Println("--- 5. Transform：删除只作用于原字符，重叠删除只生效一次 ---")
	source := "abcdef"

	// 完全重叠：双方都删 "abc"。
	fullA, err := ot.New(ot.Delete(3), ot.Retain(3))
	must(err)
	fullB, err := ot.New(ot.Delete(3), ot.Retain(3))
	must(err)
	aAfterB, bAfterA, err := ot.Transform(fullA, fullB, "A", "B")
	must(err)
	fmt.Printf("完全重叠删除: 收敛文本 %q（期望 %q）\n",
		mustConverge(source, fullA, fullB, aAfterB, bAfterA), "def")

	// 部分重叠：a 删 "bcd"，b 删 "cde"，并集 "bcde" 只删一次。
	a, err := ot.New(ot.Retain(1), ot.Delete(3), ot.Retain(2))
	must(err)
	b, err := ot.New(ot.Retain(2), ot.Delete(3), ot.Retain(1))
	must(err)
	aAfterB, bAfterA, err = ot.Transform(a, b, "A", "B")
	must(err)
	fmt.Printf("部分重叠删除: 收敛文本 %q（期望 %q），aAfterB=%s, bAfterA=%s\n",
		mustConverge(source, a, b, aAfterB, bAfterA), "af", aAfterB, bAfterA)

	// 删除区域内部的并发插入不会被吞掉。
	src2 := "12345"
	del, err := ot.New(ot.Retain(1), ot.Delete(3), ot.Retain(1))
	must(err)
	ins, err := ot.New(ot.Retain(3), ot.Insert("X"), ot.Retain(2))
	must(err)
	dAfterI, iAfterD, err := ot.Transform(del, ins, "deleter", "inserter")
	must(err)
	fmt.Printf("删除内部插入: 收敛文本 %q（期望 %q，插入 X 被保留）\n",
		mustConverge(src2, del, ins, dAfterI, iAfterD), "1X5")
	fmt.Println()
}

func demoRandomized() {
	fmt.Println("--- 6. 固定种子随机验证（150 个小样本）---")
	rng := rand.New(rand.NewSource(20260919))
	for it := 0; it < 150; it++ {
		src := genSource(rng)
		source := string(src)
		a := genOp(rng, src)
		b := genOp(rng, src)

		aAfterB, bAfterA, err := ot.Transform(a, b, "user-a", "user-b")
		must(err)
		merged := mustConverge(source, a, b, aAfterB, bAfterA)

		mid, err := ot.Apply(source, a)
		must(err)
		inv, err := ot.Invert(source, a)
		must(err)
		back, err := ot.Apply(mid, inv)
		must(err)
		if back != source {
			panic("inverse did not restore source")
		}

		c0 := genOp(rng, []rune(mid))
		composed, err := ot.Compose(a, c0)
		must(err)
		twoStep, err := ot.Apply(mid, c0)
		must(err)
		oneStep, err := ot.Apply(source, composed)
		must(err)
		if twoStep != oneStep {
			panic(fmt.Sprintf("compose mismatch: %q != %q", twoStep, oneStep))
		}
		if len([]rune(merged)) != aAfterB.TargetLen() {
			panic("merged length mismatch")
		}
	}
	fmt.Println("全部样本满足：Transform 收敛、Invert 还原、Compose 与两次 Apply 等价。")
	fmt.Println()
}

func demoFailure() {
	fmt.Println("--- 7. 实际触发的失败：相同客户端 ID 被拒绝 ---")
	a, err := ot.New(ot.Retain(1), ot.Insert("X"), ot.Retain(1))
	must(err)
	b, err := ot.New(ot.Retain(1), ot.Insert("Y"), ot.Retain(1))
	must(err)

	_, _, err = ot.Transform(a, b, "same-id", "same-id")
	fmt.Printf("Transform(a, b, %q, %q) 返回错误:\n  %v\n", "same-id", "same-id", err)
	if errors.Is(err, ot.ErrSameID) {
		fmt.Println("确认是 ErrSameID —— 这是代码真实计算触发并被正确拒绝的失败用例。")
	} else {
		panic(fmt.Sprintf("期望 ErrSameID，实际 %v", err))
	}
}

// mustConverge 计算并校验变换恒等式，返回收敛文本。
func mustConverge(source string, a, b, aAfterB, bAfterA ot.Op) string {
	left1, err := ot.Apply(source, a)
	must(err)
	left, err := ot.Apply(left1, bAfterA)
	must(err)
	right1, err := ot.Apply(source, b)
	must(err)
	right, err := ot.Apply(right1, aAfterB)
	must(err)
	if left != right {
		panic(fmt.Sprintf("convergence mismatch: %q != %q", left, right))
	}
	return left
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

var alphabet = []rune("abcXY世界😀🎉éZ9")

func genSource(rng *rand.Rand) []rune {
	n := rng.Intn(10)
	src := make([]rune, n)
	for i := range src {
		src[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return src
}

func genOp(rng *rand.Rand, src []rune) ot.Op {
	var cs []ot.Component
	pos, n := 0, len(src)
	for pos < n {
		if rng.Intn(2) == 0 {
			cs = append(cs, ot.Insert(randomText(rng)))
		}
		remain := n - pos
		k := 1 + rng.Intn(min(3, remain))
		if rng.Intn(2) == 0 {
			cs = append(cs, ot.Retain(k))
		} else {
			cs = append(cs, ot.Delete(k))
		}
		pos += k
	}
	if rng.Intn(2) == 0 {
		cs = append(cs, ot.Insert(randomText(rng)))
	}
	op, err := ot.New(cs...)
	if err != nil {
		panic(err)
	}
	return op
}

func randomText(rng *rand.Rand) string {
	k := 1 + rng.Intn(3)
	var sb strings.Builder
	for i := 0; i < k; i++ {
		sb.WriteRune(alphabet[rng.Intn(len(alphabet))])
	}
	return sb.String()
}
