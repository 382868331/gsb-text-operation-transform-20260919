package ot

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// mustNew 构造 Op，失败即终止测试。
func mustNew(t *testing.T, cs ...Component) Op {
	t.Helper()
	op, err := New(cs...)
	if err != nil {
		t.Fatalf("New(%v) error: %v", cs, err)
	}
	return op
}

func TestNewRejectsNegativeLength(t *testing.T) {
	cases := [][]Component{
		{Retain(-1)},
		{Delete(-1)},
		{Retain(2), Delete(-3), Retain(1)},
		{Insert("x"), Delete(-1)},
	}
	for i, cs := range cases {
		if _, err := New(cs...); !errors.Is(err, ErrNegativeLength) {
			t.Errorf("case %d: got %v, want ErrNegativeLength", i, err)
		}
	}
}

func TestNewRejectsInvalidUTF8(t *testing.T) {
	bad := string([]byte{0x41, 0xff, 0xfe, 0x42})
	if _, err := New(Insert(bad)); !errors.Is(err, ErrInvalidUTF8) {
		t.Errorf("Insert invalid UTF-8: got %v, want ErrInvalidUTF8", err)
	}
	if _, err := Apply(bad, mustNew(t)); !errors.Is(err, ErrInvalidUTF8) {
		t.Errorf("Apply invalid UTF-8 source: got %v, want ErrInvalidUTF8", err)
	}
	if _, err := Invert(bad, mustNew(t)); !errors.Is(err, ErrInvalidUTF8) {
		t.Errorf("Invert invalid UTF-8 source: got %v, want ErrInvalidUTF8", err)
	}
}

func TestNewNormalizes(t *testing.T) {
	// 零长度段移除；相邻同类段合并（Insert 拼接）。
	op := mustNew(t,
		Retain(0), Retain(2), Retain(3),
		Insert(""), Insert("ab"), Insert("cd"),
		Delete(1), Delete(2),
		Retain(0),
	)
	got := op.Components()
	want := []Component{
		Retain(5),
		Insert("abcd"),
		Delete(3),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("component %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if op.SourceLen() != 8 || op.TargetLen() != 9 {
		t.Errorf("lengths: got src=%d tgt=%d, want 8/9", op.SourceLen(), op.TargetLen())
	}
}

func TestNewDoesNotAliasInput(t *testing.T) {
	src := []Component{Insert("abc"), Retain(1)}
	op, err := New(src...)
	if err != nil {
		t.Fatal(err)
	}
	src[0] = Delete(7)
	if op.Components()[0].Text != "abc" {
		t.Errorf("New aliased input slice: %v", op.Components())
	}
}

func TestApplyBasic(t *testing.T) {
	source := "Hello, World!" // 13 个 rune
	op := mustNew(t, Retain(7), Delete(5), Insert("OT"), Retain(1))
	// 删除 "World"，插入 "OT"，保留最后的 "!"。
	got, err := Apply(source, op)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello, OT!" {
		t.Errorf("got %q, want %q", got, "Hello, OT!")
	}
	if op.SourceLen() != len([]rune(source)) {
		t.Errorf("source len = %d", op.SourceLen())
	}
}

func TestApplyLengthMismatch(t *testing.T) {
	if _, err := Apply("abc", mustNew(t, Retain(2))); !errors.Is(err, ErrLengthMismatch) {
		t.Errorf("short op: got %v", err)
	}
	if _, err := Apply("abc", mustNew(t, Retain(4))); !errors.Is(err, ErrLengthMismatch) {
		t.Errorf("long op: got %v", err)
	}
	if _, err := Apply("abc", mustNew(t, Retain(2), Delete(2))); !errors.Is(err, ErrLengthMismatch) {
		t.Errorf("overrun: got %v", err)
	}
}

func TestApplyEmptyText(t *testing.T) {
	got, err := Apply("", mustNew(t))
	if err != nil || got != "" {
		t.Errorf("empty/empty: got %q, %v", got, err)
	}
	op := mustNew(t, Insert("start"))
	got, err = Apply("", op)
	if err != nil || got != "start" {
		t.Errorf("insert into empty: got %q, %v", got, err)
	}
	if op.SourceLen() != 0 || op.TargetLen() != 5 {
		t.Errorf("lengths: %d -> %d", op.SourceLen(), op.TargetLen())
	}
}

func TestApplyInputUnchanged(t *testing.T) {
	op := mustNew(t, Retain(1), Insert("XY"), Delete(1), Retain(1))
	before := op.String()
	if _, err := Apply("abc", op); err != nil {
		t.Fatal(err)
	}
	if op.String() != before {
		t.Errorf("op mutated by Apply: before=%s after=%s", before, op)
	}
}

func TestInvert(t *testing.T) {
	cases := []struct {
		source string
		op     Op
	}{
		{"abcdef", mustNew(t, Retain(1), Delete(2), Insert("XY"), Retain(2), Delete(1))},
		{"", mustNew(t, Insert("hello"))},
		{"xyz", mustNew(t, Delete(3), Insert("abc"))},
		{"世界😀a", mustNew(t, Retain(1), Delete(1), Insert("🎉"), Retain(2))},
		// 组合字符按 code point 计数：e + 组合锐音符 是 2 个 code point。
		{"ébc", mustNew(t, Retain(2), Delete(1), Insert("Z"), Delete(1))},
	}
	for i, tc := range cases {
		mid, err := Apply(tc.source, tc.op)
		if err != nil {
			t.Fatalf("case %d: apply: %v", i, err)
		}
		inv, err := Invert(tc.source, tc.op)
		if err != nil {
			t.Fatalf("case %d: invert: %v", i, err)
		}
		back, err := Apply(mid, inv)
		if err != nil {
			t.Fatalf("case %d: apply inverse: %v", i, err)
		}
		if back != tc.source {
			t.Errorf("case %d: got %q, want source %q (mid=%q inv=%s)",
				i, back, tc.source, mid, inv)
		}
	}
}

func TestInvertLengthMismatch(t *testing.T) {
	if _, err := Invert("ab", mustNew(t, Retain(1))); !errors.Is(err, ErrLengthMismatch) {
		t.Errorf("got %v", err)
	}
}

func TestTooLargeRejected(t *testing.T) {
	big := strings.Repeat("a", MaxCodePoints+1)
	if _, err := Apply(big, Op{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized source Apply: got %v", err)
	}
	if _, err := Invert(big, Op{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized source Invert: got %v", err)
	}
	// 目标长度超过上限：10000 个 Retain + 1 个 Insert。
	if _, err := New(Retain(MaxCodePoints), Insert("x")); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized target: got %v", err)
	}
	// 恰好上限应通过。
	op := mustNew(t, Retain(MaxCodePoints))
	if op.TargetLen() != MaxCodePoints {
		t.Errorf("boundary target = %d", op.TargetLen())
	}
}

func TestComposeEquivalence(t *testing.T) {
	cases := []struct {
		source string
		a, b   Op
	}{
		{
			"abcdef",
			mustNew(t, Retain(1), Insert("XY"), Retain(2), Delete(1), Retain(2)),
			mustNew(t, Retain(3), Delete(2), Insert("Z"), Retain(2)),
		},
		// b 删除 a 插入文本的一部分。
		{
			"ab",
			mustNew(t, Retain(1), Insert("XYZW"), Retain(1)),
			mustNew(t, Retain(2), Delete(2), Retain(2)),
		},
		// a 尾部删除，b 只在前面插入。
		{
			"abc",
			mustNew(t, Retain(1), Delete(2)),
			mustNew(t, Insert("Q"), Retain(1)),
		},
		// 空源。
		{
			"",
			mustNew(t, Insert("abc")),
			mustNew(t, Retain(2), Delete(1)),
		},
	}
	for i, tc := range cases {
		c, err := Compose(tc.a, tc.b)
		if err != nil {
			t.Fatalf("case %d: compose: %v", i, err)
		}
		mid, err := Apply(tc.source, tc.a)
		if err != nil {
			t.Fatalf("case %d: apply a: %v", i, err)
		}
		twoStep, err := Apply(mid, tc.b)
		if err != nil {
			t.Fatalf("case %d: apply b: %v", i, err)
		}
		oneStep, err := Apply(tc.source, c)
		if err != nil {
			t.Fatalf("case %d: apply c: %v", i, err)
		}
		if twoStep != oneStep {
			t.Errorf("case %d: two-step %q != composed %q (c=%s)", i, twoStep, oneStep, c)
		}
		if c.SourceLen() != tc.a.SourceLen() || c.TargetLen() != tc.b.TargetLen() {
			t.Errorf("case %d: composed lengths %d->%d, want %d->%d",
				i, c.SourceLen(), c.TargetLen(), tc.a.SourceLen(), tc.b.TargetLen())
		}
	}
}

func TestComposeLengthMismatch(t *testing.T) {
	a := mustNew(t, Retain(3), Insert("x")) // target 4
	b := mustNew(t, Retain(2))              // source 2
	if _, err := Compose(a, b); !errors.Is(err, ErrLengthMismatch) {
		t.Errorf("got %v, want ErrLengthMismatch", err)
	}
}

func TestComposeInputsUnchanged(t *testing.T) {
	a := mustNew(t, Retain(1), Insert("XY"), Retain(1))
	b := mustNew(t, Delete(1), Retain(2), Delete(1))
	sa, sb := a.String(), b.String()
	if _, err := Compose(a, b); err != nil {
		t.Fatal(err)
	}
	if a.String() != sa || b.String() != sb {
		t.Errorf("inputs mutated: a=%s b=%s", a, b)
	}
}

func TestTransformSamePositionInsert(t *testing.T) {
	source := "AB"
	a := mustNew(t, Retain(1), Insert("X"), Retain(1))
	b := mustNew(t, Retain(1), Insert("y"), Retain(1))

	aAfterB, bAfterA, err := Transform(a, b, "alpha", "beta")
	if err != nil {
		t.Fatal(err)
	}
	got, err := converge(source, a, b, aAfterB, bAfterA)
	if err != nil {
		t.Fatal(err)
	}
	if want := "AXyB"; got != want {
		t.Errorf("idA<idB: got %q, want %q", got, want)
	}

	aAfterB, bAfterA, err = Transform(a, b, "beta", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	got, err = converge(source, a, b, aAfterB, bAfterA)
	if err != nil {
		t.Fatal(err)
	}
	if want := "AyXB"; got != want {
		t.Errorf("idA>idB: got %q, want %q", got, want)
	}

	// 两侧在源开头/结尾同位置插入。
	a0 := mustNew(t, Insert("L"), Retain(2))
	b0 := mustNew(t, Insert("R"), Retain(2))
	aAfterB, bAfterA, err = Transform(a0, b0, "a", "b")
	if err != nil {
		t.Fatal(err)
	}
	got, err = converge(source, a0, b0, aAfterB, bAfterA)
	if err != nil {
		t.Fatal(err)
	}
	if want := "LRAB"; got != want {
		t.Errorf("head insert: got %q, want %q", got, want)
	}
}

func TestTransformInsertInsideDelete(t *testing.T) {
	// a 删除 "234"，b 在被删区域中间插入 "X"。删除不吞并发插入。
	source := "12345"
	a := mustNew(t, Retain(1), Delete(3), Retain(1))
	b := mustNew(t, Retain(3), Insert("X"), Retain(2))

	aAfterB, bAfterA, err := Transform(a, b, "site-A", "site-B")
	if err != nil {
		t.Fatal(err)
	}
	got, err := converge(source, a, b, aAfterB, bAfterA)
	if err != nil {
		t.Fatal(err)
	}
	// 结果保留 b 的插入："1X5"。
	if want := "1X5"; got != want {
		t.Errorf("got %q, want %q (aAfterB=%s bAfterA=%s)",
			got, want, aAfterB, bAfterA)
	}
}

func TestTransformOverlappingDeletes(t *testing.T) {
	source := "abcdef"

	// 完全重叠：双方删除同一段。
	a := mustNew(t, Delete(3), Retain(3))
	b := mustNew(t, Delete(3), Retain(3))
	aAfterB, bAfterA, err := Transform(a, b, "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	got, err := converge(source, a, b, aAfterB, bAfterA)
	if err != nil {
		t.Fatal(err)
	}
	if want := "def"; got != want {
		t.Errorf("full overlap: got %q, want %q", got, want)
	}

	// 部分重叠：a 删 "bcd"，b 删 "cde"，并集 "bcde" 只删一次。
	a = mustNew(t, Retain(1), Delete(3), Retain(2))
	b = mustNew(t, Retain(2), Delete(3), Retain(1))
	aAfterB, bAfterA, err = Transform(a, b, "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	got, err = converge(source, a, b, aAfterB, bAfterA)
	if err != nil {
		t.Fatal(err)
	}
	if want := "af"; got != want {
		t.Errorf("partial overlap: got %q, want %q (aAfterB=%s bAfterA=%s)",
			got, want, aAfterB, bAfterA)
	}
}

func TestTransformIDValidation(t *testing.T) {
	op := mustNew(t, Retain(1))
	if _, _, err := Transform(op, op, "same", "same"); !errors.Is(err, ErrSameID) {
		t.Errorf("same id: got %v, want ErrSameID", err)
	}
	if _, _, err := Transform(op, op, "", "b"); !errors.Is(err, ErrEmptyID) {
		t.Errorf("empty id: got %v, want ErrEmptyID", err)
	}
	if _, _, err := Transform(op, op, "a", ""); !errors.Is(err, ErrEmptyID) {
		t.Errorf("empty idB: got %v, want ErrEmptyID", err)
	}
	if _, _, err := Transform(op, op, "a", "bé"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("non-ascii id: got %v, want ErrInvalidID", err)
	}
	if _, _, err := Transform(mustNew(t, Retain(1)), mustNew(t, Retain(2)), "a", "b"); !errors.Is(err, ErrLengthMismatch) {
		t.Errorf("source mismatch: got %v, want ErrLengthMismatch", err)
	}
}

func TestTransformUnicode(t *testing.T) {
	// 😀 是 1 个 code point；e+◌́ 是 2 个 code point（不按字素簇合并）。
	source := "a😀éz" // 5 个 code point: a, 😀, e, ́, z
	a := mustNew(t, Retain(1), Delete(1), Retain(3))
	b := mustNew(t, Retain(2), Insert("🎉"), Retain(3))
	aAfterB, bAfterA, err := Transform(a, b, "u1", "u2")
	if err != nil {
		t.Fatal(err)
	}
	got, err := converge(source, a, b, aAfterB, bAfterA)
	if err != nil {
		t.Fatal(err)
	}
	// a 删掉 😀；b 在 e+◌́ 前插入 🎉。
	if want := "a🎉éz"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTransformEmptyText(t *testing.T) {
	a := mustNew(t, Insert("ping"))
	b := mustNew(t, Insert("pong"))
	aAfterB, bAfterA, err := Transform(a, b, "x", "y")
	if err != nil {
		t.Fatal(err)
	}
	got, err := converge("", a, b, aAfterB, bAfterA)
	if err != nil {
		t.Fatal(err)
	}
	if want := "pingpong"; got != want {
		t.Errorf("empty source concurrent insert: got %q, want %q", got, want)
	}
}

// converge 验证变换恒等式并返回收敛文本。
func converge(source string, a, b, aAfterB, bAfterA Op) (string, error) {
	left1, err := Apply(source, a)
	if err != nil {
		return "", fmt.Errorf("apply a: %w", err)
	}
	left, err := Apply(left1, bAfterA)
	if err != nil {
		return "", fmt.Errorf("apply bAfterA: %w", err)
	}
	right1, err := Apply(source, b)
	if err != nil {
		return "", fmt.Errorf("apply b: %w", err)
	}
	right, err := Apply(right1, aAfterB)
	if err != nil {
		return "", fmt.Errorf("apply aAfterB: %w", err)
	}
	if left != right {
		return "", fmt.Errorf("convergence mismatch: %q != %q", left, right)
	}
	return left, nil
}

// alphabet 用于随机生成：ASCII、CJK、emoji、组合字符基符与组合记号各自独立，
// 因此 code point 计数天然不同于字素簇计数。
var alphabet = []rune("abcXY世界😀🎉éZ9")

// genOp 用 rng 为源 src 生成一个合法操作；插入文本同样取自 alphabet。
func genOp(rng *rand.Rand, src []rune) Op {
	var cs []Component
	pos := 0
	n := len(src)
	for pos < n {
		if rng.Intn(2) == 0 {
			cs = append(cs, Insert(randomText(rng)))
		}
		remain := n - pos
		k := 1 + rng.Intn(min(3, remain))
		if rng.Intn(2) == 0 {
			cs = append(cs, Retain(k))
		} else {
			cs = append(cs, Delete(k))
		}
		pos += k
	}
	if rng.Intn(2) == 0 {
		cs = append(cs, Insert(randomText(rng)))
	}
	op, err := New(cs...)
	if err != nil {
		panic(fmt.Sprintf("genOp produced invalid op: %v (cs=%v)", err, cs))
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

// genSource 生成 0~11 个 code point 的随机源文本（含空文本）。
func genSource(rng *rand.Rand) []rune {
	n := rng.Intn(12)
	src := make([]rune, n)
	for i := range src {
		src[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return src
}

func TestRandomizedProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(20260919))
	const iterations = 400
	for it := 0; it < iterations; it++ {
		src := genSource(rng)
		source := string(src)

		a := genOp(rng, src)
		b := genOp(rng, src)
		idA := fmt.Sprintf("client-%d", rng.Intn(1000))
		idB := idA + "-x" // 保证不同且字典序确定

		// 1) Transform 收敛。
		aAfterB, bAfterA, err := Transform(a, b, idA, idB)
		if err != nil {
			t.Fatalf("iter %d: transform: %v\na=%s\nb=%s", it, err, a, b)
		}
		merged, err := converge(source, a, b, aAfterB, bAfterA)
		if err != nil {
			t.Fatalf("iter %d: %v\nsource=%q\na=%s\nb=%s", it, err, source, a, b)
		}

		// 2) 逆操作恢复源。
		for label, op := range map[string]Op{"a": a, "b": b} {
			mid, err := Apply(source, op)
			if err != nil {
				t.Fatalf("iter %d: apply %s: %v", it, label, err)
			}
			inv, err := Invert(source, op)
			if err != nil {
				t.Fatalf("iter %d: invert %s: %v", it, label, err)
			}
			back, err := Apply(mid, inv)
			if err != nil {
				t.Fatalf("iter %d: apply inverse %s: %v", it, label, err)
			}
			if back != source {
				t.Fatalf("iter %d: inverse %s gave %q, want %q", it, label, back, source)
			}
		}

		// 3) Compose 等价两次 Apply：在 a 的结果上再生成合法操作 c0。
		mid, err := Apply(source, a)
		if err != nil {
			t.Fatal(err)
		}
		c0 := genOp(rng, []rune(mid))
		composed, err := Compose(a, c0)
		if err != nil {
			t.Fatalf("iter %d: compose: %v\na=%s\nc0=%s", it, err, a, c0)
		}
		twoStep, err := Apply(mid, c0)
		if err != nil {
			t.Fatal(err)
		}
		oneStep, err := Apply(source, composed)
		if err != nil {
			t.Fatal(err)
		}
		if twoStep != oneStep {
			t.Fatalf("iter %d: compose mismatch %q != %q\nsource=%q\na=%s\nc0=%s\nc=%s",
				it, twoStep, oneStep, source, a, c0, composed)
		}

		// 4) 规范化幂等：Components 往返后语义不变。
		for label, op := range map[string]Op{"a": a, "b": b, "m": composed, "x": aAfterB} {
			re, err := New(op.Components()...)
			if err != nil {
				t.Fatalf("iter %d: renormalize %s: %v", it, label, err)
			}
			if re.String() != op.String() || re.SourceLen() != op.SourceLen() || re.TargetLen() != op.TargetLen() {
				t.Fatalf("iter %d: %s not normalization-stable: %s vs %s", it, label, op, re)
			}
		}

		// 5) 收敛文本长度等于两个变换结果的目标长度。
		if len([]rune(merged)) != aAfterB.TargetLen() {
			t.Fatalf("iter %d: merged length %d != aAfterB target %d",
				it, len([]rune(merged)), aAfterB.TargetLen())
		}
	}
}

func TestRandomizedTransformSelfConvergence(t *testing.T) {
	// 同一操作与自身变换（ID 不同）：相当于两个相同的并发编辑，删除重叠一次。
	rng := rand.New(rand.NewSource(4242))
	for it := 0; it < 100; it++ {
		src := genSource(rng)
		op := genOp(rng, src)
		aAfterB, bAfterA, err := Transform(op, op, "id-a", "id-b")
		if err != nil {
			t.Fatalf("iter %d: %v", it, err)
		}
		if _, err := converge(string(src), op, op, aAfterB, bAfterA); err != nil {
			t.Fatalf("iter %d: %v\nop=%s", it, err, op)
		}
	}
}
