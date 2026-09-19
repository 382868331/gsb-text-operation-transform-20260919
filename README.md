# 文本操作变换与组合内核

纯 Go（标准库）实现的两个并发文本编辑合并内核：操作变换（Transform）、操作组合（Compose），以及应用与逆操作。仅支持两个并发操作，不包含三操作 TP2、网络会话、撤销栈或富文本。

模块路径：`github.com/382868331/gsb-text-operation-transform-20260919`，包名 `ot`（仓库根目录）。

## 接口

```go
import ot "github.com/382868331/gsb-text-operation-transform-20260919"

// 构造段
ot.Retain(n int) ot.Component          // 保留 n 个 Unicode code point
ot.Delete(n int) ot.Component          // 删除 n 个 code point
ot.Insert(text string) ot.Component    // 插入 text

// 校验并规范化：拒绝负长度/非法 UTF-8；零长度段移除；相邻同类段合并
op, err := ot.New(ot.Retain(2), ot.Insert("x"), ot.Delete(1))

op.SourceLen() int   // Retain+Delete：消耗的源长度
op.TargetLen() int   // Retain+Insert：生成的目标长度
op.Components() []ot.Component
op.String() string   // 规范化结果，如 Retain(2), Insert("x"), Delete(1)

// 核心 API
out, err := ot.Apply(source string, op ot.Op) (string, error)
inv, err := ot.Invert(source string, op ot.Op) (ot.Op, error)
c,   err := ot.Compose(a, b ot.Op) (ot.Op, error)
aAfterB, bAfterA, err := ot.Transform(a, b ot.Op, idA, idB string) (ot.Op, ot.Op, error)
```

### 语义约定

- 所有长度按 **Unicode code point（rune）** 计数；组合字符不按字素簇合并（`é` = e + U+0301 算 2 个 code point）。
- Retain 与 Delete 长度之和必须**恰好**等于源长度，否则返回 `ErrLengthMismatch`。
- 输入文本与单个操作的目标长度上限均为 `ot.MaxCodePoints`（10000）个 code point。
- 负长度返回 `ErrNegativeLength`，非法 UTF-8 返回 `ErrInvalidUTF8`，超长返回 `ErrTooLarge`，长度算术溢出返回 `ErrOverflow`。
- `Op` 构造后不可变；Apply/Invert/Compose/Transform 均不修改输入；Compose 不需要源文本。
- `Compose(a,b)` 等价“先 a 后 b”，要求 `a.TargetLen() == b.SourceLen()`。
- `Transform(a,b,idA,idB)` 要求两者源长度相同；`idA/idB` 必须是**不同的非空 ASCII** 字符串（否则 `ErrEmptyID`/`ErrInvalidID`/`ErrSameID`）。同一源位置双方都插入时，ID 字典序较小者的插入排在前面。删除只作用于原始字符，不吞并对方的并发插入；双方重叠删除的字符只删除一次。

### 保证的代数关系

```text
Apply(Apply(s, a), bAfterA) == Apply(Apply(s, b), aAfterB)   // Transform 收敛
Apply(Apply(s, op), Invert(s, op)) == s                      // 逆操作恢复源
Apply(Apply(s, a), b) == Apply(s, Compose(a, b))             // 组合等价两次 Apply
```

## 环境与运行

Windows 原生 Go 1.26.5，仅标准库，无第三方依赖、无外部服务、无 Docker。离线可用（可用 `GOPROXY=off` 验证）。

```bash
go run ./cmd/demo                       # 约数秒内完成（含编译），展示正常结果与一个真实触发的失败
go test ./... -count=1 -timeout=60s     # 全部测试
```

演示内容：规范化结果与源/目标长度、Apply/Invert、Compose 等价性、同位置插入（两种 ID 顺序）、完全/部分重叠删除与删除内部插入、150 个固定种子随机样本的代数关系校验，以及一个被代码实际触发并拒绝的失败（`Transform` 使用相同 ID → `ErrSameID`）。

## 代码组织

| 文件 | 内容 |
| --- | --- |
| `ot.go` | 段/Op 类型、`New` 校验与规范化、长度与溢出检查 |
| `apply.go` | `Apply`、`Invert` |
| `compose.go` | `Compose` |
| `transform.go` | `Transform`、ID 校验 |
| `util.go` | rune 工具函数 |
| `ot_test.go` | 单元测试与固定种子（20260919 / 4242）随机属性测试 |
| `cmd/demo/main.go` | 演示程序 |

随机验证使用固定种子、少量小样本（源文本 0–11 个 code point，字符集覆盖 ASCII、CJK、emoji 与组合字符），不做长时间压力测试。
