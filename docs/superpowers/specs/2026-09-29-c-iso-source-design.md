# C ISO 来源设计（落库 + 远程下载 + 本地上传 + 断点续传）

日期：2026-09-29
状态：设计定稿，待实现
关联：SPEC §2.1、§9.1、§12.6、`docs/contracts/http-console.md` §3、`docs/contracts/filesystem.md`
并行：与 spec A / B 基本不耦合，唯一接触点是制品集创建端点

## 1. 目标与验收判据

让 ISO 制品的来源从「只能填登记服务器本地路径」扩展到三种（需求 13）：本地路径、
远程下载链接（服务器去拉）、浏览器上传。两条传输通道都要**断点续传**。

**验收判据**：一张 5 GB 的 ISO 传到一半杀掉 lober 进程，重启后接着传完并导入成功。
远程下载与本地上传各验一次。

### 1.1 先修一个现存缺口

**来源路径现在根本不落库。** `internal/console/artifact.go:40-47` 把这个缺口写成了
注释：`model.ArtifactSet` 没有承载来源路径的字段（只有 `SourceISOSHA256` 记内容摘要），
所以导入只能在**登记那一次 HTTP 请求的生命周期内**触发——进程一重启，在途导入中断，
而没有任何地方能重新取到那个路径。

这意味着现在连「重试导入」都做不到。加远程下载与上传之前必须先补这一层，否则断点
续传无从谈起（续传的前提是知道在传什么、传到哪了）。

### 1.2 不在本设计范围

| | 理由 |
|---|---|
| ISO 内容识别、抽取、manifest | `internal/imports` 现有能力，不改 |
| 制品集的 `family` 字段与交叉校验 | spec A §2.1 / spec B §2.1 |
| 制品由 nginx 直发 | `docs/contracts/filesystem.md` 现有契约，不改 |

## 2. 数据模型：`model.ArtifactSource`

新增一张表，与制品集一对一（制品集删除时级联删来源）。

```go
type SourceKind string
const (
    SourceKindLocal  SourceKind = "local"   // 登记服务器本地路径（现有行为）
    SourceKindURL    SourceKind = "url"     // 服务器远程下载
    SourceKindUpload SourceKind = "upload"  // 浏览器分片上传
)

type SourceState string
const (
    SourcePending  SourceState = "pending"   // 已登记，尚未开始取
    SourceFetching SourceState = "fetching"  // 正在下载/接收分片
    SourceComplete SourceState = "complete"  // 字节已齐备，可交给导入
    SourceFailed   SourceState = "failed"
)

type ArtifactSource struct {
    ID            string
    ArtifactSetID string

    Kind SourceKind
    Role SourceRole   // iso | netboot | winpe-wimboot | winpe-bcd | ...

    // URI 的含义按 Kind 分：local 是本地路径，url 是下载链接，
    // upload 是落地后的暂存路径（上传开始时即确定，不等传完）。
    URI string

    // ReceivedBytes 是已落地的字节数，续传的唯一依据。
    // TotalBytes 为 0 表示未知（远端未给 Content-Length）。
    ReceivedBytes int64
    TotalBytes    int64

    // ExpectedSHA256 是运维登记时填的期望校验值，可空。
    // 非空时字节齐备后比对，不符则置 failed —— 气隙环境里这是唯一能发现
    // 「传过来的 ISO 本身就不对」的手段。
    ExpectedSHA256 string
    ActualSHA256   string

    State      SourceState
    FailReason string

    CreatedAt int64
    UpdatedAt int64
}
```

`Role` 是一个来源集合的必要维度：Debian 需要 ISO 加 netboot 两个来源
（`console/artifact.go:67-79`），Windows 还要 WinPE 四个文件（spec B §4）。**一个制品集
有多条来源记录**，全部 `complete` 才触发导入。

`SPEC §2.1` 与现有实现都是「不接受上传流」（气隙环境以本地路径为主）。本设计**改写
这条范围决定**：上传与远程下载都接受，理由是运维的 ISO 未必已经在 lober 这台机器上，
而先 scp 再填路径这一步在有 Web 界面的前提下是纯粹的摩擦。`local` 作为 `Kind` 之一
保留，现有行为不破坏。

## 3. 落盘布局

来源字节落在 `/srv/lober/incoming/<sourceID>/`，导入完成后按需清理。这是
`docs/contracts/filesystem.md` 的契约新增，要追加一条变更记录。

```
/srv/lober/incoming/<sourceID>/
    data                  ← 目标文件，续传就是往它尾部追加
    data.part/<N>         ← 仅 upload：已收到的分片，按序号落地
```

**`incoming/` 不能在 nginx 的 `location /artifacts/` 之下**：那里是公开直发的，把
在传的 ISO 放进去等于让装机 VLAN 上的裸机能下载它。目录必须在制品树之外。

## 4. 远程下载（`kind=url`）

服务端起 goroutine 拉取，用 HTTP `Range` 续传。

```
1. 读 ArtifactSource.ReceivedBytes
2. ReceivedBytes > 0 时发 Range: bytes=<ReceivedBytes>-
3. 远端返回 206 → 以 O_APPEND 追加写 data
   远端返回 200（不支持 Range）→ 截断 data 从头开始，ReceivedBytes 归零
4. 每写满一个刷新间隔更新 ReceivedBytes（见下）
5. 字节齐备 → 算 sha256 → 比对 ExpectedSHA256 → state=complete
```

**`ReceivedBytes` 的刷新节奏**：每 8 MB 或每 2 秒（取先到者）写一次库。太密会让一次
5 GB 下载产生几百次 UPDATE，太疏会让崩溃后重复下载的量变大。刷新前必须先
`fsync(data)`——先写库后落盘的顺序会在断电时产生「库说收了 3 GB，盘上只有 2.9 GB」，
续传从错误的偏移接着写，产出一个**校验能通过长度但内容错位的文件**。顺序是
fsync 然后更新库，宁可重传一段也不要错位。

**进程重启后的恢复**：启动时扫 `state=fetching` 的来源，逐条与盘上 `data` 的实际大小
比对，取**两者的较小值**作为续传起点（库可能领先于盘）。`cmd/lober` 的启动自检
（`-check`）里加这一步。

## 5. 本地上传（`kind=upload`）

分片 POST，每片默认 8 MB。三个端点：

| 端点 | 作用 |
|---|---|
| `POST /artifact-sets/{id}/sources` | 登记一条 upload 来源，返回 `sourceId` 与分片大小 |
| `PUT /artifact-sets/{id}/sources/{sourceId}/chunks/{n}` | 上传第 n 片（0 起） |
| `POST /artifact-sets/{id}/sources/{sourceId}/complete` | 声明传完，服务端组装并校验 |

`GET /artifact-sets/{id}/sources` 返回每条来源的 `receivedBytes` / `totalBytes` /
`state`，以及 upload 来源**已收到的分片序号集合**——前端据此只重传缺的片，不重传全部。

分片先各自落地 `data.part/<N>`，`complete` 时按序号拼接成 `data`。不直接按偏移写同一个
文件：并发上传多片时按偏移写需要文件锁，而分片落地天然无冲突，代价只是一次顺序拼接。

**幂等**：同一个分片重复 PUT 直接覆盖并返回成功。网络抖动导致前端不确定某片是否成功
是常态，报 409 会让前端无从处置。

## 6. 安全

本设计是**本系统唯一接受外部字节与外部 URL 的面**，以下每条都要落成测试。

### 6.1 路径穿越

`sourceID` 与分片序号是落盘路径的组成部分，两者**必须由服务端生成或严格校验**：
`sourceID` 用 UUID（不接受客户端提供），分片序号必须是非负整数且小于按 `totalBytes`
算出的分片总数。

运维填的文件名**不进路径**。上传的原始文件名只作为 `ArtifactSet.Name` 的默认值展示，
落盘一律用 `sourceID/data`。带 `../` 的文件名在这套布局下无处可施。

### 6.2 远程下载的 SSRF

气隙内网不等于没有内部攻击面——本进程自己就有三个 listener（网关、控制台、syslog
UDP），同机可能还有 PostgreSQL 与 nginx。下载 URL 由运维填写，必须校验：

| 规则 | 理由 |
|---|---|
| scheme 只允许 `http` / `https` | `file://` 能读本机任意文件，`gopher://` 之类可构造协议走私 |
| 拒绝 loopback（`127.0.0.0/8`、`::1`）与 `0.0.0.0` | 指向本机 listener |
| 拒绝链路本地（`169.254.0.0/16`、`fe80::/10`） | 云元数据端点惯用地址 |
| **解析后的 IP 再校验一次**，且用解析到的 IP 直连 | 只校验主机名会被 DNS rebinding 绕过：校验时解析到合法 IP，实际连接时解析到 127.0.0.1 |
| 可选的 `allowlist` 配置项（CIDR 列表） | 内网部署通常只需要从一两个已知镜像站拉；配了就只允许这些 |
| 重定向最多 3 跳，每跳按上述规则重新校验 | 远端用 302 指向 127.0.0.1 是最省事的绕过 |

### 6.3 认证、授权与审计

三个上传端点与来源登记端点全部走 `internal/console` 现有的 RBAC 与审计中间件，权限
码沿用 `codeArtifactImport`。**不新开免认证通道**——这条要显式写下来：上传端点接受
大体积 body，若漏了认证，装机 VLAN 上的任何机器都能往服务器写满磁盘。

来源登记与 `complete` 两个动作写审计（谁、什么时候、从哪个 URL 或上传了什么）。分片
PUT 不逐片写审计（一张 5 GB 的 ISO 有 640 片，会把审计表冲垮）。

### 6.4 容量与清理

| 项 | 处置 |
|---|---|
| 单个来源的大小上限 | 配置项，默认 16 GB。超过在 `complete`（或 Content-Length）时拒绝 |
| 磁盘水位 | 开始接收前检查 `incoming/` 所在文件系统剩余空间，不足则拒绝并说明还缺多少 |
| 未完成来源的过期清理 | 启动时与每 6 小时一轮：`state=pending/fetching` 且 `UpdatedAt` 超过 7 天的，删目录与记录 |
| 导入成功后的 `data` | **保留**，不自动删。`SourceISOSHA256` 的重新校验与「重试导入」都要它；删除由运维显式操作（删制品集时级联） |

单个来源上限做成配置项而不是写死：Windows Server 的 ISO 加 WinPE 四件套接近 10 GB，
而 ESXi 只有几百 MB，一个固定值对两者都不合适。

## 7. 与导入的接缝

来源齐备后触发导入，沿用现有的 `ArtifactImporter` 窄接口（`console/artifact.go:48`）与
「接口定义在 console 包内、实现放 `cmd/lober` 装配点」这套接缝——`console` 不能 import
`internal/imports`（`archtest` 的依赖方向守卫，唯一豁免是装配点）。

改动：`ImportSource` 从「handler 收到的原始请求值直接转发」改为「**由装配点从
`ArtifactSourceStore` 读出来**」。这是本设计修掉 §1.1 那个缺口的地方：导入不再依赖
某一次 HTTP 请求的上下文，因此
 
- 「重试导入」成为可能（来源还在库里，直接重新触发）
- 进程重启后在途导入可恢复
- 触发时机与登记请求解耦

`ImportSource` 的字段由 `Role` 映射：`iso` → `ISOPath`，`netboot` → `NetbootPath`，
四个 `winpe-*` → `WinPE`（spec B §4）。

**新增 `POST /artifact-sets/{id}/reimport`**：来源 `complete` 而制品集 `failed` 时重新
触发导入。这是 §1.1 缺口修好后顺带得到的能力，实现成本接近零，但它消掉了「导入失败
只能删掉重新登记、重新传 5 GB」这个现状。

## 8. 前端

`views/artifact-sets/index.vue`（309 行）的创建弹窗改成来源三选一：

```
来源  ○ 服务器本地路径   [/data/isos/rocky-9.4.iso            ]
      ○ 远程下载链接     [http://mirror.internal/rocky-9.4.iso]
      ● 本地上传         [选择文件] rocky-9.4-x86_64-dvd.iso
                         ████████████░░░░░░░░  61%  3.1/5.0 GB
      期望 SHA256（可选） [                                    ]
      族                 [linux ▾]
```

上传走 `fetch` 逐片 PUT，前端维护重试。页面刷新或关闭后重新打开该制品集，从
`GET .../sources` 拿到已收到的分片集合，**可以接着传**（浏览器侧需要用户重新选同一个
文件——File 对象不能持久化，这一点要在 UI 上写清，不要让运维以为刷新页面就丢了）。

列表页的 `state` 列在 `importing` 之前多一个来源阶段的展示：`fetching 61%`。现在这里
只有制品集自己的四个状态，看不出「还在传」与「传完了正在解压」的区别。

## 9. 风险

| # | 风险 | 缓解 |
|---|---|---|
| 1 | 库里的 `ReceivedBytes` 与盘上实际大小不一致，续传写错位置 | fsync 先于更新库；恢复时取两者较小值（§4）。专门构造「库领先于盘」的测试用例 |
| 2 | SSRF 绕过（DNS rebinding、重定向） | 解析后的 IP 再校验并用 IP 直连；重定向每跳重校（§6.2）。这三条各写一个测试 |
| 3 | 大 body 打满 gin 的默认 multipart 内存 | 分片 PUT 用 `io.Copy` 流式写盘，不用 `c.FormFile`（后者会把整片读进内存或临时文件）；显式设 `MaxMultipartMemory` |
| 4 | 并发上传多个 ISO 打满磁盘 | 开始接收前查水位（§6.4）；单来源上限配置项 |
| 5 | `incoming/` 误落在 nginx 直发路径下 | §3 已写明约束；`deploy/nginx` 配置与 `cmd/lober` 启动自检各加一道断言（自检时检查 `incoming` 不是 `artifacts` 的子目录） |
| 6 | 分片拼接时磁盘峰值占用翻倍（分片 + 拼接结果同时存在） | 拼接后立即删 `data.part/`；水位检查按 2 倍预留 |

## 10. 实现阶段划分

与 spec A / B 可并行。每段末尾 `make check` 全绿。

| 段 | 内容 | 出口判据 |
|---|---|---|
| C1 | `ArtifactSource` 模型 + conformance 断言 + fake + postgres + schemacheck | 两个 Store 实现同时通过契约套件 |
| C2 | 来源落库与导入解耦（修 §1.1 缺口）+ `reimport` 端点 | 登记后重启进程仍能触发导入 |
| C3 | 远程下载 + Range 续传 + 重启恢复 | 下载到一半杀进程，重启后续传完成 |
| C4 | SSRF 校验（scheme / loopback / 链路本地 / 解析后 IP / 重定向每跳） | 五条规则各一个测试 |
| C5 | 分片上传三端点 + 幂等 + 拼接 + 校验 | 缺片重传只传缺的那片 |
| C6 | 容量、水位、过期清理 | 水位不足时拒绝并给出缺口数值 |
| C7 | 前端来源三选一 + 进度条 + 断点续传交互 | 浏览器传一张 5 GB ISO，中途刷新页面后接着传完 |

