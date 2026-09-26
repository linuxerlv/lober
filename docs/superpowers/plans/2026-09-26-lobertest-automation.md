# lobertest 三层自动化测试工程 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 一个 uv Python 工程 `tests/lobertest`，三层（接口 / 装机 / 前端）全部跑在 Linux VM 的真实部署上，取代 14 个 e2e bash 脚本与旧 `tools/vmlab` 测试床。

**架构：** 单工程三层分包。`src/lobertest/` 六个模块（config / envelope / ssh / api / qemu / deploy）按单向依赖组织，`tests/l1_api`、`tests/l2_install`、`tests/l3_ui` 三层靠目录名字典序保证「先后端再前端」。每层开跑前都要 VM-S 处于真实部署状态：交叉编译 linux/amd64、推二进制、建 schema、systemd 重启。三层差别在于要不要起目标机。

**技术栈：** uv + Python 3.12、requests、paramiko、pytest、playwright（Python 绑定，仅 L3）。QEMU 11.0.1（WHPX）、Debian 13 guest、PostgreSQL、Go 1.27.1 交叉编译 linux/amd64。

**设计依据：** `docs/superpowers/specs/2026-09-26-lobertest-automation-design.md`

**分支：** 全部落 `test` 分支（工作树 `E:/src/lober-wt/test`）。

---

## 前置依赖：必须先并入生产码修复

**本计划的任务 4 起全部依赖 `feat/vmlab-real-install` 的 45 个生产码文件已并入 dev 并同步到 test。**

那 26 个 `fix(...)!` 修的是六个家族装机链路上的阻断缺陷（kickstart 不发装机信号、
iPXE 菜单缺 `#!ipxe` 首行与 boot 命令、BIOS+GPT 缺 biosboot 分区、nginx 把制品
下载套进引导端点限流桶）。缺了它们，L1 的应答文件断言与 L2 的装机全部必然失败，
而失败原因指向的是已经修好的旧缺陷——这种红灯没有信息量。

**顺序是硬要求：** 先并入生产码修复，再删 `tools/vmlab`（任务 2）。反过来会先删掉
唯一能验证那些修复的东西。

---

## 文件结构

### 新建工程（`tests/lobertest/`）

| 文件 | 职责 | 为什么这样切 |
|---|---|---|
| `pyproject.toml` | 工程定义；playwright 进 `optional-dependencies` | L1/L2 不需要浏览器。装不装 playwright 是运行者的选择，不该是必需依赖 |
| `src/lobertest/config.py` | 环境事实的唯一来源 | 叶子模块。VM-S 地址、QEMU 路径、schema 名散落在各处会让「换一台 VM 跑」变成全工程搜索替换 |
| `src/lobertest/envelope.py` | Envelope 解包与错误码表 | 叶子。L1 的负面用例要断言错误码，这批断言不该依赖 API 客户端的具体方法 |
| `src/lobertest/ssh.py` | paramiko 封装 | 三层都要远程执行命令。`Result.check()` 把 rc 与 stderr 一起抛是旧 harness 唯一值得原样保留的设计 |
| `src/lobertest/api.py` | 控制台 API 客户端 | **不 import `qemu`**——这是 L1 在不起目标机时可跑的编译期保证 |
| `src/lobertest/qemu.py` | VM 生命周期 | 只被 L2/L3 的 fixture 引用。与 `api.py` 分开，L1 才不可能误用 |
| `src/lobertest/deploy.py` | 交叉编译 + 前端构建 + schema + 推送 | 每轮的部署闸门。`build_web` 独立于 `deploy`，L1/L2 才不必白花几分钟构建 dist |
| `conftest.py` | target 解析、层间闸门、session 级部署 | 顶层只放三层共用的 fixture |
| `tests/l1_api/` | 接口层用例 | 目录名前缀让 pytest 默认字典序 = 先后端再前端 |
| `tests/l2_install/` | 装机层用例，含自己的 `conftest.py`（目标机 fixture） | |
| `tests/l3_ui/` | 前端层用例，含自己的 `conftest.py`（`build_web` + playwright） | |

**为什么放 `tests/` 而不是 `tools/`：** `tools/` 在本仓库是开发期工具的位置
（`tools/gosurgeon` 是 Go 源码行级编辑器，与测试无关）。

### 删除

| 路径 | 时机 |
|---|---|
| `tools/vmlab/`（23 文件） | 任务 2，且必须在生产码修复并入之后 |
| `scripts/vmlab/`（5 文件） | 任务 2 |
| `scripts/e2e*.sh` 与 `scripts/e2e/`（14 文件） | 任务 11，且必须逐脚本核对 L1 覆盖之后 |

`docs/real-machine/` 下 6 份配套 e2e 报告保留作历史记录——它们记录的是当时真实
发生过的验证，删脚本不等于那些验证没发生过。

---

## 任务 1：uv 工程骨架与两个叶子模块

**目标：** 建起可 `uv sync` 的工程，`config` 与 `envelope` 两个叶子模块可独立测试，不依赖 VM。

**文件：**
- 创建：`tests/lobertest/pyproject.toml`、`.python-version`、`README.md`、`.gitignore`
- 创建：`tests/lobertest/src/lobertest/__init__.py`、`config.py`、`envelope.py`
- 测试：`tests/lobertest/tests/test_config.py`、`tests/test_envelope.py`

**接口契约：**

```python
# config.py
@dataclass(frozen=True)
class Settings:
    ssh_host: str; ssh_port: int; ssh_user: str; ssh_key_path: Path
    console_base_url: str
    pxe_subnet: str; tftp_root: str
    qemu_bin: Path; ovmf_code: Path; iso_dir: Path
    schema: str

def load(env: Mapping[str, str] | None = None) -> Settings

# envelope.py
class APIError(Exception):
    code: str; http_status: int; message: str

def unwrap(resp: requests.Response, what: str) -> Any
```

- `load()` 的每个字段有默认值，环境变量 `LOBERTEST_<字段大写>` 覆盖之。`env=None` 时读 `os.environ`（注入 `env` 是为了可测）
- `Settings` 是 `frozen=True`：配置在 session 开始时定死，用例不得改
- `unwrap` 成功时返回 Envelope 的 `data` 字段；非 2xx 或 `data` 缺失时抛 `APIError`，**消息里必须带原始响应体**（截断到前 500 字符）
- `what` 是调用方给的语义标签（如 `"create host"`），进异常消息帮助定位
- `envelope.py` 与 `config.py` 都**不 import 工程内任何模块**

**要覆盖的输入类别：**

`config`：1) 全默认；2) 环境变量覆盖单个字段；3) `ssh_port` 给非数字字符串 → 明确报错而不是 `ValueError` 裸抛；4) `frozen` 生效（改字段抛 `FrozenInstanceError`）

`envelope`：5) 2xx + 合法 Envelope → 返回 `data`；6) 2xx 但无 `data` 字段 → `APIError`；7) 4xx + 错误 Envelope → `APIError` 且 `code` 取自响应体；8) 响应体不是 JSON → `APIError` 且消息含原始文本片段

- [ ] **红：** 为上述八类写失败测试。运行 `uv run pytest tests/test_config.py tests/test_envelope.py -v`，预期 FAIL（模块未定义，import 错误即是红灯）。

- [ ] **绿：** 实现两个模块直到八类全绿。不要实现 `api.py`（任务 3）。

- [ ] **重构 + commit：** 确认 `uv sync` 干净、`uv run pytest -q` 全绿后 commit（`feat(lobertest): uv 工程骨架与 config/envelope 两个叶子模块`）。

**风险：** 旧 `tools/vmlab` 误把 9 个 `__pycache__/*.pyc` 提交进库。本任务的 `.gitignore` 必须覆盖 `__pycache__/`、`.pytest_cache/`、`.venv/`，且 commit 前用 `git status --short` 确认没有 `.pyc` 入库。

---

## 任务 2：删除旧测试床

**目标：** 移除 `tools/vmlab` 与 `scripts/vmlab`，让新工程成为唯一的测试床。

**文件：**
- 删除：`tools/vmlab/`（23 个已提交文件）
- 删除：`scripts/vmlab/`（5 个文件：`lib-vm.sh`、`run-install.sh`、`start-target.sh`、`start-vms.sh`、`serverside/dnsmasq.conf.in`）

**前置检查（不通过则停止并报告）：** `feat/vmlab-real-install` 的 45 个生产码文件
已在 test 分支上。用 `git log --oneline | grep -c 'fix(render)!'` 之类的方式确认，
或直接检查一个标志性修复是否在位——`internal/render/kickstart.go` 里应有 `biosboot`
分区指令（那是 `83584d7` 加的）。

**要从旧代码搬走的知识（搬进任务 3-5，不是搬代码）：**

| 旧位置 | 沉淀下来的事实 | 搬到 |
|---|---|---|
| `api.py` 的 `_unwrap` | Envelope 解包失败时必须带原始响应体，否则错误无法定位 | 任务 1 已做 |
| `api.py:106` `wait_artifact_ready` | 导入是异步的，超时要给足 10 分钟；`failed` 时必须打印 `FailReason`，否则表现成无信息的超时 | 任务 3 |
| `api.py:168` `activate_hook` | hook 有审批闸门，draft 状态不能被任务引用 | 任务 3 |
| `api.py:214` `set_template_state` | 模板状态流转要带 version（乐观锁） | 任务 3 |
| `api.py:237` `cancel_active_tasks` | 同一 SN 不能有两个活跃任务，建任务前要先清 | 任务 3 |
| `server.py` 的 `Result.check` | 远程命令失败时 rc 与 stderr 要一起抛 | 任务 4 |
| `qemu.py` | WHPX 下 `-cpu max`/`-cpu host` 必炸；必须配 `-accel whpx,kernel-irqchip=off` | 任务 7 |

- [ ] **红：** 本任务无红灯——删除不产生失败测试。改为**取证**：删除前把 `tools/vmlab/src/vmlab/api.py` 与 `server.py` 完整读一遍，确认上表七项知识都已记录在本计划或已实现的模块里。漏了任何一项，先补进对应任务的契约再删。

- [ ] **绿：** 执行删除。运行 `uv run pytest -q`（在 `tests/lobertest/` 下）确认任务 1 的用例不受影响；运行 `go build ./...` 确认删除未牵连 Go 侧（预期退出 0，因为这两个目录不被任何 Go 代码引用）。

- [ ] **重构 + commit：** commit（`chore(lobertest)!: 删除旧 vmlab 测试床，知识已沉淀进新工程`）。commit 信息里列出上表七项，让下一个人知道旧代码里的教训去哪了。

**风险：** `tools/vmlab/.venv`、`.pytest_cache`、`.tmp/` 是被 gitignore 的运行期产物，`git rm` 不会碰它们。物理目录要单独清理，但**先确认 `.tmp/` 里没有还想留的串口日志**——那是上一轮真实装机的证据。

---

## 任务 3：控制台 API 客户端

**目标：** 一个 `Console` 类覆盖 L1 需要的全部端点，所有错误经 `envelope.APIError` 统一抛出。

**文件：**
- 创建：`tests/lobertest/src/lobertest/api.py`
- 测试：`tests/lobertest/tests/test_api.py`（用 `requests-mock` 或本地 stub server，**不连真实 VM**）

**接口契约：**

```python
class Console:
    def __init__(self, base_url: str, *, timeout: int = 30) -> None
    def token(self, role: str) -> str                    # 角色名见任务 6
    def call(self, method: str, path: str, *, role: str = "admin", **kw) -> Any

    # host
    def create_host(self, sn, mac, arch, firmware, disks: list[dict] | None = None) -> str
    def ensure_host(self, sn, mac, arch, firmware) -> str

    # artifact-set
    def import_artifact(self, source_path: str, name: str) -> str
    def wait_artifact_ready(self, artifact_id: str, timeout: int = 600) -> dict

    # template
    def create_template(self, name, family, body, **fields) -> str
    def set_template_state(self, template_id: str, state: str, version: int) -> dict
    def activate_template(self, template: dict) -> dict

    # hook
    def create_hook(self, name, phase, script) -> str
    def approve_hook(self, hook_id: str, *, role: str = "hook-approver") -> dict

    # task
    def create_task(self, host_sn, template_id, form_data: dict) -> str
    def wait_task_state(self, task_id: str, want: str, timeout: int) -> dict
    def cancel_active_tasks(self, sn: str) -> list[str]
    def events(self, task_id: str) -> list[dict]

    # 机器面（经 gateway，非 console）
    def boot_menu(self, sn, mac, arch="i386", plat="pcbios") -> str
    def answer_file(self, task_id: str, token: str, filename: str) -> str
```

**实现后订正（任务 3 已完成，以下为对照真实 handler 得到的事实，后续任务直接用，不要再从本计划的方法名反推路径）：**

- **`GET /tasks/{id}` 不存在。** `internal/console/task.go:28-32` 只注册了五条：`POST /tasks`、`GET /tasks`、`GET /tasks/:id/events`、`POST /tasks/:id/retry`、`POST /tasks/:id/cancel`。所以 `wait_task_state` 是取列表按 id 过滤，与 `cancel_active_tasks` 同一做法。**任务 8 写轮询用例时别指望单点查询端点。**
- **`Console.__init__` 实际签名是 `(base_url, gateway_base_url=None, *, timeout=30)`。** 机器面与人面是两个 base_url，而 `config.Settings` 里没有机器面字段（任务 1 已冻结），所以在构造时单独传。`gateway_base_url` 为 `None` 时调机器面方法明确抛 `ValueError`，不静默用错 URL。
- **hook 的字段名是 `stage` 不是 `phase`**（`internal/console/hook.go:57`，对应 `model.HookStage`，取值 `pre`/`post-nochroot`/…）。Python 侧形参名保留 `phase` 可读，请求体必须发 `stage`。
- **模板没有独立的 family 字段**，家族由 `artifactSetId` 决定（`template.go:55-65` 的字段表）。`create_template` 的 `family` 形参转成请求体的 `artifactSetId`。
- **`token(role)` 的账号名约定 `lobertest-<role>` 内联在 `api.py` 里**，没有 import `deploy.py`——任务 6 顺序在后，反向依赖会让 L1 无法独立跑。这是两处之间唯一的耦合点，任务 6 若改前缀要同步改，`api.py` 里有注释标注。

- `token(role)` 按角色缓存，同一 role 不重复登录
- `call` 的 `role` 参数决定用哪个身份的 token；默认 `admin`
- `wait_artifact_ready` 轮询到 `ready`；命中 `failed` 时**立即抛 `APIError` 并把 `FailReason` 放进消息**（不要等超时）。超时默认 600 秒——真 ISO 抽取实测 16.8 秒，但 Ubuntu 2.6G 镜像更久
- `set_template_state` 必带 `version`（乐观锁）；`activate_template` 是便利包装，从 template dict 里取 version
- `approve_hook` 默认用 `hook-approver` 身份——`hook:approve` 权限码只有这个角色和 admin 有
- `create_task` 前不自动清活跃任务；清理是 `cancel_active_tasks` 的事，由 fixture 显式调用（隐式清理会掩盖「同一 SN 两个活跃任务」这条准入规则的测试）
- `boot_menu` / `answer_file` 走机器面端口，`Console` 内部区分两个 base_url
- `boot_menu` 的 `arch` 默认 `i386`：真实 iPXE 在 32 位 BIOS 下上报的是 `i386` 而非 `x86_64`（`d014d50` 修的正是 arch 闸门把它判成不兼容）。默认值取真实上报值，不取「看起来对」的值

**要覆盖的输入类别（全部用 mock，不连 VM）：**
1. `token` 同一 role 调两次 → 只发一次登录请求
2. `wait_artifact_ready` 遇 `failed` → 抛 `APIError` 且消息含 `FailReason` 内容
3. `wait_artifact_ready` 一直 `importing` 到超时 → 抛超时错误且消息含已等待秒数
4. 任一方法收到 4xx Envelope → 抛 `APIError` 且 `code` 正确
5. `set_template_state` 缺 version → 明确报错而非发出非法请求

- [ ] **红：** 为上述五类写失败测试。运行 `uv run pytest tests/test_api.py -v`，预期 FAIL。

- [ ] **绿：** 实现 `Console` 直到五类全绿。不实现 `ssh`/`qemu`/`deploy`（任务 4、5、7）。

- [ ] **重构 + commit：** 确认 `api.py` 的 import 列表里**没有 `qemu`**（这是设计约束，`grep -n 'import' src/lobertest/api.py` 人工核对）。commit（`feat(lobertest): 控制台 API 客户端`）。

**风险：** 21 个方法留在一个类里会让文件偏大（预计 300 行上下）。这是刻意的——按资源域拆成 6 个类会让调用方到处传实例。若超过 400 行，按「机器面 vs 人面」切成两个类，而不是按资源域切。

---

## 任务 4：SSH 封装

**目标：** 一个 `SSH` 类让三层都能在 VM-S 上执行命令、读文件、推文件，失败时给出可定位的错误。

**文件：**
- 创建：`tests/lobertest/src/lobertest/ssh.py`
- 测试：`tests/lobertest/tests/test_ssh.py`（`Result` 的逻辑可纯单测；连接相关的用 `-m needs_vm` 标记）

**接口契约：**

```python
@dataclass
class Result:
    rc: int; stdout: str; stderr: str
    @property
    def ok(self) -> bool
    def check(self, what: str) -> "Result"      # rc != 0 时抛，消息含 what/rc/stderr

class SSH:
    def __init__(self, settings: Settings) -> None
    def __enter__(self) -> "SSH"
    def __exit__(self, *exc) -> None
    def run(self, cmd: str, timeout: int = 120) -> Result
    def sudo(self, cmd: str, timeout: int = 120) -> Result
    def read_file(self, path: str, *, sudo: bool = True) -> str
    def push(self, local: Path, remote: str) -> None      # SFTP
```

- `check()` 返回 `self` 以便链式调用；失败消息必须含 `what`、`rc`、`stderr` 三者
- `run` 超时时抛异常而非返回 `Result`——超时不是「命令失败」，是「不知道命令是否执行了」，两者必须可区分
- `push` 走 SFTP；目标目录不存在时**不自动创建**（静默建目录会掩盖路径写错）
- `AutoAddPolicy` 用于 host key——这是一次性测试 VM，不是生产连接

**要覆盖的输入类别：**
1. `Result(rc=0).ok` 为真、`check()` 返回自身
2. `Result(rc=1, stderr="boom").check("provision")` → 异常消息同时含 `provision`、`1`、`boom`
3. `run` 执行 `exit 3` → `rc == 3` 且不抛（命令失败不是异常）
4. `run` 超时 → 抛超时异常（标记 `needs_vm`）
5. `push` 到不存在的目录 → 抛异常且消息含远端路径（标记 `needs_vm`）
6. **大输出不死锁**：远端产出 500KB stdout → 不卡死且 `len(stdout)` 等于完整长度（标记 `needs_vm`）。这是上面那两个死锁变体的回归护栏，小输出测不出来

- [ ] **红：** 为五类写失败测试，前三类不需要 VM。运行 `uv run pytest tests/test_ssh.py -v -m 'not needs_vm'`，预期 FAIL。

- [ ] **绿：** 实现到三类纯逻辑用例全绿。`needs_vm` 那两类在任务 5 有了可连的 VM-S 后再验。

- [ ] **重构 + commit：** commit（`feat(lobertest): paramiko SSH 封装`）。

**风险：** paramiko 的 `exec_command` 在命令输出量大时会死锁。有**两个**变体，只防住第一个不够：

1. 先 `recv_exit_status()` 再读 channel——远端写满接收窗口后阻塞，我们在等它退出，互等。这是教科书版本
2. 顺序读：`stdout.read()` 读到 EOF 再 `stderr.read()`——远端交替写两路时，stderr 缓冲区填满会让它停止写 stdout，于是 stdout 的 EOF 永远不来，卡在第一个 `read()` 上

**旧 `server.py` 只防住了第一个**（`git show 7893a30^:tools/vmlab/src/vmlab/server.py` 可取到原文，它是顺序读）。正确做法是 channel 级 `select` + 非阻塞 `recv_ready()`/`recv_stderr_ready()`，同一循环里交替抽取两路，都排空且 `exit_status_ready()` 后才退出。

**超时要用截止时刻（deadline）而不是每轮重置的固定值**：把固定 timeout 传给每次 `select`，远端只要间歇性吐一点数据就会不断把时限扣满，永远不触发超时。

这两条都要有测试守着，不能只靠实现时小心——它们在小输出下绝不出现，只在真实装机日志几十 KB 时炸。

---

## 任务 5：部署闸门

**目标：** 一条命令把当前源码变成 VM-S 上跑着的 lober：交叉编译、建 schema、推二进制与配置、重启并确认就绪。

**文件：**
- 创建：`tests/lobertest/src/lobertest/deploy.py`
- 测试：`tests/lobertest/tests/test_deploy.py`（编译相关可本机验；推送相关标 `needs_vm`）

**接口契约：**

```python
def cross_compile(repo_root: Path, out: Path) -> Path
    # GOOS=linux GOARCH=amd64 go build ./cmd/lober；失败抛异常并带 go 的完整 stderr

def create_schema(ssh: SSH, name: str) -> None
def teardown_schema(ssh: SSH, name: str) -> None

def render_config(*, schema: str, users: list[TestUser]) -> str
    # 产出 lober.yaml 正文；users 见任务 6

def deploy(ssh: SSH, binary: Path, config: str) -> None
    # 推二进制 + 推配置 + systemctl restart + 等就绪

def wait_ready(ssh: SSH, console: Console, timeout: int = 60) -> None
def build_web(ssh: SSH, repo_root: Path) -> Path      # 任务 10 才用
```

- `cross_compile` 是**第一道闸门**：它失败时必须让整个 session 立刻停止，不跑任何用例。异常消息带 go 的完整 stderr——`5089a86` 那类 GOOS 隐式约束错误就是靠这段输出定位的
- `create_schema` 必须自己建：`config.go:274` 明确 lober 不会自动创建 `search_path` 指向的 schema
- `render_config` 的 `search_path` 填本轮 schema 名；`gateway.listen` 与 `console.listen` 绑 `127.0.0.1`，nginx 做唯一对外入口
- **就绪判据（没有健康检查端点，用三条组合）：**
  1. `lober -config /etc/lober/lober.yaml -check` 退出 0（systemd unit 的 `ExecStartPre` 就是这条）
  2. `systemctl is-active lober` 为 `active`
  3. 未认证 `POST /console/v1/auth/login` 拿到 4xx 而非连接错误（服务在听且认证生效）

  三条全过才算就绪。只看 `systemctl is-active` 不够——`Type=simple` 下进程起来了不代表端口在听

**要覆盖的输入类别：**
1. `cross_compile` 在干净仓库上 → 产出文件存在且 `file` 报 ELF 64-bit（本机可验）
2. `cross_compile` 在故意引入 Linux 不兼容代码的临时副本上 → 抛异常且消息含 go 的错误文本（本机可验）
3. `render_config` 产出的 YAML 能被 `lober -check` 接受（需 VM，标 `needs_vm`）
4. `wait_ready` 在服务未起时超时 → 抛异常且消息说清三条判据里哪条没过
5. `create_schema` 同名调两次 → 第二次不报错（幂等）

- [ ] **红：** 为五类写失败测试。运行 `uv run pytest tests/test_deploy.py -v -m 'not needs_vm'`，预期 FAIL。

- [ ] **绿：** 实现到类别 1、2、4 全绿（不需要 VM 的那些）。`needs_vm` 的在任务 8 有 VM-S 后验。

- [ ] **重构 + commit：** 类别 2 用的临时副本要清理干净（铁律：不留临时文件）。commit（`feat(lobertest): 部署闸门 —— 交叉编译、schema 隔离、推送与就绪判据`）。

**风险：** 交叉编译的 Go 版本必须与仓库一致。`[[go-toolchain-mismatch-use-d-golang]]` 记录过两份 Go 版本不同导致编译全炸，现已统一到 1.27.1，直接用 `go`。若 `cross_compile` 报的错像是语言特性不支持而非代码问题，先核 `go version`。

---

## 任务 6：五个角色的测试账号

**目标：** L1 能以五种身份调 API，为 RBAC 边界测试提供输入。

**文件：**
- 修改：`tests/lobertest/src/lobertest/deploy.py` —— 加 `TestUser` 与账号生成
- 测试：`tests/lobertest/tests/test_users.py`

**权威定义（不要另立一套）：** `internal/console/rbac.go:19-37` 定义五个角色与七个权限码。

| 角色 | 权限码 |
|---|---|
| `viewer` | 无（空数组，不是 null） |
| `operator` | `task:create` `task:retry` `task:cancel` |
| `hook-author` | `hook:write` |
| `hook-approver` | `hook:approve` |
| `admin` | 以上全部 + `provider:write` `artifact:import` |

**接口契约：**

```python
# config.py（叶子）新增——见下方「凭据必须收敛到一处」
TEST_USER_PREFIX: str           # "lobertest-"
TEST_PASSWORD: str              # 明文，长度要过 lober 的强度校验
TEST_ROLES: tuple[str, ...]     # 五个角色名，取自 rbac.go

# deploy.py
@dataclass(frozen=True)
class TestUser:
    name: str; bcrypt_hash: str; roles: list[str]

def build_test_users(repo_root: Path) -> list[TestUser]
    # 五个，name 为 f"{TEST_USER_PREFIX}{role}"

def bcrypt_hash(repo_root: Path, password: str) -> str
    # 经 scripts/mkuser 生成；不在 Python 侧实现 bcrypt
```

**为什么是函数而不是模块级 `TEST_USERS` 静态列表**（任务 6 实现时的决定，已验证）：算哈希要跑
`go run ./scripts/mkuser`，需要 `repo_root`。静态列表意味着在 import 时机就得算好五个哈希，
那要在 `deploy.py` 里新增一套「模块自己发现 repo_root」的机制——而同模块的 `cross_compile`
与 `build_web` 都是「调用方传入 repo_root」。为一处引入第二种约定不值得。任务 8 的
`deployed` fixture 本来就要拿 `repo_root` 调 `cross_compile`，传同一个变量即可。

**凭据必须收敛到一处（任务 3、5 实现后暴露的问题，任务 6 要一并解决）：**

现状是裂开的——`api.py:27,32` 自己存了 `_TEST_USER_PREFIX` 与 `_TEST_PASSWORD`
（任务 3 为避免反向依赖尚不存在的 `deploy.py` 而内联），`deploy.py` 的 `TestUser`
只有 `bcrypt_hash` 没有明文密码。**两边必须指同一个密码**：`deploy` 用它算哈希写进
`lober.yaml`，`api` 用它登录。一旦漂移，症状是全部 L1 用例 401，而两处各自看起来
都对。

按设计 §4 的依赖方向，`api.py` 与 `deploy.py` 是同级，不能互相 import；`config.py`
是叶子、两者都可以 import。所以三个常量落在 `config.py`，任务 6 同时删掉 `api.py`
里的两个内联副本改为 import。

`TestUser` 保留 `bcrypt_hash` 而非计划原文的 `password`：写进配置文件的是哈希，
明文只在调 `mkuser` 时用一次，不该存进每条用户记录。

- 用户来自**配置文件** `auth.users`（`config.go:347` 的 `Roles []string`），不是数据库。所以建账号是 `render_config` 写 YAML + 重启，不是调 API
- `bcrypt_hash` 调 `go run ./scripts/mkuser -user <name> -stdin`。不在 Python 侧装 bcrypt 库算哈希：它必须由 lober 自己的工具生成，否则「测试账号能登录」证明不了「运维按文档建的账号能登录」。（用 bcrypt 库**校验**哈希是另一回事，那是测试类别 6 的手段，不是生产路径）
- `subprocess` 调 `mkuser` 时密码只走 `stdin=PIPE`，**不进 argv**（argv 会出现在 `ps` 输出里）。这条要有测试守着——拦截调用参数断言密码不在 argv 列表中
- `subprocess` 必须显式 `encoding="utf-8"`：本机默认区域设置是 GBK，而 `mkuser` 的中文 stderr 是 UTF-8，不指定会在读 stderr 时 `UnicodeDecodeError`，失败路径的错误消息因此不可读
- **密码强度的唯一闸门是 `mkuser` 自己**：`scripts/mkuser/main.go:46` 的 `minPasswordLen = 12`。`internal/config/config.go` 那一段只校验 `auth.users[]` 的 name / bcrypt_hash / roles 非空，没有密码复杂度规则（原计划说「config.go:620 附近有校验」，指的其实是这个非空校验，不是强度校验）
- `hook-author` 与 `operator` 是互不重叠的两个角色（`rbac.go:45-47`：hook 以 root 身份在分区之前运行，能力等同擦掉整机所有盘）。**不要给任何测试账号同时挂这两个角色**，那会让 L1 测不出这条边界

**要覆盖的输入类别：**
1. `bcrypt_hash` 产出以 `$2` 开头、60 字符的字符串
2. `TEST_USERS` 五个角色齐全且无重复 name
3. 没有任何一个 user 同时含 `hook-author` 与 `operator`
4. `render_config` 产出的 YAML 里五个用户都在，哈希非空
5. **凭据单一来源**：`api.py` 里不再有硬编码的密码或账号前缀字面量（`grep` 断言），且它用的前缀与 `TEST_USERS` 的 name 前缀是同一个值
6. **哈希与明文对得上**：拿 `TEST_PASSWORD` 算出的哈希，用 bcrypt 校验函数验证能匹配该明文（`mkuser` 出的哈希不是随便一串 `$2` 开头的东西）。若本机没有 bcrypt 库可用，退化为「同一密码调两次 `mkuser` 得到两个不同哈希」（bcrypt 加盐，相同输入不同输出）——这至少能证明它真的在做 bcrypt 而不是返回常量

- [ ] **红：** 为四类写失败测试。运行 `uv run pytest tests/test_users.py -v`，预期 FAIL。

- [ ] **绿：** 实现到四类全绿。类别 1 会真的跑 `go run ./scripts/mkuser`，确认它能在本机跑通（不需要 VM）。

- [ ] **重构 + commit：** commit（`feat(lobertest): 五个角色的测试账号，哈希经 scripts/mkuser 生成`）。

**风险：** `mkuser` 的 `-stdin` 模式从标准输入读密码。用 `subprocess` 喂密码时注意不要把密码留在命令行参数里（那会进 `ps` 输出），要走 `stdin=PIPE`。

---

## 任务 7：QEMU 生命周期

**目标：** 起停 VM-S 与目标机，串口日志落盘，为 L2 提供可重复的机器环境。

**文件：**
- 创建：`tests/lobertest/src/lobertest/qemu.py`
- 测试：`tests/lobertest/tests/test_qemu.py`（参数拼装可纯单测；起 VM 的标 `needs_vm`）

**已知事实（不要重新试错，来自 `[[qemu-local-vm]]` 与旧 harness）：**
- WHPX 下 `-cpu max` 与 `-cpu host` 必然 `WHPX: Unexpected VP exit code 4`，串口零输出。**可用值是 `-cpu qemu64`**：名义上只到 x86-64-v1，但实测能跑 RHEL 9 系（glibc 的 v2 检查没触发）
- 必须配 `-accel whpx,kernel-irqchip=off`，否则刷 `Ignoring request for interrupt vector 0`
- QEMU 在 `D:\msys\ucrt64\bin`，用绝对路径调用
- 受 15.5G 内存限制，同时只能跑一台目标机

**六条从旧 harness 取证得来的硬约束（任务 2 删除前逐条核实过源码，都带真实报错）：**

1. **`-serial file:` 在这个 QEMU 构建上写出 0 字节日志。** 必须用 `-nographic` 让串口走进程 stdout，再由 `subprocess` 重定向到文件。踩这个坑的后果是拿到空证据却以为装机没输出
2. **`disk_serial` 必须拆成 `-drive if=none,id=hd0` + 独立 `-device virtio-blk-pci,drive=hd0,serial=...`。** serial 是设备属性不是驱动属性，写在 `-drive` 上 QEMU 直接拒绝启动（`Block format 'qcow2' does not support the option 'serial'`）。这是 Debian 家族服务端选盘依赖 `/dev/disk/by-id/virtio-<serial>` 的前提
3. **必须给 `-device virtio-rng-pci`。** 否则 guest 内 sshd-keygen 从 `/dev/random` 取熵时因熵池补充极慢而挂起（实测卡 7 分钟无进展、无明确报错）。真机不需要，纯虚拟化环境特有
4. **残留的目标机进程会占着 VM-S 的 socket listen 端口**，新目标机拿不到网络，症状是 iPXE 报 `No configuration methods succeeded`。更坏的情形是它偷走新任务的 pending 状态（`FindPendingTaskByIdentity` 匹配 pending+booting）。**按 `-name` 精确清理，不能按内存大小猜**——踩过一次：装到一半的目标机占 2.3GB，被「小于 1500MB」的过滤漏掉
5. **MSYS 的 argv 翻译只作用于「整个参数就是一个 POSIX 绝对路径」的裸参数**，不作用于 `file=X` 这种复合参数内嵌的路径，与路径深浅无关。这是 `WinError 2` 的真实成因。统一用 `D:/...` 正斜杠形式
6. **串口日志按 mtime 归档而非覆盖。** 装机失败常要跨轮次对比（这一轮卡在哪、上一轮到哪），旧 harness 有过一次因覆盖丢失 anaconda 错误输出的教训

**接口契约：**

```python
def start_server() -> Popen                      # VM-S；幂等（已在跑则返回现有句柄）
def reset_overlay(entry_id: str, size: str) -> Path
def start_target(entry_id: str, firmware: str, mem: str, disk_serial: str = "") -> Popen
def stop_target(entry_id: str) -> None           # 确认进程真的退出
def kill_stale_targets() -> list[int]            # 按 -name 清理全部残留，返回被杀的 PID
def serial_log_path(entry_id: str) -> Path
def archived_log_path(entry_id: str, stamp: str) -> Path
def winpath(p: Path | str) -> str                # POSIX → Windows 路径，强制绝对化
```

- `start_target` 的 `firmware` 取 `bios` / `uefi`；`uefi` 时挂 `-drive if=pflash` 加 **每条目一份独立的可写 vars 副本**——共用会让引导项互相污染，且 QEMU 可能因文件被占用直接拒绝启动
- **vars 副本必须从 `edk2-i386-vars.fd` 拷，不能自己造一个空文件或全零文件。** 本机没有 `edk2-x86_64-vars.fd`，这不是包装缺陷：QEMU 自己的 `share/qemu/firmware/60-edk2-x86_64.json` 声明 `executable` 是 `edk2-x86_64-code.fd`、`nvram-template` 是 `edk2-i386-vars.fd`——varstore 格式与架构无关，只发一份（aarch64 同理用 `edk2-arm-vars.fd`）。模板 540672 字节，偏移 0x28 处有 `_FVH` 固件卷签名；全零文件缺这个头，OVMF 会判 NVRAM 损坏或走重建路径。也不要「按 code.fd 大小创建」——code 与 vars 是两个独立 flash 设备，各按自己的文件大小映射，尺寸不必相同（3653632 vs 540672）。已存在时不覆盖（幂等，别冲掉 guest 已写入的 UEFI 变量）。
  **方法论：** 缺配套文件时先翻工具链自己的元数据（这里是 firmware descriptor），不要凭推理造替代品
- `start_target` 每次启动前先调 `kill_stale_targets`（约束 4）；`disk_serial` 非空时按约束 2 拆参数
- `stop_target` 必须确认进程退出，不只是发信号：内存约束下一条目才能起
- `kill_stale_targets` 按 `-name` 匹配命令行，与 `stop_target` 分开：后者管单个已知 entry，前者管「上一轮崩溃留下的、entry_id 已不可知的」进程
- `serial_log_path` 在文件已存在时先归档到 `archived_log_path`（stamp 取旧文件的 mtime），不覆盖
- `winpath` 强制绝对化（相对路径原样透给 QEMU 会以 QEMU 的 cwd 解析，那不是调用方期望的）

**要覆盖的输入类别（前六类纯逻辑，不起 VM）：**
1. `start_target(firmware="uefi")` 的参数列表含 `pflash` 且 vars 路径含 `entry_id`
2. `start_target(firmware="bios")` 的参数列表**不含** `pflash`
3. 参数列表含 `-accel whpx,kernel-irqchip=off`，且 `-cpu` **等于 `qemu64`**（不是「不等于 max/host」——护栏要锁住已验证可用的那个值，排除坏值会让下一个人以为随便换一个都行）
4. 参数列表含 `-nographic` 与 `-device virtio-rng-pci`，且**不含** `-serial file:`（约束 1、3）
5. `disk_serial` 非空时参数含 `if=none` 与独立的 `-device virtio-blk-pci,...,serial=`；为空时用 `if=virtio` 简写（约束 2）
6. `winpath("relative/x")` 产出绝对 Windows 路径
7. `serial_log_path` 在旧日志存在时把它归档且不删除（约束 6）
8. `stop_target` 在进程已死时不抛（幂等，标 `needs_vm`）

- [ ] **红：** 为八类写失败测试。运行 `uv run pytest tests/test_qemu.py -v -m 'not needs_vm'`，预期 FAIL。

- [ ] **绿：** 实现到类别 1-7 全绿。**类别 3、4、5、7 是回归护栏**：它们锁住的是上面六条约束里已经踩过的坑，不是新功能。护栏失效的代价不是「测试变红」，是任务 9 花 20 分钟到 1 小时真实装机后拿到一份空证据或假失败。

- [ ] **重构 + commit：** commit（`feat(lobertest): QEMU 生命周期与串口日志落盘`）。

**风险：** 若 `-cpu` 的候选值在 WHPX 下全部失败，退路是 `-accel tcg`（慢一个数量级，装机可能从 20 分钟变成数小时）。真走到这一步**先停下来报告**，不要默默切 TCG。

---

## 任务 8：L1 接口层与层间闸门

**目标：** 六个家族参数化走全链路，跑在 VM-S 的真实 Linux 部署上；L1 失败时后两层自动 skip。

**文件：**
- 创建：`tests/lobertest/conftest.py` —— `settings` / `deployed` / `console` / 层间闸门
- 创建：`tests/lobertest/tests/l1_api/test_lifecycle.py` —— 六家族参数化主链路
- 创建：`tests/lobertest/tests/l1_api/test_admission.py` —— 准入控制负面用例
- 创建：`tests/lobertest/tests/l1_api/test_rbac.py` —— 七个权限码边界

**fixture 契约：**

```python
@pytest.fixture(scope="session")
def settings() -> Settings
@pytest.fixture(scope="session")
def deployed(settings) -> DeployInfo    # 交叉编译 + 建 schema + 推送 + 等就绪
@pytest.fixture(scope="session")
def console(deployed) -> Console
```

- `console` fixture 要同时给出人面与机器面两个 base_url（见任务 3 的实现后订正）：机器面用例（`boot_menu`/`answer_file`）缺了它会抛 `ValueError`
- 轮询任务状态走 `GET /tasks` 列表按 id 过滤——没有单点查询端点

- `deployed` 是 session 级：每轮 pytest 一次部署，不是每个用例一次
- `deployed` 失败（尤其 `cross_compile` 失败）时整个 session 立刻停止
- L1 的用例**只能看见 `console` 与 `deployed`**。不得引入 `qemu` 的 fixture——那是 L2/L3 的事

**层间闸门（用 `pytest_runtest_setup`，不是 `pytest_collection_modifyitems`）：**

订正：原文写 `pytest_collection_modifyitems`，那是错的 —— 它只在收集阶段跑一次，那时任何用例都还没执行，「L1 是否失败过」必然是空的。任务 8 用假 L1/L2 用例验证时真的观察到假 L2 没被跳过、直接执行并报错。正确的挂点是 `pytest_runtest_setup`（每个 item setup 前都触发）。这个坑值得写进 conftest 注释。
- L1 有任何失败 → L2/L3 的用例打 **skip 而非 fail**。理由：接口层断了，装机与 UI 必然跟着断，再跑一小时没有信息量
- L2/L3 默认不收集，要 `-m slow` 显式开

**marker 的三层划分（订正：原文把前两层混为一谈）：**

原文说「`uv run pytest` 默认只跑 L1 且全绿」，这句话自相矛盾——L1 按本计划的设计就跑在 VM-S 上，没有 VM-S 时它不可能绿。实际有三层，各自的默认行为不同：

| 层 | marker | 默认收集 | 需要什么 | 量级 |
|---|---|---|---|---|
| 纯逻辑 | 无 | 是 | 什么都不用 | 毫秒级 |
| L1 接口 | `needs_vm` | 否 | VM-S 部署好 | 分钟级 |
| L2 装机 / L3 前端 | `slow` | 否 | VM-S + 目标机 / node 构建 | 小时级 |

`addopts` 因此带 `-m "not needs_vm and not slow"`：**裸跑 `uv run pytest` 只跑纯逻辑层，在任何机器上都该全绿**（config/envelope 的单测、`api.py` 的 mock 用例、`Result` 的数据逻辑）。

- 跑 L1：`uv run pytest -m needs_vm`
- 跑全部：`uv run pytest -m "needs_vm or slow"`

`needs_vm` 这个 marker 由任务 4 引入（给 `ssh.py` 里那些要连真实主机的用例），任务 8 的 L1 用例沿用同一个 marker——它们的前提是一样的：有一台部署好的 VM-S。

**六家族参数化的主链路（每个家族一条参数）：** 建 host（带磁盘清单）→ 导制品集并轮询 ready → 建模板并流转到 active → 建 hook 并审批到 active → 建任务 → 取 iPXE 菜单 → 取应答文件。家族差异（ISO 路径、家族名、应答文件名、菜单形态）全进参数表。

现在是七个 bash 脚本各写一遍同样的流程，改一个端点要改七处。

**要覆盖的输入类别：**

主链路（`test_lifecycle.py`）：1) 六个家族各走完整链路；2) 应答文件形态按家族断言（kickstart / preseed / autoinstall / AutoYaST / unattend.xml / boot.cfg）；3) iPXE 菜单含 `#!ipxe` 首行与 boot 命令（`99b6ca7`、`4e32ed9` 的回归护栏）

准入（`test_admission.py`）：4) 同一 SN 已有活跃任务时再建 → 拒绝；5) 固件不兼容 → 拒绝；6) attempt 配额耗尽 → 拒绝；7) 引用 draft 状态的 hook → 拒绝

**实现后订正（任务 8 实测，四条）：**

1. **没有独立的 arch 兼容检查。** 原文类别 5 写「arch 不兼容」，代码里只有 `model.FirmwareCompatible`（`inventory.go:122`）。而那张表里 rhel/debian/ubuntu/suse/windows 五家族都同时兼容 bios 与 uefi，**唯一受限的是 esxi（仅 uefi）** —— 所以这条规则要触发必须有 ESXi 制品。没有 ESXi ISO 时只能 skip，不要为了让它绿而改断言。
2. **draft hook 的拒绝点在建模板，不在建任务。** `template.go:39` 的注释写明了这个分工：`handleCreateTemplate` 硬编码新模板为 draft，hook 引用校验在那一步（返回 404）。原文写「建任务时拒绝」是错的。
3. **类别 6（attempt 配额）在 L1 没有公开入口可触发。** 判定发生在 `StartBoot` 里，唯一路径是反复调 `boot_menu` 让同一任务多次引导 —— 那是 L2 的重试语义。L1 只能 skip。
4. **`sourcePath` 必须是 guest 路径。** 导入 job 跑在 guest 内的 lober 进程里，Windows 宿主路径对它不可见。ISO 要先推上去，且中转目录不能用 `/tmp`（tmpfs 只有 736M，推 1.7G 的 ISO 会写满），用 `/home/debian`。
5. **`import_artifact` 对 Debian 需要 `netboot_path` 参数**绕开 `ErrInitrdCDOnly`。任务 3 时这是纯理论知识，第一次真跑才用得上。

RBAC（`test_rbac.py`）：8) `viewer` 调 `/auth/codes` 拿到 `[]` 而非 `null`（`rbac.go:77-80` 明确要求）；9) `operator` 建任务成功、审批 hook 被拒；10) `hook-author` 写 hook 成功、审批被拒；11) `hook-approver` 审批成功、建任务被拒；12) `admin` 七个码全有

- [ ] **红：** 为十二类写失败测试。运行 `uv run pytest tests/l1_api/ -v`，预期 FAIL（fixture 未定义）。

- [ ] **绿：** 实现 conftest 与三个测试文件直到十二类全绿。**这是本计划第一次真的连 VM-S**，几乎必然暴露前五个任务未预见的缺口。按 systematic-debugging 定位根因，不要猜着改。

  每修一处先判断属于哪类：(a) 测试工程问题 → 改 `tests/lobertest/`；(b) 后端缺陷 → 先补 Go 单测再改 `internal/`，且单测要能在 VM 之外复现；(c) 环境限制 → 记进 README，不改代码。三类不要混着改。

- [ ] **重构 + 验证 + commit：** 确认 `uv run pytest` 默认只跑 L1 且全绿；确认 `-m slow` 时 L2/L3 被收集（哪怕还没实现）。commit（`feat(lobertest): L1 接口层 —— 六家族参数化、准入负面用例、RBAC 七个权限码`）。

**风险：** 六个家族里 Windows 与 ESXi 需要外部制品（wimboot / BCD / boot.sdi / boot.wim；ESXi ISO 需 Broadcom 账号）。拿不到的家族在参数表里标 `pytest.mark.skip` 并写明缺什么、从哪能拿到——**不要降低断言让它假绿**。

---

## 任务 9：L2 装机层

**目标：** 每个家族真的把系统装上去，两条独立证据齐全。

**文件：**
- 创建：`tests/lobertest/tests/l2_install/conftest.py` —— 目标机 fixture
- 创建：`tests/lobertest/tests/l2_install/test_install.py` —— 家族参数化真实装机

**fixture 契约：**

```python
@pytest.fixture
def target(request, settings) -> TargetVM   # 按家族参数起目标机，用完必停
```

- 每个家族一个独立 schema（避免 `idx_tasks_sn_active` 被上一家族的活跃任务占住）
- `target` 的 teardown 必须确认目标机进程退出——内存约束下一家族才能起
- 单家族墙钟预算 45 分钟，超时按失败处理

**验收要两条独立证据（缺一条都不算通过）：**

1. **状态机终态与阶段序列**：任务 state 为 `done`；events 按序含 `booting → installing → rebooting → initializing → done`
2. **SSH 登进新装的机器**：用注入的公钥登录，`/etc/os-release` 与期望家族一致，`systemctl is-system-running` 为 `running` 或 `degraded`（degraded 可接受但要在失败摘要里写清哪个 unit 失败）

证据二不能省：那串信号是我们自己渲染的脚本发出来的，全到齐只证明脚本跑完了，不证明系统是好的。它同时验证 `rootpw --lock` + 公钥注入是唯一登录通路。

**失败时必须落的证据（缺一样都算测试工程的 bug）：** 目标机串口 log、qemu stderr、VM-S 的 `journalctl -u lober`、dnsmasq log、nginx access log、`events()` 的 JSON、目标机 overlay（保留不删）。

**串口 log 这一样有两个前提，都在任务 7 的约束里，这里复述因为证据是本任务的验收项：** 它必须是 `-nographic` 经 subprocess 重定向来的（`-serial file:` 在这个 QEMU 构建上写出 0 字节），且上一轮的日志必须已被归档而非覆盖（跨轮次对比是定位「这一轮比上一轮早死在哪」的唯一手段）。拿到 0 字节串口 log 时先查这两条，不要当成「装机过程真的没有输出」。

**要覆盖的输入类别：**
1. RHEL BIOS 装完（**最小成功线**）
2. RHEL UEFI 装完，且下发的 kickstart 含 `part /boot/efi`、`bootloader` 不带 `--location=mbr`
3. Debian / Ubuntu / openSUSE 各装完（全 UEFI，BIOS 路径已由类别 1 覆盖）
4. 失败路径：指向不存在的 ISO → 七样证据齐全且状态判定为失败

- [ ] **红：** 先跑类别 4（**故意失败**的用例）。失败路径比成功路径更容易写漏，先确认证据收集是全的。再跑类别 1，预期失败——这是第一次真实装机。

- [ ] **绿：** 修到类别 1 两条证据齐全，再逐个家族推进。**三个家族分别走红绿循环，不要合并**——家族差异正是要暴露的东西，合并会让归因变难。

- [ ] **重构 + commit：** 每个家族独立 commit。README 里写明哪些部分不能代表真实硬件（QEMU BIOS ROM 本身就是 iPXE 构建这类局限）。

**风险：** ESXi 仅 UEFI（`esxi.go:218` 的 `ErrESXiRequiresUEFI` 会拒绝 BIOS），且 8.0 硬性要求 8G 内存——8G 目标机 + 2.5G VM-S + 宿主会贴到 15.5G 天花板并开始换页，优先找 7.0。拿不到 ISO 就判 **blocked 而非 failed**，在 README 写清缺什么、从哪能拿到、拿到后跑哪条命令。**不要为了让测试全绿而降低验收标准。**

---

## 任务 10：L3 前端层

**目标：** guest 内构建真实前端，playwright 打 nginx 验证八个页面与 RBAC 按钮可见性。

**文件：**
- 创建：`tests/lobertest/tests/l3_ui/conftest.py` —— `build_web` + playwright 的 browser/page
- 创建：`tests/lobertest/tests/l3_ui/test_pages.py` —— 八个页面可达性
- 创建：`tests/lobertest/tests/l3_ui/test_rbac_ui.py` —— 按钮可见性随角色变化
- 修改：`tests/lobertest/pyproject.toml` —— playwright 进 `optional-dependencies`

**当前前提（本任务要打破的）：** `internal/console/ui/dist/` 只有占位 `index.html`
与一个 `_placeholder-keeps-embed-underscore-mode.js`。真实前端 68 个源文件、8 个
页面从未在本机构建过。`embed.go:28` 的 `//go:embed all:dist` 要求 dist 目录存在。

**构建链路（node 在 Debian guest 里，宿主不装）：**
1. 把 `web/` 推上 VM-S
2. guest 内 `pnpm install && pnpm build`（首次要 apt 装 node + pnpm，幂等）
3. 产物回传到宿主的 `internal/console/ui/dist/`
4. 重新 `cross_compile`（这次二进制里带真实 dist）
5. `deploy` 推上去

`build_web` 只被 L3 的 fixture 调用——L1/L2 不需要真实前端，每轮多花几分钟构建是白费。

**要覆盖的输入类别：**
1. 八个页面各自可达且渲染出内容（login、hosts、artifact-sets、hooks、tasks 列表 / 详情 / 新建、reports）
2. 未登录访问受保护页面 → 跳登录页
3. `viewer` 登录后看不到「新建任务」按钮；`operator` 看得到
4. `hook-author` 看得到 hook 的编辑入口、看不到审批按钮；`hook-approver` 相反
5. 任务详情页的阶段流转能呈现 L1 建出来的真实任务数据

**L1 与 L3 共用 schema：** L3 正需要看 L1 建出来的数据在 UI 上呈现得对不对（类别 5）。L2 才需要每家族独立 schema。

- [ ] **红：** 为五类写失败测试。运行 `uv run pytest tests/l3_ui/ -v -m slow`，预期 FAIL。

- [ ] **绿：** 实现到五类全绿。**先确认 `build_web` 真能产出非占位的 dist**（检查产物里有 hash 命名的 js/css，不只是占位文件），再写页面断言——dist 是占位时所有页面断言都会以误导性的方式失败。

- [ ] **重构 + commit：** 确认 `uv sync`（不带 `--extra ui`）后 L1/L2 仍可跑。commit（`feat(lobertest): L3 前端层 —— guest 内构建 dist，playwright 验八个页面与 RBAC`）。

**风险：** `[[lober-web-build-toolchain]]` 记录 `vite build` 会清掉入库的占位 dist。产物回传后若构建失败，仓库里的占位文件可能已被删除而 `go:embed` 编译不过。回传前先备份占位文件，或让 `build_web` 失败时恢复它们。

---

## 任务 11：删除 e2e bash 脚本

**目标：** 14 个 bash 脚本的覆盖全部由 L1 承接后删除它们，测试集只剩一套。

**文件：**
- 删除：`scripts/e2e.sh`、`e2e-debian.sh`、`e2e-ubuntu.sh`、`e2e-suse.sh`、`e2e-windows.sh`、`e2e-esxi.sh`、`e2e-nginx.sh`
- 删除：`scripts/e2e/`（`lib-db.sh` 与五个 `mkfixture-*.sh`、`mkfixture.sh`）
- 修改：`tests/lobertest/README.md` —— 记录覆盖映射

**前置：逐脚本核对（不通过则不删该脚本）**

`scripts/e2e.sh` 的九个阶段与 L1 的对应关系必须逐条列出：

| e2e.sh 阶段 | L1 承接处 |
|---|---|
| 1 造假制品树、建二进制、造 admin、写配置 | `deployed` fixture + 任务 6 的五个账号 |
| 2 起服务轮询就绪 | `deploy.wait_ready`（三条判据） |
| 3 登录拿 token | `Console.token(role)` |
| 4 录入主机、登记制品集、建模板 | `test_lifecycle.py` 主链路 |
| 5 preview 端点 | `test_lifecycle.py` |
| 6 准入控制四条负面用例 | `test_admission.py` |
| 7 取引导脚本与菜单、抠 token | `test_lifecycle.py` |
| 8 取应答文件、逐个信号打点 | `test_lifecycle.py` + L2 真实装机 |
| 9 终态 done、装完再引导不循环重装 | L2 `test_install.py` |

`e2e-nginx.sh` 是特例：它测 nginx 配置本身（限流桶、location 分离），而 L1 经 nginx
访问但不专测 nginx 配置。**删它之前确认 `e8940b0`（制品下载套了引导端点限流桶）
那个缺陷有对应的回归护栏**——若没有，在 L1 补一条「制品下载不受引导端点限流影响」
的用例，再删。

`feat/n9-suse-e2e-finish` 那 372 行不迁移：SUSE 的 6-9 阶段由 L1 的参数化用例覆盖。

- [ ] **红：** 本任务无红灯。改为**取证**：把上表逐行走一遍，对每个 e2e 脚本确认它测的东西在 L1/L2 有对应用例。列出所有没有对应的项，为它们补用例后再继续。

- [ ] **绿：** 执行删除。运行 `uv run pytest -q` 与 `uv run pytest -m slow -q` 确认测试集不受影响；运行 `go build ./... && go vet ./...` 确认退出 0。

- [ ] **重构 + commit：** commit（`chore(lobertest)!: 删除 14 个 e2e bash 脚本，覆盖已由 L1/L2 承接`）。commit 信息带上覆盖映射表。

**风险：** `docs/real-machine/` 下 6 份配套 e2e 报告失去对应脚本。**保留它们**——记录的是当时真实发生过的验证，删脚本不等于那些验证没发生过。在 README 里说明这些报告对应的脚本已被 L1 取代。

---

## 整体验收

全部任务完成后逐项确认：

- [ ] `uv sync` 在 `tests/lobertest/` 下干净完成，不带 `--extra ui`
- [ ] `uv run pytest -q` 裸跑只收集纯逻辑层，在没有 VM 的机器上也全绿（见任务 8 的 marker 三层划分）
- [ ] `uv run pytest -m needs_vm` 跑 L1 全绿，且每条结论都来自 Linux 上运行的 lober
- [ ] 故意破坏交叉编译（临时改一处 Linux 不兼容代码）后 `uv run pytest` 立即停止且不跑任何用例，错误消息含 go 的完整 stderr。**验证后恢复该改动**
- [ ] `uv run pytest -m slow -q` 收集到 L2/L3 的用例
- [ ] 人为让一条 L1 用例失败后，L2/L3 的用例显示为 **skipped 而非 failed**。验证后恢复
- [ ] `git status --short` 在 `tests/lobertest/` 下无 `.pyc`、`.venv`、`.pytest_cache` 入库
- [ ] `grep -n 'import' tests/lobertest/src/lobertest/api.py` 结果里没有 `qemu`
- [ ] `go build ./...` 与 `go vet ./...` 退出 0
- [ ] `LOBER_TEST_PG_DSN=... go test ./...` 全绿（29 个包；缺该变量时 store 测试会刻意整批失败，见 `[[lober-test-pg-dsn]]`）
- [ ] `tools/vmlab/` 与 `scripts/vmlab/` 已不在 git 索引中
- [ ] `scripts/e2e*.sh` 与 `scripts/e2e/` 已不在 git 索引中，且 README 里有覆盖映射表
- [ ] L2 至少 RHEL BIOS（最小成功线）达成两条证据：任务 `done` + SSH 登入成功
- [ ] L3 八个页面可达，且至少两个角色的按钮可见性差异被断言
- [ ] 拿不到制品的家族（Windows / ESXi）在参数表里是显式 skip 并写明缺什么，不是假绿
- [ ] README 写明三层各要什么前提、怎么跑、哪些部分不能代表真实硬件

---

## 备注：三类归因法

任务 8 起的每次绿灯修复，先判断属于哪类再动手，三类不要混着改：

| 类别 | 判据 | 处置 |
|---|---|---|
| (a) 测试工程问题 | 后端行为正确，是用例传错参/等错状态/证据没落全 | 改 `tests/lobertest/`，不动 `internal/` |
| (b) 后端缺陷 | 后端行为不符合 SPEC 或自身文档 | **先补能在 VM 之外复现的 Go 单测**，再改代码 |
| (c) 环境限制 | QEMU/WHPX/镜像可得性/内存所致 | 记进 README，不改代码 |

混着改的后果是：终于绿了，但分不清哪一处修复真正起了作用，下次换个家族又断。

按四层分支模型，(b) 类的 Go 单测与生产码改动属于**特性分支**，不属于 `test`。
真遇到 (b) 类要开 `feat/<模块名>` 工作树改，再走 dev 集成回 test。

