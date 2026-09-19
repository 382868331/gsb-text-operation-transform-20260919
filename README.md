# 文本操作变换与组合内核

纯 Go 标准库实现的文本编辑操作变换（OT）内核，用于合并两个基于同一旧文本的并发编辑，以及组合连续编辑。完整规格见 TASK.md。

## 接口（包 `ot`）

操作由三种段组成，长度一律按 Unicode code point 计数（不按字素簇处理组合字符）：

```go
type Segment struct { Kind Kind; N int; Text string } // Retain(n) / Delete(n) / Insert(text)
type Op []Segment
// 便捷构造：ot.R(n)、ot.D(n)、ot.I(text)
```

- `Normalize(op) (Op, error)` — 校验并规范化：拒绝负长度与非法 UTF-8，移除零长度段，合并相邻同类段。
- `Apply(source, op) (string, error)` — 应用操作；操作必须恰好消耗源长度，否则返回 `ErrLengthMismatch`。
- `Invert(source, op) (Op, error)` — 构造逆操作，保证 `Apply(Apply(s, op), Invert(s, op)) == s`。
- `Compose(a, b) (Op, error)` — 组合为等价于“先 a 后 b”的单操作；要求 a 的目标长度等于 b 的源长度，不借助源文本。
- `Transform(a, b, idA, idB) (aAfterB, bAfterA Op, err error)` — 对同源并发操作做变换，保证
  `Apply(Apply(s,a),bAfterA) == Apply(Apply(s,b),aAfterB)`。
  ID 必须是不同的非空 ASCII 字符串；同源位置的并发插入由字典序较小者先插入；
  删除只作用于原字符、不吞并发插入；重叠删除只生效一次。
- `SourceLen(op)` / `TargetLen(op)` — 源/目标长度（code point 数），长度算术带溢出检测。

约束：输入源文本与单操作源/目标长度至多为 `ot.MaxLen`（10000）个 code point；所有函数不修改输入。错误通过哨兵值区分：`ErrNegativeLength`、`ErrInvalidUTF8`、`ErrLengthMismatch`、`ErrOverflow`、`ErrTooLong`、`ErrBadID`。

## 运行方法

Windows 原生 Go 1.26.5，仅标准库，无第三方依赖、无网络与外部服务。

```sh
go run ./cmd/demo                          # 演示：正常结果 + 实际触发的失败（约 2 秒）
go test ./... -count=1 -timeout=60s        # 测试
```

测试覆盖：负长度、非法 UTF-8、同 ID 拒绝、同位插入、删除内部插入、完全/部分重叠删除、emoji/组合字符、空文本、长度溢出、超长拒绝、输入不可变，以及固定种子（20260919）随机小样本的收敛/逆操作/组合等价性验证。

## 实现范围

仅两个并发操作的内核：不含三操作 TP2、网络会话、撤销栈或富文本，无 UI、CI 或部署配置。
