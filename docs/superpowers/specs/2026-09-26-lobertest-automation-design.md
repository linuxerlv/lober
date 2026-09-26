# lobertest：三层自动化测试工程

| | |
|---|---|
| 日期 | 2026-09-26 |
| 状态 | 设计定稿，待实现 |
| 范围 | 一个 uv Python 工程，三层（接口/装机/前端）全部跑在 Linux VM 上，取代 14 个 e2e bash 脚本与旧 vmlab 测试床 |

## 1. 问题

当前测试资产分三摊，各有一处致命缺陷。

**14 个 `scripts/e2e*.sh`**：一律用 `curl` 模拟机器打点，用 psql 直接把
`artifact_sets` 推到 `ready`。七个家族脚本各写一遍同样的流程，改一个端点
要改七处。`e2e-suse.sh` 只有 5 个阶段（其余 9 个），`e2e-nginx.sh` 跑不了。
补完 SUSE 6-9 阶段的那 372 行压在 `feat/n9-suse-e2e-finish` 上，且写在
sqlite3 时期、有 8 处直查 `DB_PATH`，在 dev 的 psql 基线上跑不起来。

**旧 `tools/vmlab`**（`feat/vmlab-real-install` 分支，1539 行）：已经是 uv +
paramiko + pytest，12 个用例覆盖 RHEL 与 Debian，含真实装机与 SSH 登入验证。
它真的把系统装上去过（`7febf7a`）。问题是它与生产码修复混在同一批提交里
（34 个提交中 15 个同时改 `internal/` 与 `tools/`），且只覆盖两个家族、
完全没有前端层。

**前端**：`internal/console/ui/dist/` 里只有占位 index.html。真实前端 68 个
源文件、8 个页面从未在本机构建过，零 UI 自动化。两个 vitest 用例只测
form-schema 与 use-task-stream 两个单元。

更根本的一点：所有快速反馈都发生在 Windows 上。`5089a86`「文件名撞上 Go 的
GOOS 隐式约束，lober 根本无法交叉编译到 Linux」证明了这类缺陷的存在——
Windows 上跑得好不代表 Linux 上能跑，而 lober 的部署目标只有 Linux。

## 2. 边界

**保留**：`feat/vmlab-real-install` 的 45 个生产码文件与 47 个 Go 单元测试
（6530 行）。那 26 个 `fix(...)!` 是真实装机一个个撞出来的阻断缺陷修复
（kickstart 不发装机信号、iPXE 菜单缺 `#!ipxe` 首行与 boot 命令、BIOS+GPT
缺 biosboot 分区、nginx 把制品下载套进引导端点限流桶等），每条提交信息都
引了当时的串口输出作证据。丢掉它们等于六个家族的装机链路重新变断。

**删除重写**：`tools/vmlab` 与 `scripts/vmlab`（37 文件 3271 行）。旧 `api.py`
里沉淀的知识（Envelope 解包、导入轮询超时、hook 审批闸门、模板状态流转）
按新分层重写进去，不重踩已知的坑。

**取代**：14 个 `scripts/e2e*.sh`。新工程覆盖它们管的全部链路后删除。
`docs/real-machine/` 下 6 份配套 e2e 报告随之失去对应脚本，保留作历史记录。

**不做**：Go 单元测试不动——按四层分支模型它们属于特性分支，与本工程不同层，
不存在重叠。`tools/gosurgeon` 保留（开发期工具，与测试无关）。

## 3. 一切都跑在 Linux 上

三层的共同前提是 VM-S 处于真实部署状态：交叉编译 linux/amd64、推二进制、
systemd 起 lober、nginx 做唯一对外入口、postgres 在 guest 本地。

**本机不起任何 lober 进程。** 进程内测试快，但它证明的是「这段 Go 代码在
Windows 上的行为」，而没有一台生产机器跑 Windows。交叉编译因此成为第一道
闸门：编不出 linux/amd64 二进制，一条用例都不跑——这正是 `5089a86` 那类
缺陷暴露的位置。

三层的差别不是「本机 vs VM」，而是**要不要起目标机**：

| 层 | 额外需要 | 量级 | 测什么 |
|---|---|---|---|
| L1 接口层 | 只要 VM-S | 分钟级 | 六家族参数化走全链路；准入控制负面用例；RBAC 七个权限码；Envelope 错误码映射 |
| L2 装机层 | 目标机（BIOS/UEFI） | 20 分钟-1 小时/家族 | 真实装完到 `done` + SSH 登进新机器 |
| L3 前端层 | guest 内 node 构建 | 分钟级 | playwright 打 nginx，8 个页面 |

L1 保住快反馈靠的是「不装机」，不是「不进 VM」。

### VM-S 生命周期

二进制每轮重推，库按 schema 隔离（与 Go 侧 `pgtest` 夹具同一套路）。
`provision` 本身幂等，打 `.provisioned` 标记。

每轮重建 schema 的一个必要后果：`idx_tasks_sn_active`（同一 SN 不能有两个
活跃任务）那类唯一索引不会被上一轮的残留数据占住。

用户账号是例外——它们在配置文件 `auth.users` 里，不在库里（`config.go:347`
的 `Roles []string`）。换 schema 不丢账号，但改账号要重推配置并重启；这与
「二进制每轮重推」是同一个动作，不额外加开销。

## 4. 工程结构

```
tests/lobertest/
  pyproject.toml          # playwright 进 optional-dependencies
  .python-version         # 3.12，与既有两个 uv 工程一致
  uv.lock
  README.md               # 怎么跑、三层各要什么前提
  conftest.py             # target 解析、层间闸门、session 级部署
  src/lobertest/
    config.py             # 环境事实的唯一来源
    envelope.py           # Envelope 解包与错误码表
    ssh.py                # paramiko 封装
    api.py                # 控制台 API 客户端
    qemu.py               # VM 生命周期
    deploy.py             # 交叉编译 + 前端构建 + schema + 推送
  tests/
    l1_api/
    l2_install/
    l3_ui/
```

依赖方向单向：`tests/` → `src/lobertest/` → 外部库。`config.py` 与
`envelope.py` 是叶子，不 import 工程内任何东西。`api.py` 只依赖这两个叶子
加 `ssh.py`。

**`api.py` 不 import `qemu.py`**，这是 L1 在不起目标机时可跑的编译期保证。
`qemu.py` 只被 L2/L3 的 fixture 引用。

目录名的 `l1/l2/l3` 前缀让 pytest 默认字典序就是「先后端再前端」。

依赖分组：

```toml
dependencies = ["requests", "paramiko", "pytest"]   # L1 + L2 够用
[project.optional-dependencies]
ui = ["playwright"]                                  # 只有 L3 要
```

`uv sync` 装前两层，`uv sync --extra ui` 才拉浏览器。浏览器驱动用 playwright
的 Python 绑定而非 node 版：宿主没有 node，playwright 自带浏览器下载。

## 5. 模块接口

### config.py

```python
@dataclass(frozen=True)
class Settings:
    ssh_host, ssh_port, ssh_user, ssh_key_path    # VM-S 的 SSH 入口
    console_base_url                              # 经 nginx 的人面入口
    pxe_subnet, tftp_root                         # 目标机网段
    qemu_bin, ovmf_code, iso_dir                  # QEMU 侧事实
    schema                                        # 本轮的库 schema 名

def load() -> Settings                            # 环境变量覆盖默认值
```

只有一个 target（VM-S），不需要「本机一套值、VM 一套值」的双份常量。环境
变量覆盖只用于 VM-S 地址与端口转发这类真实会变的东西。

### envelope.py

```python
def unwrap(resp, what: str) -> Any    # 解 data 字段；失败时把原始响应体带进异常
class APIError(Exception): code, http_status, message
```

单独成模块的理由：L1 的负面用例要断言错误码，这批断言不该依赖 API 客户端
的具体方法。

### ssh.py

```python
class SSH:
    def __enter__/__exit__
    def run(cmd, timeout) -> Result       # Result: ok / rc / stdout / stderr
    def sudo(cmd, timeout) -> Result
    def read_file(path, sudo=True) -> str
    def push(local, remote)               # SFTP

class Result:
    def check(what)                       # 失败时把 rc 与 stderr 一起抛
```

`Result.check` 的形态沿用旧 harness——那个设计是对的。

### api.py

```python
class Console:
    def token(role: str) -> str           # 按角色取 token，见 §6
    def call(method, path, **kw) -> Any
    # host / artifact-set / template / hook / task / answer / menu 各域方法
```

21 个方法留在一个类里。按资源域拆成 6 个类会让调用方到处传实例，收益不抵
成本。

### qemu.py

```python
def start_server()
def reset_overlay(entry_id, size)
def start_target(entry_id, firmware, mem)   # uefi 时挂独立 pflash 可写副本
def stop_target(entry_id)                   # 内存约束：下一家族才能起
def serial_log_path(entry_id) -> Path
```

pflash vars 每条目一份可写副本，不能共用——共用会让引导项互相污染，且
QEMU 可能因文件被占用直接拒绝启动。

### deploy.py

```python
def cross_compile() -> Path        # GOOS=linux GOARCH=amd64，失败即 raise
def build_web() -> None            # guest 内 pnpm build，产物回传进 dist
def create_schema(name) -> None
def deploy(binary, schema) -> None # 推二进制 + 重推配置 + systemd 重启 + 等健康检查
def teardown_schema(name) -> None
```

`build_web` 独立于 `deploy`：L1/L2 不需要真实前端，每轮多花几分钟构建 dist
是白费。只有 L3 的 fixture 调它。

## 6. RBAC 与测试账号

`internal/console/rbac.go:19-37` 是权威定义：五个角色（`viewer` /
`operator` / `hook-author` / `hook-approver` / `admin`）、七个权限码
（`task:create` / `task:retry` / `task:cancel` / `hook:write` /
`hook:approve` / `provider:write` / `artifact:import`）。

`hook-author` 与 `operator` 是互不重叠的两个角色，不能互相代替（`rbac.go:45-47`：
hook 以 root 身份在分区之前运行，能力等同擦掉整机所有盘）。这个边界是 L1
负面用例要锁住的东西。

用户来自配置文件而非数据库，所以建账号不是调 API：`deploy` 阶段用
`scripts/mkuser` 生成 bcrypt 哈希，把五条用户记录写进 `lober.yaml` 再推上去。
账号名 `lobertest-<role>`，密码用固定测试值。

`Console.token(role)` 按角色名取 token。L1 用它测七个权限码的边界，L3 用它
测按钮可见性。

## 7. 层间闸门

L1 有失败时 L2/L3 打 **skip 而不是 fail**——接口层断了，装机与 UI 必然跟着
断，再跑一小时没有信息量。实现在 `pytest_collection_modifyitems` 里按上一层
结果打标记（pytest 的 `--ignore` 做不到这个）。

L2/L3 默认不跑，要 `-m slow` 显式开。`uv run pytest` 在没有目标机的情况下
是干净通过的 L1 全绿。

schema 共用关系：L1 与 L3 可以共用同一个 schema 顺序跑——L3 正需要看 L1 建
出来的数据在 UI 上呈现得对不对。L2 每个家族一个独立 schema。

## 8. fixture 分层

`conftest.py`（顶层）：`settings`、`deployed`（session 级，交叉编译 + 推送 +
建 schema）、`console`。

`l2_install/conftest.py`：目标机相关。
`l3_ui/conftest.py`：`build_web` 与 playwright 的 browser/page。

L1 的用例只看得见 `console` 与 `deployed`，编译期就不可能误用 QEMU 的东西。

## 9. 验收标准

- `uv run pytest` 只跑 L1，全绿，且每条结论都来自 Linux 上运行的 lober
- 交叉编译失败时立即停止，不跑任何用例
- 六个家族在 L1 走同一批参数化用例，家族差异只体现在参数表
- L1 覆盖 14 个 e2e bash 脚本管的全部链路（逐脚本核对后才删它们）
- L2 每个家族两条独立证据：任务到 `done`，且 SSH 登进新装的机器
  （`/etc/os-release` 正确、`systemctl is-system-running` 为 running 或
  degraded，degraded 要写清哪个 unit 失败）
- L3 八个页面可达，且 RBAC 按钮可见性随角色变化
- 工程内无 `.pyc` 入库（旧 `tools/vmlab` 误提交了 9 个 `__pycache__` 文件）

## 10. 明确不做

- 不改 Go 单元测试（属于特性分支，与本工程不同层）
- 不在宿主装 node/pnpm——前端构建在 Debian guest 里做，产物回传
- 不为 L1 保留「本机进程内」模式，哪怕它更快
- 不补 `feat/n9-suse-e2e-finish` 那 372 行的 psql 迁移——SUSE 的 6-9 阶段由
  L1 的参数化用例覆盖，那个脚本随其余 13 个一起删
- 不碰 `master`（停在 216 个提交之前的 M1 地基）

## 11. 与既有文档的关系

取代 `docs/superpowers/specs/2026-09-25-vmlab-real-install-design.md` 的第 6 节
（队列运行器）与第 4 节（串行队列）。该文档第 5 节列的后端改动已在
`feat/vmlab-real-install` 上实现，本设计保留那部分成果。

`docs/real-machine/17-iso读取实现.md` 记录的「e2e 全绿但没有一次真实安装」
教训是本设计存在的直接原因。
