# SUSE 端到端验证报告

对应分支 `feat/n8-suse-e2e`。验证方式：`scripts/e2e-suse.sh`（curl 模拟机器
打点，本地跑通全链路，不碰硬件），结构照抄 `scripts/e2e.sh`（RHEL 基线）与
`scripts/e2e-debian.sh` 的形状写（本轮未修改它们一个字符）。已连续跑两次，
结果稳定：**在阶段 5 必然 FAIL，退出码 1**。RHEL 基线另跑一次，95 个 PASS、
exit 0，未受影响。

## 一句话结论

**SUSE 的渲染层单元测试全绿（AutoYaST 渲染器 27 条 + golden 2 个，SUSE
抽取器 7 条），但装机链路目前会在真机上 100% 卡死在应答文件渲染这一步**
——不是偶发，是无条件必然：`internal/render/menu.go` 的 `RenderMenu` 与
`internal/render/renderer.go` 的 `RenderAnswerFile` 两个分派入口对
`model.OSFamilySUSE` 都硬编码走 `unimplementedFamilyError`，从未调用
`internal/render/autoyast.go` 里已经完整实现、且有独立单元测试覆盖的
`RenderAutoYaSTMenu`/`AssembleAutoYaST`。这两个函数是"孤岛实现"：全仓库
grep 排除它们自己的文件与测试文件后零调用点。这是本次验证发现的最严重
问题，写在最前面。

任务描述里写"`menu.go` / `renderer.go` 的分派已接线"与本次读代码+实测
的结果不一致——那句描述大概率是对 RHEL/Debian/Ubuntu/Windows/ESXi 五个
家族成立、误推广到了 SUSE，或是 AutoYaST 渲染器落地之后分派没有跟着更新。
两个 case 分支里的注释都写着"AutoYaST 的渲染实现……在本 worktree 内不
存在（已检查 internal/render/ 下所有文件……）"——这条注释本身已经过时。

## 环境与方法

本地起服务（`go build ./cmd/lober` 后 `-config ... -check` 再直接跑），
`curl` 模拟真实机器发出的 HTTP 请求，`sqlite3` CLI 直接查库核对状态与
断言依据（不依赖 jq，本机没有）。假 SUSE (openSUSE Leap 15.6) ISO 内容树
由新建的 `scripts/e2e/mkfixture-suse.sh` 生成：
`boot/x86_64/loader/{linux,initrd}` + `repodata/repomd.xml` +
`media.1/products` + `suse/{setup/descr/packages,x86_64/*.rpm}`——这是
`internal/imports/identify.go`（`repodata/repomd.xml` + `media.1/products`
组合判据）与 `internal/imports/extract_suse_test.go` 的 `suseFakeRoot()`
同款最小夹具，不是凭空编的目录形状。**在写 e2e 脚本之前，已用一段临时的
`go run` 程序（跑完即删，未留在仓库或 worktree 任何地方）直接对着这棵
落盘的目录树调用真实的 `imports.Identify` 与 `imports.ExtractSUSE`
验证过一遍**：`Identify` 返回 `suse`，`ExtractSUSE` 返回
`{Kernel:linux Initrd:[initrd] Repo:repo/ Extra:map[]}`，落盘文件
`repo/repodata/repomd.xml` 路径与 `internal/statemachine/precheck.go` 的
硬编码判据 `repodata/repomd.xml` 完全对齐——SUSE **不会**撞上
`scripts/e2e-debian.sh` 报告里记录的那个 precheck 硬编码 bug（那个 bug
只影响 `Repo` 字段指向非 yum/zypper 树的家族，SUSE 的 `repo/` 下确实有
`repodata/repomd.xml`）。

## 真实发现（按严重程度排序）

### 发现 1（阻断性 bug）：SUSE 的菜单渲染与应答文件渲染两个分派入口都硬编码拒绝，从未接线到已实现的 AutoYaST 渲染器

**位置**：
- `internal/render/menu.go` 第 46-47 行（`RenderMenu` 函数内的 switch）：
  ```go
  case model.OSFamilySUSE:
      return "", unimplementedFamilyError(ctx.Artifact.OSFamily)
  ```
- `internal/render/renderer.go` 第 85-90 行（`(*Renderer).RenderAnswerFile`
  方法内的 switch）：
  ```go
  case model.OSFamilySUSE:
      // AutoYaST 的渲染实现（AssembleAutoYaST）在本 worktree 内不存在
      // （已检查 internal/render/ 下所有文件），没有实现可接。……
      return "", unimplementedFamilyError(ctx.Artifact.OSFamily)
  ```

两处注释都声称"AutoYaST 的渲染实现……在本 worktree 内不存在"，但
`internal/render/autoyast.go`（516 行，本次验证前已存在于本 worktree）
包含：
- `AssembleAutoYaST(ctx model.RenderContext) (string, error)`（第 422 行起，
  组装完整的 `autoinst.xml` 正文，含 `<pre-scripts>`/`<chroot-scripts>`
  两个 hook 段落、SSH 公钥注入、`FirstbootScaffold` 调用）
- `RenderAutoYaSTMenu(ctx model.RenderContext) (string, error)`（第 496 行起，
  渲染 `kernel ... autoyast=... / initrd ...` 两行菜单文本）

对整个仓库 grep `AssembleAutoYaST\b` 与 `RenderAutoYaSTMenu\b`（排除
`autoyast.go` 自己与 `autoyast_test.go`），**零个调用点**。这两个函数只被
`autoyast_test.go` 的单元测试直接调用验证过，从未被 `RenderMenu`/
`RenderAnswerFile` 这两个真实的生产分派入口引用——是完整实现但从未接线
的"孤岛代码"。

**复现步骤**（本次验证脚本实测，未加任何绕过时）：
1. 用 `mkfixture-suse.sh` 造真实的 SUSE ISO 内容树
2. 因为真实导入路径走不通（见发现 2），手动按 `ExtractSUSE` 的产出结构
   搬运到制品集目录：`linux` + `initrd` + `repo/repodata/` + `repo/suse/`
   （与 `ExtractSUSE` 函数产出逐字节一致的目录形状，只是搬运动作用 shell
   代替了 Go 抽取器）
3. `paths_json` 写 `{"Kernel":"linux","Initrd":["initrd"],"Repo":"repo/",...}`，
   `state='ready'`——与 `docs/contracts/filesystem.md` 硬约束 1 完全一致，
   磁盘上的文件真实存在
4. 建模板（`active`），调用 `POST /console/v1/templates/{id}/preview`
5. 该端点内部调用 `(*Renderer).RenderAnswerFile`，命中上面的 SUSE 分支，
   返回错误
6. 实测响应体（逐字符）：
   ```json
   {"code":1000,"data":null,"message":"渲染应答文件失败: render: OS 家族 \"suse\" 的 iPXE 菜单渲染在 MVP 范围内未实现（Phase 2/3 才会补齐；SUSE 需要注意它用 AutoYaST 而非 kickstart，不能路由到本渲染器的 kickstart 分支）"}
   ```

**一处附带发现（次要，写在这里而不是单开一节，因为它是同一个阻断点的
文案细节）**：这条错误消息的措辞是"iPXE **菜单**渲染在 MVP 范围内未
实现"，但触发路径是 `preview` 端点走的 `RenderAnswerFile`（**应答文件**
渲染），不是 `RenderMenu`（菜单渲染）。原因是两个分支共用同一个
`unimplementedFamilyError(family)` 辅助函数，该函数的错误文案里写死了
"iPXE 菜单渲染"字样，`renderer.go` 的 `RenderAnswerFile` 分支调用它时
没有传入"这次是应答文件渲染还是菜单渲染"的上下文。不影响本次验证的
结论（两条路径都会被拒绝，只是拒绝时的错误消息措辞不完全贴合触发路径），
但如果这条消息会展示给运维排障，措辞歧义值得一并修。

**影响范围**：`/v1/boot` 命中时的菜单渲染（`RenderMenu`）与
`preview`/机器取应答文件时的应答文件渲染（`RenderAnswerFile`）都会对
**每一个** SUSE 任务返回错误，与该任务的制品集/主机/模板配置得多完整、
多正确无关——这不是"某种边界条件下才触发"的缺口，是这条渲染路径对
SUSE 家族 100% 必然拒绝。

**正解方向**：把 `menu.go` 第 46-47 行的 `case model.OSFamilySUSE` 分支
改为调用 `RenderAutoYaSTMenu(ctx)`，把 `renderer.go` 第 85-90 行的
`case model.OSFamilySUSE` 分支改为调用 `AssembleAutoYaST(ctx)`——两个
函数的签名（`model.RenderContext) (string, error)`）与其它家族已接线的
分支（如 `renderRockyMenu`/`AssembleKickstart`）完全一致，是纯粹的"查表
补一行"级别的改动，不需要新写任何渲染逻辑。这两个文件都不在本轮任务
（`n8-suse-e2e`）的可写文件列表内（归实现轨），本报告只记录发现、不动手
改 Go 代码。

**本次验证的应对方式**：**没有加 workaround 跳过这一步**——与
`scripts/e2e-debian.sh` 遇到 precheck 硬编码 bug 时"补一个占位文件绕过
去继续验证后续阶段"不同，本次的阻断点是渲染函数本身直接返回错误，没有
类似"补一个文件让判断走另一条分支"这种绕法可用（唯一的绕法是改
Go 代码去调用 `AssembleAutoYaST`/`RenderAutoYaSTMenu`，这超出本轨"只写
e2e 脚本"的授权范围，且会掩盖"这条路径目前是断的"这一事实）。脚本在
阶段 5 如实 FAIL 并退出，阶段 6-9（准入负面用例、菜单渲染、AutoYaST
应答文件、装机期与重启后信号、终态断言）**完全未执行**——它们全部依赖
菜单/应答文件渲染成功才能拿到 install token 继续往下走，在这个阻断解除
之前无法验证，写死的假数据硬跑下去只会验证不到任何真实链路。

### 发现 2：制品集登记 → 导入 job 已经真实接线，但 `MountExtractor.Open` 尚未实现真实挂载逻辑（全家族共享的缺口，非 SUSE 专属）

任务描述要求本脚本**先尝试真实的登记→导入→ready 路径**，走不通再退回
`sqlite3` 绕法，且要在报告里写明为什么走不通。这一点与 `e2e-debian.sh`
报告记录的"完全没接线"不同——读代码确认**这一轮已经把桥接补上了**：

- `internal/console/artifact.go` 的 `handleCreateArtifactSet` 落库之后
  调用 `s.deps.ArtifactImporter.TriggerImport(context.WithoutCancel(r.Context()), created.ID, req.SourcePath)`。
- `cmd/lober/importrunner.go`（本轮新增文件）的 `importRunner.TriggerImport`
  是生产实现，起 goroutine 调用 `imports.NewJob(r.store, r.extractor).Run(ctx, artifactSetID, isoPath, destDir)`。
- `internal/imports/job.go` 的 `Job.Run` 有 `familyExtractors` 查表分派
  （第 82-87 行），**SUSE 已在表内**：`model.OSFamilySUSE: ExtractSUSE`。

链路本身是通的，但：

- `cmd/lober/wire.go` 第 616 行：`importer := newImportRunner(st, nil, cfg.Store.ArtifactPath)`
  ——第二个参数 `extractor` 传 `nil`。
- `cmd/lober/importrunner.go` 的 `newImportRunner`：`extractor` 为 `nil`
  时兜底为 `imports.NewMountExtractor("")`。
- `internal/imports/extract.go` 第 59-64 行，`MountExtractor.Open` 的
  完整实现：
  ```go
  func (m *MountExtractor) Open(ctx context.Context, isoPath string) (fs.FS, error) {
      return nil, fmt.Errorf("imports: MountExtractor.Open 尚未实现真实挂载逻辑"+
          "（ISO=%q）——……真实的 mount(8) 调用依赖部署环境（Linux 特权、loop 设备），"+
          "留给部署适配阶段接入，不改变 Extractor 接口本身", isoPath)
  }
  ```

**这不是 SUSE 专属问题，是 RHEL/Debian/Ubuntu/SUSE 四个已在
`familyExtractors` 表内的家族共享的同一个缺口**——挂载 ISO 需要 Linux
特权与 loop 设备，本机是 Windows 开发环境，任何家族调用真实端点触发导入
都会在 `Open` 这一步 100% 失败。

**脚本内的自动化验证**：阶段 4 用真实端点落一条 `importing` 记录，取得
真实分配的 ID，轮询最多 4 秒观察状态——两次实际运行，`state` 都在约
1.5 秒后从 `importing` 自行推进到了 **`failed`**（不是像 `e2e-debian.sh`
报告里那样原样卡在 `importing` 不动——那份报告验证的是"HTTP 层完全不
可达"，这次验证的是"HTTP 层可达、job 真的异步跑了，但卡在 `Open` 这一
步"，是不同性质的观测，脚本按实际观察到的状态分支处理，不是照抄旧报告
的断言方向）。服务端日志逐字符确认失败原因：
```
level=ERROR msg="导入 job 执行失败" artifact_set_id=<真实ID> iso_path=<真实路径> error="imports: 导入失败: 打开 ISO 失败: imports: MountExtractor.Open 尚未实现真实挂载逻辑（ISO=\"<真实路径>\"）——……"
```
与读代码分析完全一致。

**结论**：确实走不通，退回与 `scripts/e2e.sh`/`scripts/e2e-debian.sh`
相同的绕法——用真实端点落一条 `importing` 记录取得真实分配的 ID，再用
`sqlite3` CLI 推进到 `ready` 并写回 `paths_json`。文件本身仍是
`mkfixture-suse.sh` 造出的真实文件、真实落在制品根目录下；绕过的只是
"谁执行真实的 ISO 挂载"这一个缺失的实现（部署环境适配，非本轮范围）。

**正解方向**：`MountExtractor.Open` 需要真正调用 `mount -o loop`（或
Windows 环境下的等价机制）并返回挂载点的 `os.DirFS` 视图，或者在测试/
CI 环境下改用一个不需要真实块设备的替代实现（比如直接对一个已解压好的
目录做 `os.DirFS`，跳过"挂载 ISO 文件"这一步，只要 `sourcePath` 本身
指向的是目录而不是 `.iso` 文件）——这不在本轮任务的可写文件列表内
（`internal/imports/**`、`cmd/lober/**` 都不归 `n8-suse-e2e` 轨），本
报告只记录发现。

## 九个阶段逐一核对（实际执行情况）

| 阶段 | 内容 | 实际结果 |
|---|---|---|
| 1 | 造 ISO 内容树、二进制、admin、配置 | **PASS**——新建 `mkfixture-suse.sh`，`repodata/repomd.xml` + `media.1/products` 组合特征，`-check` 通过 |
| 2 | 起服务 | **PASS**——网关端点 15 秒内就绪 |
| 3 | 登录 | **PASS**——拿到 access token |
| 4 | 主机+制品集+模板 | **PASS**——先验证真实导入路径（观测到 job 异步执行后置为 `failed`，见发现 2），退回 sqlite3 绕法，落盘文件路径与 `ExtractSUSE` 产出结构逐一核对存在 |
| 5 | preview 渲染 | **FAIL（预期内的真实阻断）**——`RenderAnswerFile` 对 `OSFamilySUSE` 硬编码拒绝（见发现 1），脚本到此停止 |
| 6 | 四条准入负面用例 | **未执行**——依赖阶段 5/7 产出的 install token |
| 7 | `/v1/boot` 命中签 token | **未执行**——`RenderMenu` 会命中同一个硬编码拒绝分支，即使跑到这一步也必然复现同一个阻断，不是"可能跑通" |
| 8 | 取应答文件 + 信号 | **未执行** |
| 9 | 终态 + 防循环重装 | **未执行** |

四条安全负面用例（§9.1 三道闸门 + §10 失败模式 1/2b）**均未验证**——它们
全部要求先有一个真正进入 `booting`/`installing` 状态的任务，而这条路径
在阶段 5 就已经断了。这不是"本次验证省略了这四条"，是"这四条在当前 Go
代码状态下对 SUSE 家族根本无法达成前置条件"。

## RHEL 基线回归确认

另跑两次 `bash scripts/e2e.sh`（本轮未修改任何 Go 代码或其它 e2e 脚本），
两次结果一致：**18 个 `[PASS]` 标记（对应九个阶段+四条负面用例的完整
分解，其中阶段 6 额外有一条汇总 PASS），exit 0**，与本轮开始前的已知
状态一致，未受影响。

## 本次验证未覆盖的部分（含仍需真机验证的项）

- **阶段 6-9 完全未执行**（见上表）——AutoYaST 应答文件的内容结构（四个
  hook 阶段是否真的都在渲染出的 XML 里、SSH 公钥注入、firstboot 脚手架、
  十一个信号点）**只有单元测试层面的覆盖**（`autoyast_test.go` 的 27 条
  测试 + 2 个 golden），**没有经过本次 e2e 验证——因为渲染函数从未被真实
  分派入口调用过，e2e 层面根本拿不到任何 AutoYaST 正文可供检查**。这与
  "渲染出的内容有瑕疵"是完全不同性质的问题，测试全绿≠端到端可用，这正是
  本轮任务开篇提到的"隔着一整条链路"的具体例证。
- 真实 SUSE 安装器（YaST/AutoYaST）是否真的能解析并执行渲染出的
  `autoinst.xml`——本次验证连"渲染出内容"这一步都没走到（发现 1 挡住了），
  完全无法评价这一点。即使发现 1 被修复、渲染函数被接线，仍然需要真实
  SUSE 安装介质（openSUSE Leap 15.x 或 SLES 15+ 的官方 ISO）与真实虚拟机/
  物理机跑一次真装机，才能验证 `<pre-scripts>`/`<chroot-scripts>` 两个
  段落、`<chrooted>` 布尔子元素、CDATA 包裹的脚本正文这些结构是否真的
  被 YaST 正确解析执行——`autoyast.go` 文件头注释里已经记录的"未核实到
  官方文档明确给出的部分"（`<feedback>`/`<notification>` 交互式弹窗子
  元素、`<interpreter>` 默认值）同样需要真机验证。
- syslog 通道（UDP，curl 发不出，同其它 e2e 脚本的已知限制）。
- `MountExtractor.Open` 的真实挂载逻辑修复后，是否真的能对着一份真实的
  openSUSE/SLES ISO 文件（而不是本次验证用的、已经是目录形式的假夹具）
  正确挂载并被 `imports.Identify`/`imports.ExtractSUSE` 处理——本次验证
  全程使用的都是普通目录而非 `.iso` 文件本身，`MountExtractor` 针对
  `.iso` 文件本身的挂载行为完全没有被本次验证触及。

## 交付文件

- `scripts/e2e-suse.sh`（新建，可重复执行，已连续验证两次，结果稳定
  为阶段 5 FAIL、exit 1）
- `scripts/e2e/mkfixture-suse.sh`（新建）
- 本报告
