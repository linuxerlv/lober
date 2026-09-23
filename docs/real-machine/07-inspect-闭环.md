# 07 inspect 硬件采集接线：让服务端决策真的用上采集数据

> 本文档记录 P2-02（t12-inspect-wire 轨）的接线修复：`render/inspect.go`
> 渲染的采集脚本上报的 facts，从"格式对得上但没人读"到"真的驱动磁盘/网卡
> 决策，决策结果真的写进最终应答文件"。
> 依据：SPEC §5.6、§10 失败模式 5/6；`internal/provider/hwfacts.go` 文件头
> 注释记录的断口。

## 缺口是什么

修复前，仓库里已经有三块独立完成、各自测试齐全的拼图：

1. `internal/render/inspect.go`：渲染采集脚本，产出
   `LOBER_INSPECT_DISK_<n>_<field>` / `LOBER_INSPECT_NIC_<n>_<field>` 形态
   的键值对，POST 到 facts 端点。
2. `internal/provider/hwfacts.go`：`ParseHWDisks` / `ParseHWNICs`，能把
   上述键值对还原成结构化的 `HWDisk` / `HWNIC`，有完整的畸形输入测试
   覆盖。
3. `internal/provider/diskplanner.go` / `nicplanner.go`：五个磁盘策略与
   网卡决策逻辑，纯函数，测试齐全。

但这三块拼图**从未被拼在一起**。`hwfacts.go` 自己的文件头注释已经诚实
记录了这一点：

> `render/inspect.go` 采到的磁盘事实目前无法进入磁盘决策。

真正接住磁盘规划输入的 `internal/provider/resolve.go` 里的
`decodeDisksFromFacts`，读的是另一个键（`model.FactDisks`，即
`LOBER_DISKS`），用的是另一种自造的 `"|"` 分隔格式——而**没有任何生产
代码把 `LOBER_INSPECT_DISK_*` 的采集结果编码成这种格式写进
`LOBER_DISKS`**。`internal/console/dryrun.go` 的 `parseDisksFact` 同样
读 `LOBER_DISKS`，但用的是第三种、又不一样的 `","`/`";"` 分隔格式。

三种格式互不兼容，其中两种（`resolve.go` 与 `dryrun.go`）读的是同一个
键但格式不同，第三种（`hwfacts.go`，真机实际产出的格式）没有任何生产
调用点。net 结果：**真机采集回来的磁盘事实，进不了磁盘决策，也进不了
dry-run 端点**——`scripts/e2e.sh` 绕过这一层直接用 `sqlite3` CLI 写
`LOBER_DISK_PRIMARY`，掩盖了这个断口。

## SPEC 怎么说

SPEC §5.4 的 fact 命名空间表格给出了关键区分（本文档反复引用的一行）：

```
LOBER_DISKS              by-id 列表（inspect 采集）
LOBER_DISK_PRIMARY       服务端决策结果
```

`LOBER_DISKS` 是**采集输入**，`LOBER_DISK_PRIMARY` 是**决策输出**。两者
是同一条流水线的两端，中间必须经过服务端决策（§5.6：先采集，再服务端
决策，再渲染）。

SPEC 没有冻结 `LOBER_DISKS` 本身的字符串序列化格式（这是本次修复前就
存在、修复后仍然存在的契约缺口，见下文 contractGaps）。

## 怎么修的

没有去猜第四种 `LOBER_DISKS` 格式，而是绕开这个未冻结的格式问题：
`decodeDisksFromFacts` 与 `dryrun.go` 的 `parseDisksFact` 都改为直接
消费 `LOBER_INSPECT_DISK_*`——这是 `render/inspect.go` 真机采集脚本
实际产出、`hwfacts.go` 已经实现并测试过解析的格式，不是新发明的东西。

- **`internal/provider/resolve.go`**：`decodeDisksFromFacts` 改为调用
  同包的 `ParseHWDisks`，把 `HWDisk` 逐字段转换为 `contract.Disk`
  （字段集合完全一致：`ByIDPath`/`SizeBytes`/`Rotational`/`Model`），
  删除原来自造的 `"|"` 分隔解析逻辑。
- **`internal/console/dryrun.go`**：`parseDisksFact` 改为在本文件内
  重新实现同一套 `LOBER_INSPECT_DISK_` 前缀键分组解析（不能直接调用
  `provider.ParseHWDisks`——console 与 provider 是两个互不可见的实现
  模块，archtest 规则三禁止跨模块 import）。
- **`internal/provider/nicplanner.go` / `hwfacts.go`**：未改动实现，
  本轨确认其纯函数性质（无 IO/时钟/随机数）并补充了闭环测试。
- **`internal/render/inspect.go`**：未改动实现，本轨补充了闭环测试
  证明其产出格式与决策结果消费端对齐。

修复后的完整链路（跨两个互不可见的模块，因此闭环证明也拆成两半）：

```
render/inspect.go 采集脚本
  → POST LOBER_INSPECT_DISK_<n>_<field> 到 facts 端点
  → provider/hwfacts.go ParseHWDisks 解析
  → provider/resolve.go decodeDisksFromFacts 转换为 []contract.Disk
  → provider/diskplanner.go 的具名策略决策
  → 决策结果（by-id 路径）写入 resolved_params[LOBER_DISK_PRIMARY]
  → （装配点职责，不在本轨范围）合并进 RenderContext.Facts
  → render/kickstart.go 等渲染器读 ctx.Facts[LOBER_DISK_PRIMARY]
  → 应答文件里的 --ondisk=/dev/disk/by-id/wwn-0x... （不是 /dev/sda）
```

网卡决策链路（`decideAndAppendInstallNICMAC`）在本轨修复前就已经正确
接线（P2-02 早前的任务已完成），本轨未改动其逻辑，只确认并补充测试。

## 测试证明闭环的方式

`render` 不能 import `provider`（archtest 规则三），闭环的四步因此拆成
两组测试，各自证明"自己这一半对得上"：

- `internal/provider/hwfacts_test.go` 的
  `TestFullLoop_RealisticInspectOutputThroughParseAndPlan`：真实形态的
  两盘采集事实（容量精确到字节、型号串含空格与厂商前缀）→
  `ParseHWDisks` → 真实的 `diskPlannerLargest`（不是测试替身）→
  验证决策选中容量更大的盘，且决策结果是 `/dev/disk/by-id/...` 路径。
- `internal/render/inspect_test.go` 的
  `TestInspect_FullLoop_DecisionResultReachesRenderedAnswerFileAsByIDPath`：
  把上一测试会产出的同一个 by-id 路径放进
  `RenderContext.Facts[LOBER_DISK_PRIMARY]` → `AssembleKickstart` →
  验证应答文件的 `--ondisk=` 指令用的是这个 by-id 路径，且断言应答文件
  不包含任何内核分配的设备名（`/dev/sda` 等）。

两个测试共享同一个场景（2TB 的 `wwn-0x5000c500bbb` 胜出），拼起来即是
SPEC §5.6 要求的完整顺序：采集 → 解析 → 决策 → 渲染。

其余锁住的行为：

- `TestFullLoop_MalformedRecordDoesNotPanicAndStillDecidesFromRest`：
  一条畸形磁盘记录不 panic、不污染其它记录的决策。
- `TestInspect_FullLoop_MissingDiskPrimaryFactDoesNotFallBackToKernelName`：
  没有决策结果时不会静默回退到内核设备名（那会掩盖"没有决策"这个事实）。
- `internal/console/dryrun_test.go` 的
  `TestDryRun_RealPlannerConsumesParsedDisksWithNonEmptyReasoning`：
  dry-run 端点用真实容量比较逻辑（不是忽略 `disks` 参数的 stub）验证
  `parseDisksFact` 的解析结果真的能驱动出正确选择、理由非空。
- `TestParseDisksFact_MalformedFieldSkipsEntryNotPanic`：dry-run 端点
  的本地解析实现同样对畸形输入健壮。

## 纯函数自查

`nicplanner.go`、`hwfacts.go`、`diskplanner.go`（只读引用，未改动）、
`resolve.go` 新增的 `decodeDisksFromFacts` 均用
`grep -nE "time\.|rand\.|os\.|ioutil\.|exec\.|net\.|http\."` 自查确认
零命中——无 IO、无时钟、无随机数。

## 契约缺口（未解决，留给契约持有者）

1. **`LOBER_DISKS` 键本身没有冻结的字符串序列化格式**。SPEC §5.4 只
   给了 `LOBER_NICS` 的格式示例（`eth0:aa:bb:cc:...:up,eth1:...:down`），
   没有给 `LOBER_DISKS` 对应的示例。本轨的修复没有去填这个键——它绕开
   了这个未冻结的问题，直接消费 `LOBER_INSPECT_DISK_*`（采集脚本与
   `hwfacts.go` 之间已经存在、已经测试过的私有编码），这是本轨文件
   归属范围内能做到的最小、最正确的接线。`LOBER_DISKS` 该长什么样、
   谁负责把 `LOBER_INSPECT_DISK_*` 编码成 `LOBER_DISKS` 写回 facts KV
   （如果确实需要这一步——也可能正解是干脆不需要 `LOBER_DISKS` 这个
   中间形态，`LOBER_INSPECT_DISK_*` 直接就是唯一需要的采集表示），
   仍是需要走契约变更流程解决的问题。
2. **网卡决策结果没有对应的冻结 fact 键**。对比磁盘决策有
   `LOBER_DISK_PRIMARY`，网卡决策结果目前写在一个非冻结的临时键名
   `nic_install_mac`（`resolve.go` 已有文档说明，本轨未改动这部分）。
3. **`console` 与 `provider` 之间的解析逻辑重复**。`dryrun.go` 的
   `parseDisksFact` 与 `hwfacts.go` 的 `ParseHWDisks` 实现同一套
   `LOBER_INSPECT_DISK_*` 分组解析逻辑，因为两个模块互不可见、这段
   逻辑又不是冻结契约，没有 model/contract 层的公共位置可以放。若未来
   这个格式升级为冻结契约，正解是把分组解析逻辑收敛到 model 或
   contract 包一次，两个模块的调用点都改为调用公共实现。
4. **"decodeDisksFromFacts 的结果如何进入 RenderContext.Facts"这一步
   不在本轨范围内**。`resolve.go` 的 `Resolve` 方法产出
   `resolved_params[LOBER_DISK_PRIMARY]`，但 `render` 包的渲染器读的是
   `RenderContext.Facts[LOBER_DISK_PRIMARY]`——两者之间的搬运（把
   `resolved_params` 合并进 `Facts`）是 M6/装配点的职责，不在本轨
   `internal/provider` / `internal/render` 的文件归属范围内。本文档的
   闭环测试因此在"决策产出结果"与"渲染消费结果"两端分别验证，中间的
   搬运环节由持有装配点的实现者负责，未被本轨测试覆盖。

## 未接线的部分（不在本轨范围内，明确留给持有对应文件的实现者）

- `%pre` 阶段"采集上报后同步等待服务端决策结果再分区"（P2-02 计划
  任务 4）——涉及 `internal/gateway/facts.go`，不在本轨文件归属内。
- 决策失败时机器该如何处置（中止安装 vs 继续但无决策结果）——同样是
  `gateway/facts.go` 的职责。
