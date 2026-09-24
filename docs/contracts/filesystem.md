# 文件系统契约（磁盘布局）

> 冻结文档，不是计划。M5（渲染 URL）、M7（导入落盘）、M9（nginx 配置）三方的
> 共同契约。nginx 按路径找文件，Go 按同一套路径签发 URL——**不一致即引导时
> 404**（SPEC §10 运维点一）。
>
> 依据：SPEC §1.4、§2.1、§5.8、§6.3、§9.3、§10。类型字段名对应
> `internal/core/model`：`ArtifactSet`/`ArtifactPaths`（inventory.go）、
> `Hook`（authoring.go）。

## 总览：两棵服务端树 + 一棵目标机树

```
服务端（运行 Go 与 nginx 的主机）
├── /var/lib/lober/log/         日志（syslog 落盘、task 日志正文），仅 Go 进程读写
└── /srv/lober/                 制品（nginx 直发），需 nginx 可读
    ├── artifacts/               对应 nginx location /artifacts/
    └── repo/                    对应 nginx location /repo/

目标机（正在装机或已装完的机器）
└── /var/lib/lober/              见 SPEC §5.8，firstboot 脚手架落地点
```

**数据库不再是本地文件树的一部分。** 数据层已从本地 SQLite 迁移到外部
PostgreSQL 服务（连接参数见 `store.postgres` 配置项），不占用本机磁盘路径，
因此不在上面的服务端目录树里列出——它是一个独立的网络服务，不是这台主机
上的一棵目录树。连带影响：

- **备份对象从「拷 `.db` 文件」变成「`pg_dump` 这个库」。** 原先备份只需
  停机或用 SQLite 在线备份 API 拷走 `lober.db`；现在要用 `pg_dump`（或
  连续归档 WAL）对着 PostgreSQL 服务做逻辑/物理备份，备份脚本与恢复演练
  都要跟着换。
- **原「db 文件仅 Go 进程读写、nginx worker 无权访问」的权限约定，改为
  「PG 账号只授予这一个库的权限，不要复用其它业务账号」**——见下方
  「约定：权限」表。

服务端剩下两棵本机目录树刻意分开挂载点前缀（`/var/lib/lober/log/` 与
`/srv/lober/`），理由见「约定：权限」——日志绝不能被 nginx worker 进程
读到，而制品必须能被 nginx worker 进程读到，混在同一棵树下容易在权限
收紧时一次改错。

---

## 服务端布局

### 1. 数据库：外部 PostgreSQL 服务，不是本机目录树

数据层由 GORM 连接外部 PostgreSQL（连接参数 `store.postgres`：host/port/
user/password/dbname/sslmode/search_path），表结构由 GORM model tag 与
`AutoMigrate` 生成，不再有 `/var/lib/lober/db/` 这样的本机路径、也没有
`.gitignore` 需要覆盖的 `.db`/`-wal`/`-shm` 文件——这些都是本地 SQLite
时代的产物，随实现一起移除。部署与权限层面的处理见文末「约定：权限」表。

### 2. 日志树 `/var/lib/lober/log/`

```
/var/lib/lober/log/
├── syslog/
│   └── <task_id>.log            该任务 UDP :514 收到的全量 syslog 正文
└── tasks/
    └── <task_id>/
        └── <phase>.log          该任务某阶段的完整安装日志正文
```

**日志正文不入库**（SPEC §2.5）：`TaskEvent.Message` 只存尾部片段，完整日志
按 `task_id`/`phase` 落文件到本树，数据库只存路径引用。`<phase>` 取值域为
`model.Phase` 的字符串值（`pre`/`packages`/`post`/...）。仅 Go 进程读写。

### 3. 制品树 `/srv/lober/`

```
/srv/lober/
├── artifacts/
│   ├── hooks/
│   │   └── <sha256>/
│   │       └── script.sh          hook 正文，目录名即 model.HookDigest(body) 的输出
│   └── <artifact_set_id 或 name>/
│       ├── vmlinuz                ArtifactSet.Paths.Kernel 对应的实际文件
│       ├── initrd.img              ArtifactSet.Paths.Initrd[0]（可有多个）
│       ├── repo/                   ArtifactSet.Paths.Repo（离线仓库树，见下）
│       └── <extra 键>               ArtifactSet.Paths.Extra 里各键对应的文件
│           （esxi: mboot.efi、boot.cfg 模板；windows: wimboot、BCD、boot.sdi、boot.wim）
└── repo/
    （MVP 未使用独立顶层 repo/——见下方「repo 位置说明」）
```

对应 nginx（SPEC §1.4）：

```nginx
location /artifacts/ { alias /srv/lober/artifacts/; ... }
location /repo/      { alias /srv/lober/repo/; ... }
```

**repo 位置说明：** SPEC §1.4 的 nginx 示例把离线仓库单独挂 `/repo/` location
（几十万小文件，配 `open_file_cache`），但 SPEC §2.1 的 `ArtifactPaths.Repo`
字段值示例是 `rocky9.4/repo/`——即仓库树实际落在某个制品集目录**内部**的
`repo/` 子目录，而不是顶层独立的 `/srv/lober/repo/`。这是 SPEC 内部两处示例
不完全一致的地方，本文档按以下方式收敛（保守做法，不扩大也不收窄任一模块
的职责）：

- **MVP 阶段**：`ArtifactPaths.Repo` 存的相对路径就是相对于该制品集目录本身
  （如 `repo/`），最终磁盘路径是 `/srv/lober/artifacts/<artifact_set_id>/repo/`，
  由 nginx 的 `/artifacts/` location 一并服务（沿用同一个 `alias`，不需要
  `/artifacts/` 与 `/repo/` 两个 location 分别配置 `open_file_cache`）。
- **`model.BaseURLs.RepoURL` 与 `ArtifactURL` 当前实现完全相同**（都拼
  `base + /artifacts/ + segments`，见 M1 model 包作者说明第 7 点）——这与
  本文档的收敛方式一致：MVP 阶段两者行为理应一致，`RepoURL` 是为将来仓库
  挪到独立 location 预留的接缝，不是当前就要落地的分离。
- 若未来仓库规模需要独立 `open_file_cache` 参数（SPEC §1.4 示例的原始意图），
  届时把 `ArtifactPaths.Repo` 改为相对于 `/srv/lober/repo/` 的路径、
  `RepoURL` 改为拼 `/repo/` 前缀、nginx 补第二个 location——这是一次独立的
  契约变更，不在本次冻结范围内预先实现。

**硬约束（三条，逐条冻结）：**

1. **制品目录下的路径与制品集里存的相对路径逐字符一致。**
   `ArtifactSet.Paths.Kernel`/`Initrd`/`Repo`/`Extra` 里存的字符串，就是相对于
   该制品集根目录（`/srv/lober/artifacts/<artifact_set_id>/`）的路径，M7
   落盘时与 M4 渲染 URL 时必须读同一份 `ArtifactPaths` 数据、不额外拼接或
   猜测——两者一旦对不齐，表现是「引导时 404」，且这类 bug 在测试环境往往
   因为路径碰巧对上而不暴露，只在生产环境某个特殊命名的制品集上才炸。

2. **hook 目录名就是正文的 sha256**（`model.HookDigest(body)` 的输出，
   64 位十六进制小写），机器侧靠它做校验（SPEC §6.3 的 `run_hook`：
   `curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh"` 后
   `sha256sum -c` 校验下载内容）。目录名与文件内容的对应关系是**内容寻址**，
   M7（导入/注册落盘）写入时必须先算好 digest 再决定目录名，不能反过来用
   一个自增 id 或用户填的 hook id 命名目录——那样机器侧的 `sha256sum -c`
   校验会失去意义（校验的是「文件没在传输中损坏」，不是「文件内容等于
   审批时看到的内容」；只有目录名=内容 hash 才能同时保证这两件事）。

3. **启动时校验 `ready` 状态制品集的文件真实存在，别等机器来了才发现。**
   Go 进程启动时（M9 装配阶段）遍历数据库中全部 `state = ready` 的
   `ArtifactSet`，对每一条的 `Paths.Kernel`/`Initrd`/`Repo`/`Extra` 逐一
   `os.Stat` 确认文件存在于 `/srv/lober/artifacts/<id>/` 下对应位置；
   缺失时**只记警告日志，不阻止服务启动**（一个坏掉的制品集不该拖累其它
   制品集与整个装机服务），但要让运维在服务日志里第一时间看到，而不是
   等某台机器引导到这个制品集时才在 `/v1/boot` 的 §4.6 三项校验里发现
   （那时机器已经在机房等着，反馈周期从「秒级」变成「人跑一趟机房」）。

### 4. hook 正文落盘的补充说明

`Hook.Body` 存在数据库里（`Hook` 结构体本身含 `Body` 字段，见
`internal/core/model/authoring.go`），**但机器侧不从数据库取 hook，而是从
`/artifacts/hooks/<digest>/script.sh` 取**（SPEC §6.3）。这意味着 M7/M6
在 hook 注册/审批流程中，除了把 `Body` 写数据库，还要**把同一份正文落盘**到
制品树的 `hooks/<sha256>/script.sh`——数据库记录与磁盘文件是同一份内容的
两个副本，靠 `BodySHA256` 字段与目录名保持一致性可核对。落盘时机是 hook
**创建或产出新版本时**（`PUT /hooks/{id}` 建新版本，`bodySha256` 变了就是新
目录），不是审批通过时——`preview` 端点（`POST /templates/{id}/preview`）需要
在 hook 还是 `draft` 状态时就能取到内容做假数据渲染。

---

## 目标机布局 `/var/lib/lober/`

SPEC §5.8 的 firstboot 脚手架，由渲染器在 `%post` 阶段生成并写入目标系统：

```
/var/lib/lober/
├── init.pending             空文件，标记「lober-init.service 待执行」；执行成功后删除
├── finalize.token           600 权限，finalize token 原文
├── params.env               600 权限，firstboot hook 需要的参数（KEY=VALUE 形式）
└── hooks/
    ├── 010-<digest>.sh      firstboot 阶段第一个 hook（三位数字前缀 = HookRef.Order）
    ├── 020-<digest>.sh      第二个
    └── manifest             文本清单：digest + required + timeout + order，逐行一条
```

**用户的 firstboot hook 脚本与参数必须在 `%post` 阶段就落进目标系统**，不能
等重启后再去 nginx 拉取——重启后业务网可能已生效，装机 VLAN 未必还可达
（SPEC §5.8：firstboot 对网络的唯一依赖应只剩「把信号发出去」）。这意味着
`%post` 阶段的渲染逻辑（M4）需要能访问到 firstboot 阶段全部 `HookRef` 的
正文（从服务端制品树的 `hooks/<digest>/script.sh` 读出），拼进应答文件里
一并下发，而不是让目标机器自己去 `curl` 取。

`manifest` 文件格式（供 `lober-init.sh` 解析，一行一条，字段用空格分隔）：

```
<digest> <required:true|false> <timeout_sec> <order>
```

**权限：**

| 路径 | 权限 | 理由 |
|---|---|---|
| `finalize.token` | `600` | 敏感凭据，仅 root 可读（`install -m 600 /dev/null` 后写入） |
| `params.env` | `600` | 可能含业务网 IP/hostname 等参数，与凭据同等对待 |
| `hooks/*.sh` | 可执行（`755` 或按 `run_hook` 实际调用方式） | 由 `%post` 阶段的 `install` 或等价命令赋权，不在本契约进一步细化——由 M4 渲染器决定具体命令 |

**清理时机（SPEC §9.5）：** firstboot 成功后，`shred -u` 删除
`params.env`/`finalize.token`，`rm -f init.pending`，且从
`/root/anaconda-ks.cfg`（RHEL 系）等安装期残留文件中清除含 `rootpw`/
`--iscrypted`/`inst.ks=` 的行——这些清理动作由渲染器生成的 firstboot 脚本
本身执行（M4 的产出内容），不是本文件系统契约需要另开端点或服务的事，
本文档只记录清理**目标**（哪些路径最终应该消失），确认三方（M4/M7/M9）
对「装完后 `/var/lib/lober/` 该剩下什么」有一致预期：清理完成后
`hooks/` 目录与 `manifest` 文件保留（供人工排查用过哪些 hook），
`init.pending`/`finalize.token`/`params.env` 三者应已不存在。

---

## 约定：权限（两个本机目录 + 一个数据库账号，逐条冻结）

| 对象 | 属主/权限模型 | 理由 |
|---|---|---|
| PostgreSQL 账号（连 `store.postgres` 指向的库） | 只授予这一个库（`dbname`）的权限，**不要复用其它业务已有的数据库账号** | 数据库含任务的 `token`/`finalizeToken` 等敏感字段；本机文件系统时代靠「目录仅 Go 进程可读写」隔离，迁到外部服务后隔离手段变成账号粒度——权限过宽的账号一旦泄露或被其它服务误用，暴露面从「这台主机」扩大到「整个 PostgreSQL 实例上的其它库」 |
| `/var/lib/lober/log/` | 仅 Go 进程所属用户可读写 | 日志正文可能含安装期敏感信息（如 kickstart 渲染失败时的参数回显），与数据库敏感字段同等对待 |
| `/srv/lober/`（`artifacts/`、`repo/`） | Go 进程所属用户可写（导入/注册时落盘），nginx worker 用户需可读 | nginx 按 SPEC §1.4 直接 `alias` 发盘（不经 Go 反代），worker 进程必须有读权限；反之 nginx worker **不需要写权限**——制品是只读服务的静态文件，让 nginx 有写权限没有任何功能收益，反而扩大攻击面（若 nginx 存在漏洞被利用，写权限意味着可以篡改制品内容影响所有后续装机） |

**部署期核对项（不做运行期检测）：** Go 进程与 nginx worker 若以不同系统
用户运行（推荐做法），`/srv/lober/` 需要让两者都能访问——典型做法是两个
用户加入同一个组，目录权限设 `750` 加组可读，或 `/srv/lober/` 属主设为
该组。具体的 useradd/chown 命令属于部署产物（M9 的 `deploy/`），不在本契约
文档展开。

---

## 三条硬约束小结（供自检引用）

1. 制品目录下的路径与制品集里存的相对路径逐字符一致（M5 渲染 URL、M7 落盘
   共同遵守）。
2. hook 目录名就是正文的 sha256（M7 落盘时计算，M4/机器侧校验时复算，两处
   算法必须都是裸 `sha256sum` 等价，不加盐不加前缀）。
3. 启动时校验 `ready` 状态制品集的文件真实存在（M9 装配阶段实现，缺失只
   警告不阻止启动）。
