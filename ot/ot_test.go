package ot

import (
	"errors"
	"math"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func mustApply(t *testing.T, source string, op Op) string {
	t.Helper()
	out, err := Apply(source, op)
	if err != nil {
		t.Fatalf("Apply(%q, %v) failed: %v", source, op, err)
	}
	return out
}

// converge 验证 Transform 的收敛保证，返回两端合并后的文本。
func converge(t *testing.T, s string, a, b Op, idA, idB string) string {
	t.Helper()
	aAfterB, bAfterA, err := Transform(a, b, idA, idB)
	if err != nil {
		t.Fatalf("Transform(%v, %v) failed: %v", a, b, err)
	}
	left := mustApply(t, mustApply(t, s, a), bAfterA)
	right := mustApply(t, mustApply(t, s, b), aAfterB)
	if left != right {
		t.Fatalf("divergence: s=%q a=%v b=%v left=%q right=%q (aAfterB=%v bAfterA=%v)",
			s, a, b, left, right, aAfterB, bAfterA)
	}
	return left
}

// ---------- 规范化与校验 ----------

func TestNormalizeMergesAndDropsZero(t *testing.T) {
	got, err := Normalize(Op{R(0), R(2), R(3), D(0), I(""), D(1), D(2), I("a"), I("b")})
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}
	want := Op{R(5), D(3), I("ab")}
	if got.String() != want.String() {
		t.Fatalf("Normalize = %v, want %v", got, want)
	}
	sl, _ := SourceLen(got)
	tl, _ := TargetLen(got)
	if sl != 8 || tl != 7 {
		t.Fatalf("SourceLen=%d TargetLen=%d, want 8/7", sl, tl)
	}
}

func TestNegativeLengthRejected(t *testing.T) {
	if _, err := Normalize(Op{R(-1)}); !errors.Is(err, ErrNegativeLength) {
		t.Fatalf("Normalize(R(-1)) err = %v, want ErrNegativeLength", err)
	}
	if _, err := Normalize(Op{D(-3)}); !errors.Is(err, ErrNegativeLength) {
		t.Fatalf("Normalize(D(-3)) err = %v, want ErrNegativeLength", err)
	}
	if _, err := Apply("abc", Op{R(1), D(-1), R(2)}); !errors.Is(err, ErrNegativeLength) {
		t.Fatalf("Apply err = %v, want ErrNegativeLength", err)
	}
	if _, err := Compose(Op{R(1)}, Op{R(-2), R(3)}); !errors.Is(err, ErrNegativeLength) {
		t.Fatalf("Compose err = %v, want ErrNegativeLength", err)
	}
}

func TestInvalidUTF8Rejected(t *testing.T) {
	bad := string([]byte{0xff, 0xfe})
	if _, err := Normalize(Op{I(bad)}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("Normalize err = %v, want ErrInvalidUTF8", err)
	}
	if _, err := Apply("abc", Op{R(3), I(bad)}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("Apply err = %v, want ErrInvalidUTF8", err)
	}
	if _, err := Apply(bad, Op{R(1)}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("Apply(bad source) err = %v, want ErrInvalidUTF8", err)
	}
}

func TestOverflowDetected(t *testing.T) {
	huge := Op{R(math.MaxInt), R(1)}
	if _, err := SourceLen(huge); !errors.Is(err, ErrOverflow) {
		t.Fatalf("SourceLen err = %v, want ErrOverflow", err)
	}
	if _, err := Apply("ab", huge); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Apply err = %v, want ErrOverflow", err)
	}
	if _, _, err := Transform(huge, Op{R(1)}, "a", "b"); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Transform err = %v, want ErrOverflow", err)
	}
}

func TestTooLongRejected(t *testing.T) {
	long := strings.Repeat("x", MaxLen+1)
	if _, err := Apply(long, Op{R(MaxLen + 1)}); !errors.Is(err, ErrTooLong) {
		t.Fatalf("Apply err = %v, want ErrTooLong", err)
	}
	big := Op{R(MaxLen), I("y"), I("z")} // 目标长度 MaxLen+2
	if _, err := Apply(strings.Repeat("x", MaxLen), big); !errors.Is(err, ErrTooLong) {
		t.Fatalf("Apply err = %v, want ErrTooLong", err)
	}
}

func TestApplyLengthMismatch(t *testing.T) {
	if _, err := Apply("abc", Op{R(2)}); !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("Apply err = %v, want ErrLengthMismatch", err)
	}
	if _, err := Apply("abc", Op{R(4)}); !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("Apply err = %v, want ErrLengthMismatch", err)
	}
}

// ---------- Apply / Invert / Compose ----------

func TestApplyBasic(t *testing.T) {
	got := mustApply(t, "hello world", Op{R(6), D(5), I("gopher")})
	if got != "hello gopher" {
		t.Fatalf("got %q", got)
	}
}

func TestInvertRestoresSource(t *testing.T) {
	s := "hello"
	op := Op{R(1), D(2), I("XY"), R(2)}
	mid := mustApply(t, s, op)
	if mid != "hXYlo" {
		t.Fatalf("mid = %q", mid)
	}
	inv, err := Invert(s, op)
	if err != nil {
		t.Fatalf("Invert failed: %v", err)
	}
	wantInv := Op{R(1), I("el"), D(2), R(2)}
	if inv.String() != wantInv.String() {
		t.Fatalf("Invert = %v, want %v", inv, wantInv)
	}
	if back := mustApply(t, mid, inv); back != s {
		t.Fatalf("round trip = %q, want %q", back, s)
	}
}

func TestComposeEquivalentToTwoApplies(t *testing.T) {
	s := "abcdef"
	a := Op{R(1), D(2), I("X"), R(3)} // "aXdef"
	b := Op{R(2), I("Y"), D(2), R(1)} // "aXYf"
	mid := mustApply(t, s, a)
	want := mustApply(t, mid, b)
	comp, err := Compose(a, b)
	if err != nil {
		t.Fatalf("Compose failed: %v", err)
	}
	if got := mustApply(t, s, comp); got != want {
		t.Fatalf("Compose apply = %q, want %q (comp=%v)", got, want, comp)
	}
}

func TestComposeInsertThenDelete(t *testing.T) {
	// a 插入的文本随即被 b 删除，组合后应为空操作。
	comp, err := Compose(Op{I("tmp")}, Op{D(3)})
	if err != nil {
		t.Fatalf("Compose failed: %v", err)
	}
	if len(comp) != 0 {
		t.Fatalf("Compose = %v, want empty op", comp)
	}
}

func TestComposeLengthMismatchRejected(t *testing.T) {
	if _, err := Compose(Op{R(1)}, Op{R(2)}); !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("Compose err = %v, want ErrLengthMismatch", err)
	}
	if _, err := Compose(Op{I("ab")}, Op{R(1)}); !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("Compose err = %v, want ErrLengthMismatch", err)
	}
}

// ---------- Transform ----------

func TestTransformSameIDRejected(t *testing.T) {
	op := Op{R(1)}
	if _, _, err := Transform(op, op, "x", "x"); !errors.Is(err, ErrBadID) {
		t.Fatalf("same ID err = %v, want ErrBadID", err)
	}
	if _, _, err := Transform(op, op, "", "x"); !errors.Is(err, ErrBadID) {
		t.Fatalf("empty ID err = %v, want ErrBadID", err)
	}
	if _, _, err := Transform(op, op, "é", "x"); !errors.Is(err, ErrBadID) {
		t.Fatalf("non-ASCII ID err = %v, want ErrBadID", err)
	}
}

func TestTransformSamePositionInsert(t *testing.T) {
	s := "ab"
	a := Op{R(1), I("X"), R(1)}
	b := Op{R(1), I("Y"), R(1)}
	// idA < idB：a 的插入在前。
	if got := converge(t, s, a, b, "aaa", "bbb"); got != "aXYb" {
		t.Fatalf("merged = %q, want %q", got, "aXYb")
	}
	// 交换 ID 顺序后，b 的插入在前。
	if got := converge(t, s, a, b, "bbb", "aaa"); got != "aYXb" {
		t.Fatalf("merged = %q, want %q", got, "aYXb")
	}
	// 变换后的具体操作内容。
	aAfterB, bAfterA, err := Transform(a, b, "aaa", "bbb")
	if err != nil {
		t.Fatal(err)
	}
	// a 的插入先于 b，但 aAfterB 需跳过 b 已插入的 "Y"。
	if aAfterB.String() != (Op{R(1), I("X"), R(2)}).String() {
		t.Fatalf("aAfterB = %v", aAfterB)
	}
	if bAfterA.String() != (Op{R(2), I("Y"), R(1)}).String() {
		t.Fatalf("bAfterA = %v", bAfterA)
	}
}

func TestTransformInsertInsideDelete(t *testing.T) {
	s := "hello"
	a := Op{D(5)}               // 删除整段
	b := Op{R(2), I("X"), R(3)} // 在被删区域内部插入
	if got := converge(t, s, a, b, "a", "b"); got != "X" {
		t.Fatalf("merged = %q, want %q (delete must not swallow concurrent insert)", got, "X")
	}
	aAfterB, _, err := Transform(a, b, "a", "b")
	if err != nil {
		t.Fatal(err)
	}
	// a 的删除为并发插入让位：Delete(2) Retain(1) Delete(3)。
	if aAfterB.String() != (Op{D(2), R(1), D(3)}).String() {
		t.Fatalf("aAfterB = %v", aAfterB)
	}
}

func TestTransformFullOverlapDelete(t *testing.T) {
	s := "abc"
	a := Op{D(3)}
	b := Op{D(3)}
	if got := converge(t, s, a, b, "a", "b"); got != "" {
		t.Fatalf("merged = %q, want empty (overlapping delete applied once)", got)
	}
}

func TestTransformPartialOverlapDelete(t *testing.T) {
	s := "abcd"
	a := Op{D(3), R(1)} // 删 abc
	b := Op{R(1), D(3)} // 删 bcd
	if got := converge(t, s, a, b, "a", "b"); got != "" {
		t.Fatalf("merged = %q, want empty (union of deletes)", got)
	}
	// 部分重叠、两端各留字符。
	s2 := "abcde"
	a2 := Op{D(3), R(2)} // 留 de
	b2 := Op{R(2), D(3)} // 留 ab
	if got := converge(t, s2, a2, b2, "a", "b"); got != "" {
		t.Fatalf("merged = %q, want empty", got)
	}
	s3 := "abcdef"
	a3 := Op{D(2), R(4)}       // 删 ab -> cdef
	b3 := Op{R(1), D(2), R(3)} // 删 bc -> adef
	if got := converge(t, s3, a3, b3, "a", "b"); got != "def" {
		t.Fatalf("merged = %q, want %q", got, "def")
	}
}

func TestEmojiAndCombiningChars(t *testing.T) {
	// 4 个 code point：emoji、e、组合重音符、x。组合字符不按字素簇处理。
	s := "🙂éx"
	if runeLen(s) != 4 {
		t.Fatalf("test source should be 4 code points, got %d", runeLen(s))
	}
	a := Op{D(1), R(3)}       // 删除 emoji -> "éx"
	b := Op{R(1), D(2), R(1)} // 删除 e 和组合符 -> "🙂x"
	if got := mustApply(t, s, a); got != "éx" {
		t.Fatalf("Apply a = %q", got)
	}
	if got := mustApply(t, s, b); got != "🙂x" {
		t.Fatalf("Apply b = %q", got)
	}
	if got := converge(t, s, a, b, "a", "b"); got != "x" {
		t.Fatalf("merged = %q, want %q", got, "x")
	}
	// 在 emoji 中间位置按 code point 插入。
	got := mustApply(t, "🙂🙂", Op{R(1), I("!"), R(1)})
	if got != "🙂!🙂" {
		t.Fatalf("got %q", got)
	}
}

func TestEmptyText(t *testing.T) {
	if got := mustApply(t, "", Op{}); got != "" {
		t.Fatalf("Apply empty = %q", got)
	}
	// 空源上的并发插入。
	a := Op{I("x")}
	b := Op{I("y")}
	if got := converge(t, "", a, b, "a", "b"); got != "xy" {
		t.Fatalf("merged = %q, want %q", got, "xy")
	}
	inv, err := Invert("", Op{})
	if err != nil || len(inv) != 0 {
		t.Fatalf("Invert empty = %v, %v", inv, err)
	}
	comp, err := Compose(Op{}, Op{})
	if err != nil || len(comp) != 0 {
		t.Fatalf("Compose empty = %v, %v", comp, err)
	}
	// 空操作是恒等。
	if got := mustApply(t, "abc", Op{R(3)}); got != "abc" {
		t.Fatalf("identity = %q", got)
	}
}

func TestInputsNotMutated(t *testing.T) {
	a := Op{R(1), D(1), I("z")}
	b := Op{R(1), I("q"), D(1)}
	aCopy := append(Op(nil), a...)
	bCopy := append(Op(nil), b...)
	if _, _, err := Transform(a, b, "a", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := Compose(a, b); err != nil {
		t.Fatal(err)
	}
	if _, err := Normalize(a); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply("xy", a); err != nil {
		t.Fatal(err)
	}
	if a.String() != aCopy.String() || b.String() != bCopy.String() {
		t.Fatalf("inputs mutated: a=%v (was %v) b=%v (was %v)", a, aCopy, b, bCopy)
	}
}

// ---------- 固定种子随机验证 ----------

var alphabet = []string{"a", "b", "c", "é", "🙂", "́", "好"}

func randText(r *rand.Rand, maxLen int) string {
	n := 1 + r.Intn(maxLen)
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(alphabet[r.Intn(len(alphabet))])
	}
	return sb.String()
}

func randSource(r *rand.Rand) string {
	n := r.Intn(13)
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(alphabet[r.Intn(len(alphabet))])
	}
	return sb.String()
}

// randOp 生成恰好消耗 srcLen 个 code point 的合法操作，插入、删除交错。
func randOp(r *rand.Rand, srcLen int) Op {
	var op Op
	pos := 0
	for pos < srcLen {
		switch r.Intn(3) {
		case 0:
			n := 1 + r.Intn(srcLen-pos)
			op = append(op, R(n))
			pos += n
		case 1:
			n := 1 + r.Intn(srcLen-pos)
			op = append(op, D(n))
			pos += n
		default:
			op = append(op, I(randText(r, 4)))
		}
	}
	if r.Intn(2) == 0 {
		op = append(op, I(randText(r, 3)))
	}
	return op
}

func TestRandomFixedSeedProperties(t *testing.T) {
	r := rand.New(rand.NewSource(20260919))
	const iters = 300
	for iter := 0; iter < iters; iter++ {
		s := randSource(r)
		a := randOp(r, runeLen(s))
		b := randOp(r, runeLen(s))

		// 收敛性：Apply(Apply(s,a),bAfterA) == Apply(Apply(s,b),aAfterB)
		converge(t, s, a, b, "alice", "bob")

		// 逆操作恢复源。
		invA, err := Invert(s, a)
		if err != nil {
			t.Fatalf("iter %d: Invert failed: %v", iter, err)
		}
		if back := mustApply(t, mustApply(t, s, a), invA); back != s {
			t.Fatalf("iter %d: invert round trip = %q, want %q (a=%v)", iter, back, s, a)
		}

		// 组合与两次 Apply 等价。
		mid := mustApply(t, s, a)
		c := randOp(r, runeLen(mid))
		comp, err := Compose(a, c)
		if err != nil {
			t.Fatalf("iter %d: Compose failed: %v", iter, err)
		}
		if got, want := mustApply(t, s, comp), mustApply(t, mid, c); got != want {
			t.Fatalf("iter %d: compose = %q, want %q (s=%q a=%v c=%v comp=%v)",
				iter, got, want, s, a, c, comp)
		}

		// 规范化结果长度与 Apply 结果一致。
		na, err := Normalize(a)
		if err != nil {
			t.Fatalf("iter %d: Normalize failed: %v", iter, err)
		}
		tl, err := TargetLen(na)
		if err != nil {
			t.Fatalf("iter %d: TargetLen failed: %v", iter, err)
		}
		if tl != runeLen(mid) {
			t.Fatalf("iter %d: TargetLen=%d, applied len=%d", iter, tl, runeLen(mid))
		}
	}
}
