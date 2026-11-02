# Lober

裸机自动装机系统。操作员登记主机、导入安装介质、组合模板，然后建任务；目标机开机后经 PXE 自己装好系统。

不用人工插盘、不用一台台点安装界面。

## 它解决什么

一台空机器上架，正常流程是：插 U 盘、开机、点十几屏安装界面、配网络、配账号。几十台就是几十遍。

Lober 把这些变成：

1. 控制台里登记主机的序列号
2. 选好 ISO 和模板，批量建任务
3. 机器上电，剩下的事它自己做

装完机器重启进系统，自动跑完首启脚本，然后向 Lober 报告「我好了」。

## 支持的系统

| 族 | 系 | 应答格式 |
| --- | --- | --- |
| Linux | RHEL、Rocky Linux、AlmaLinux、openEuler、OpenCloudOS | kickstart |
| Linux | Debian | preseed |
| Linux | Ubuntu | autoinstall |
| Linux | SLES 15、openSUSE Leap 15 | AutoYaST |
| Linux | SLES 16、openSUSE Leap 16 | Agama |
| Windows | Windows Server、Windows 桌面版 | unattend |
| ESXi | ESXi | ESXi ks |

## 四个核心概念

**ISO** — 一份厂商安装介质。导入时解包，抽出的内核、initrd 和仓库树放进制品目录。

**Hook** — 一段可复用的安装逻辑，比如「怎么分区」「怎么配网络」「装哪些包」。一个 hook 有多份实现，每种安装格式一份。

**模板** — 由若干 hook 拼成的应答文件骨架。模板勾选它支持哪些系，装配表决定每个位置放哪个 hook、参数从哪来。

**任务** — 一台主机、一个 ISO、一个模板版本，外加建任务时冻结的快照。之后 ISO 或模板怎么改，都不影响已建的任务。

## 工作方式

```
                  ┌──────────────┐
   操作员 ────────▶│   控制台      │  HTTP + JSON
                  │  /console/v1 │
                  └──────┬───────┘
                         │
                  ┌──────▼───────┐
                  │   PostgreSQL │
                  └──────┬───────┘
                         │
   目标机 ───────────────▶│  /v1/boot    取引导菜单
   （PXE 启动）           │  /v1/answer  取应答文件
                         │  /v1/signal  报进度
                  ┌──────▼───────┐
                  │  制品目录     │  内核、initrd、仓库树
                  │  静态服务     │
                  └──────────────┘
```

目标机开机 → DHCP 下发 iPXE → iPXE 带上序列号来问 → Lober 按序列号找到任务、渲染引导菜单 → 安装器按菜单下载内核与应答文件 → 装好重启 → 首启脚本跑完 → 任务完成。

**ISO 不需要挂载**，内核、initrd、软件包全部走 HTTP。

## 硬件采集

装机前可以先让机器跑一个最小镜像，把磁盘、CPU、内存情况报回来，存成 `inv_*` 主机属性。

这样同一个模板可以对多台盘序不同的机器装机——分区 hook 引用的盘名参数，值来自采集结果。

采集**不采网络地址**：机器在装机网络里的地址对业务没有意义，采了只会让人误以为那是主机地址。

## 构建

需要 Go 1.27.3。

```sh
make check        # 格式、静态检查、测试、竞态、Linux 交叉编译、Wire 一致性
make test         # 只跑测试
```

前端在 `web/`，是一个 pnpm workspace：

```sh
cd web
pnpm install --frozen-lockfile
pnpm dev:ele      # 开发服务器，代理到 127.0.0.1:58080
pnpm build:ele    # 产出到 internal/interfaces/console/ui/dist/
```

前端产物不入库，只保留两个占位文件让 `go build` 能通过。发布前必须先构建前端。

## 运行

```sh
go build -o dist/lober ./cmd/lober
./dist/lober -config /etc/lober/lober.yaml
```

配置示例见 `cmd/lober/lober.example.yaml`。数据库用 PostgreSQL。

## 代码结构

```
internal/
├── domain/          领域层：纯判定，不碰文件、网络、时钟
│   ├── catalog/     族、系、格式、时机这些固定词表
│   ├── model/       领域对象：主机、ISO、模板、hook、任务
│   ├── render/      渲染：把冻结快照变成应答文件
│   └── ...
├── application/     应用层：编排，不直接碰数据库和 HTTP
├── infrastructure/  基础设施：PostgreSQL、制品文件、日志收集
└── interfaces/      接口层：只做协议转换
    ├── console/     控制台 HTTP
    └── machine/     机器面 HTTP
```

分层由 `internal/archtest` 强制检查——越界直接测试失败，不靠自觉。

## 文档

文档只保留在本机 `docs/` 下，不随仓库分发：

- `docs/contracts/` — 各模块的行为契约
- `docs/superpowers/specs/` — 设计文档
- `docs/superpowers/plans/` — 实施计划
- `AGENTS.md` — 仓库规范

## 许可

Apache License 2.0，见 [LICENSE](LICENSE)。

前端基于 [vue-vben-admin](https://github.com/vbenjs/vue-vben-admin)，按 MIT 许可使用，其许可声明保留在 `web/` 下。
