# 17 ISO 内容读取：给 MountExtractor 接上真实的 ISO 9660 解析

> **数据层迁移说明**：本文记录的是数据层仍为 SQLite 时的情况，数据层已于
> 2026-09-24 迁移到 GORM + PostgreSQL（见
> `docs/superpowers/specs/2026-09-24-gorm-postgres-migration-design.md`），
> 正文保留当时原貌，未作修改。

> 本文档记录 m3-mount-extractor 这次工作：把 `internal/imports/extract.go`
> 的 `MountExtractor.Open` 从骨架（固定返回「尚未实现」错误）接上真实的
> ISO 内容读取。依据：实现计划任务 9 的骨架说明、SPEC §2.1 的特征文件表。

## 现状：导入管线只在内存文件系统层成立

在本轮工作开始之前，`Identify`（identify.go）与六个家族的 `ExtractXxx`
（extract_debian.go 等）都已经实现且有完整单元测试覆盖，但全部测试
构造的是 `testing/fstest.MapFS` 内存文件系统，从未读过一份真实 ISO。
真实调用路径上的唯一入口——`MountExtractor.Open`——固定返回一个字符串
「尚未实现真实挂载逻辑」的错误。

这个骨架产生了一个具体后果：`scripts/e2e*.sh` 全部端到端脚本为了绕开
这个空缺，直接用 `sqlite3` CLI 写 `artifact_sets` 表把制品集状态推到
`ready`，文件本身虽然是真实落在磁盘上的（`mkfixture.sh` 造的），但没有
一次 `imports.Job.Run` 真正跑过——「导入管线已接上」这个结论此前只在
单元测试层成立。

## 方向选择：纯 Go 解析，不真的 mount，不调外部工具

骨架原来的设计判断是「真实 mount(8) 依赖 Linux 特权与 loop 设备，风险
高于收益，交给部署环境适配」。但读 ISO 内容本身不需要挂载文件系统——
ISO 9660 是有公开规范的格式（ECMA-119，等同 ISO/IEC 9660:1988），可以
纯用户态解析。三个可选方向的取舍：

1. **自己实现 ISO 9660 读取器（选中）**：纯 Go、零外部依赖（本项目
   禁止新增 go.mod 依赖）、可在任何平台单测、不需要特权。本轮做的正是
   这个方向，实现在新增文件 `internal/imports/iso9660.go`。
2. **调用外部工具**（7z/bsdtar/isoinfo）：本机确认可用 `xorriso`
   （`command -v xorriso` 命中版本 1.5.6），但没有采用——气隙部署环境
   下无法保证目标机器也装了这个工具，且会引入 `os/exec` 之外的运行时
   依赖。**若未来需要改用这个方向**，`xorriso -osirrox on -indev
   <iso> -extract_all <目录>` 是能用的命令形态，但依赖链要写进部署
   文档并在启动自检里加一次「工具是否存在」的检查。
3. **真的 mount(8)**：Linux 专属、需要特权，本机（Windows 开发环境）
   连语义都不成立，选它会导致这部分代码永远无法在本机验证，直接违反
   「完成前必须真的执行验证」的铁律，本轮未采用。

## 支持范围：两种扩展都实现了，但各自只覆盖各自的场景

方向 1 的工作量集中在目录记录解析与两种「保留真实文件名」的扩展机制
上——纯 ISO 9660 规范本身会把文件名强制大写、做 8.3 截断、加 `;1`
版本号后缀，这与本模块调用方（`identify.go`、各 `ExtractXxx`）里写死
的精确路径字符串（`.treeinfo`、`sources/install.wim`、
`boot/x86_64/loader/linux` 等）完全不匹配，若不做任何扩展支持，读取器
在真实 ISO 上会全面失效。

**Joliet**（补充卷描述符，转义序列 `%/@`/`%/C`/`%/E`）：完整支持，解析
UCS-2/UTF-16BE 编码的长文件名。这是 Windows 安装 ISO 的惯例做法，
`sources/install.wim` 这条路径依赖它才能被精确匹配到。

**Rock Ridge（RRIP）**：只支持 `NM`（备用名字）系统使用字段，这是恢复
`.treeinfo`、`boot/x86_64/loader/linux` 这类含前导点/要求保留小写路径
所需的最小子集——RHEL/Debian/Ubuntu/SUSE/ESXi 的真实安装 ISO 惯例上用
Rock Ridge 而不是 Joliet 保留这些名字（`xorriso --help` 的输出印证了
这一点：`-rockridge`/`-joliet` 是两个独立开关，`-read_fs` 支持
`norock`/`nojoliet` 两种单独禁用）。**明确不支持**的部分：

- **CE 延续区**：若一个 `NM` 名字长到系统使用区本身放不下、需要跳到
  另一个 extent 继续，本解析器不会跟随 CE 指针，只返回系统使用区内能
  直接拿到的片段。真实世界的这几个特征文件名都很短，不会触发这个限制。
- **PX**（POSIX 权限位/属主）、**SL**（符号链接）、**CL/PL**（重定位
  目录）等其它 RRIP 字段一律不解析——本模块的全部调用方只需要「文件在
  哪、多大、是不是目录」三件事。

这个选择的一个直接推论：**若某天需要读取的特征路径本身依赖符号链接
跟随、或深到需要 CE 延续区，当前实现会静默按「找不到这个文件」处理**
（而不是 panic 或误报），因为 `fs.Stat` 只是找不到条目、返回
`fs.ErrNotExist`——这与「找不到」本身的语义是一致的，只是原因不同
（真找不到 vs. 名字被截断导致查不到）。

## 一个真实踩到的字节布局 bug：SUSP 填充规则的方向搞反了

写测试夹具生成器（`iso9660_test.go` 的 `buildISO9660`）时，为了验证
Rock Ridge 路径，需要在写入端也正确实现 ECMA-119 §9.1.12 的填充规则：
「若文件名字段长度为偶数，则在其后补 1 字节填充，使系统使用区从偶数
偏移开始」。第一版写入端把判断条件写反了（判断 `len(nameBytes)%2 != 0`
时才填充，即在**奇数**长度时填充），而读取端（`iso9660.go` 的
`parseDirectoryRecord`）判断条件是对的（`nameLen%2 == 0` 时才填充）。

这个不对称在纯 ASCII 场景下产生了一个诡异的现象：`.treeinfo` 这样的
6 字节文件名（偶数）本该被填充，但写入端没填充，导致 `NM` 字段的签名
恰好写到了读取端认为是填充字节之后的偏移，读出来的两个字节不是 `"NM"`
而是别的东西——`parseRockRidgeNM` 因为签名不匹配直接跳过整段系统使用
区，Rock Ridge 名字悄悄丢失，退化成纯 ISO 9660 的大写截断名。

暴露这个 bug 的不是任何断言失败的错误信息，而是一个具体、可复现的现象：
`fs.WalkDir` 遍历出的路径是 `.TRE` 而不是 `.treeinfo`。定位方法是逐字节
打印目录记录的原始十六进制内容，手工按 ECMA-119 的字段偏移表核对——
这也是为什么本轮选择「测试夹具自己实现写入端」而不是依赖外部工具生成
固定的二进制文件：写入端本身的字节布局逻辑与读取端共享同一套规范
理解，一旦某一处理解错了，构造出的夹具会让读取端的测试直接失败，
不需要额外的交叉核对步骤就能定位到具体是哪个字段错了。

## 另一个真实踩到的问题：Windows 下的文件描述符泄漏

`MountExtractor.Open` 最初的实现是 `os.Open` 一次拿到 `*os.File`，
把它作为 `io.ReaderAt` 传给 `OpenISO9660`，返回的 `ISO9660FS` 在整个
生命周期内持有这个文件描述符。这个设计有一个结构性问题：
`Extractor` 接口的 `Open` 方法返回裸 `fs.FS`，这个接口没有 `Close`
方法，`job.go`（`Job.Run`）也从未调用任何清理——文件描述符被一直
持有，直到进程退出或垃圾回收。

这个问题不是理论上的。开发过程中给 `MountExtractor` 补的第一版端到端
测试（写一份真实 ISO 到 `t.TempDir()`，用 `MountExtractor.Open` 读它）
在测试清理阶段报错：

```
TempDir RemoveAll cleanup: unlinkat ...\test.iso: The process cannot
access the file because it is being used by another process.
```

这是 Windows 文件锁语义的真实体现，不是测试环境的偶然限制——持有打开的
文件句柄会阻止其被删除或移动。生产环境下同样的问题会表现为运维想在
导入完成后清理/移动那份 ISO 文件时被意外拒绝，或者长期运行的进程里
文件描述符随每次导入 job 持续增长。

修复方式是 `iso9660.go` 新增的 `OpenISO9660File(path string)` 与内部
类型 `perCallOpenReaderAt`：不持有跨调用的文件描述符，每次
`io.ReaderAt.ReadAt` 独立完成「打开 → 读取 → 关闭」。代价是每次内容
读取多一次系统调用，但识别与抽取阶段对同一个文件的读取次数在十到百级
（目录记录扫描 + 少数几个特征文件/内核/initrd/仓库树里的文件），这个
开销可以接受，换来的是不会在 Windows 上被文件锁卡住、也不会在长期
运行的进程里累积泄漏的文件描述符。`OpenISO9660`（接受任意
`io.ReaderAt`）本身保持不变，测试仍然用 `bytes.NewReader` 这种没有
文件描述符概念的内存实现，两者的职责边界很清楚：`OpenISO9660` 是通用
的字节解析入口，`OpenISO9660File` 是它面向真实本地文件的安全包装。

## 测试夹具：纯 Go 生成，不入库二进制文件，不依赖外部工具

`iso9660_test.go` 的 `buildISO9660` 在测试运行时动态生成合法的 ISO
9660 字节流（系统区 + 卷描述符序列 + 目录树 + 文件内容，按 ECMA-119
的扇区布局手工拼出来），不依赖任何预生成的二进制夹具文件，也不在
测试运行时调用本机确认可用的 `xorriso`。选择动态生成而不是入库一份
二进制文件的理由：生成逻辑本身就是对规范理解的验证——如果生成器写错
了字节布局，读取器会读出错误结果，测试直接失败，不需要额外的交叉
核对步骤（上一节的 bug 就是这样被发现的）。

## 测试覆盖的具体场景

- 读根目录下的文件（`.treeinfo`，带 Rock Ridge）
- 读嵌套文件（`sources/install.wim`，带 Joliet）
- 读三层嵌套路径（`boot/x86_64/loader/linux`，SUSE 的真实路径，带
  Rock Ridge）
- `fs.Stat` 对文件与目录均正确（`repodata/repomd.xml` 与 `repodata`
  目录本身）
- `fs.WalkDir` 正确遍历整棵树
- 跨多个扇区（4800 字节）的文件内容完整无错位
- 不存在的路径精确返回 `fs.ErrNotExist`（`Stat` 与 `Open` 两条路径
  都验证）
- 四类畸形/截断输入（空字节流、系统区不足、标准标识符错误、卷描述符
  序列没有终止符）均返回明确错误、不 panic
- 数据区被截断（用一个到指定偏移后开始报错的 `io.ReaderAt` 模拟）
  返回明确错误、不 panic
- Joliet 长文件名（超出 8.3 限制）能被精确读出
- 完全不带任何扩展的最基础 ISO 9660（用规范化后的大写名字查找，不假装
  能查到原始小写名字）

## 对 `MountExtractor.Open` 接线之后，端到端脚本理论上可以怎么改

本轮任务不拥有 `scripts/**`，以下写给编排方：`scripts/e2e.sh`、
`scripts/e2e-windows.sh` 等脚本文件头「已知限制」第 2 条描述的
sqlite3 绕法，理论上现在可以被替换成真实调用
`imports.Job.Run`（或 `RunRHEL`）—— `MountExtractor.Open` 已经能读出
真实构造的 ISO 内容，不再固定返回错误。但这需要先解决脚本注释里指出
的另一个真实缺口：`internal/console/artifact.go` 的
`handleCreateArtifactSet` 目前只落一条 `state=importing` 的记录就
返回，请求体里的 `sourcePath` 从未被持久化（`model.ArtifactSet` 没有
`SourcePath` 字段），装配点因此无法知道该对哪个本地路径跑导入 job——
这是一处接口形状缺口，不是本轮任务范围内的文件能解决的（见下方
contractGaps）。

## 契约缺口

`model.ArtifactSet` 没有字段承载「导入用的本地 ISO 路径」
（`internal/core/model` 是契约只读区，本任务无权修改），
`internal/console/artifact.go` 的 `handleCreateArtifactSet` 因此无法
把请求体里的 `sourcePath` 传给任何调用 `imports.Job.Run` 的装配代码。
这是「端到端脚本仍然需要 sqlite3 绕法」这一现状在本轮之后依然成立的
根本原因——不是 `MountExtractor` 没接上（它现在接上了），是「谁在什么
时候用哪个路径调用 `Job.Run`」这条装配链上，制品集的登记入口本身就
没有留出能传递本地路径的字段。正解方向：`model.ArtifactSet` 增加一个
`SourcePath`（或类似语义）字段，`SetArtifactSetContent` 的契约不需要
变（内容仍在导入完成后才知道），但制品集创建时的输入需要多一个字段。
