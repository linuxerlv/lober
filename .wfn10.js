export const meta = {
  name: 'lober-console-https-only',
  description: 'HTTPS only between browser and console; machine plane stays HTTP. Retire the abandoned full-TLS and Secure Boot material.',
  phases: [{ title: 'Scope', detail: '人面上 HTTPS、机器面保持 HTTP，清理已放弃部分的遗留表述' }],
}

const WT = 'E:/src/lober-wt/n10-console-https'

const R = {
  type: 'object',
  properties: {
    files: { type: 'array', items: { type: 'string' } },
    commits: { type: 'array', items: { type: 'string' } },
    verifyCommands: { type: 'array', items: { type: 'string' } },
    verifyOutcome: { type: 'string' },
    findings: { type: 'array', items: { type: 'string' } },
    contractGaps: { type: 'array', items: { type: 'string' } },
    unfinished: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string' },
  },
  required: ['files', 'commits', 'verifyCommands', 'verifyOutcome', 'findings', 'contractGaps', 'unfinished', 'notes'],
}

phase('Scope')

const r = await agent(`你是 Lober 项目的实现者。Windows 主机，bash shell，Go 1.26.4。

## 你的工作区

  worktree：${WT}
  分支：    feat/n10-console-https

**所有操作都在这个 worktree 里**（绝对路径，或先 \`cd ${WT}\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录。**

## 用户刚做的两个决定

**决定一：放弃 TLS 的全量方案与 Secure Boot。** 这两块不做了，不是推迟。

**决定二：HTTPS 只用在浏览器与控制台之间；装机端与网关之间保持 HTTP。**

这个切法绕开了原方案最贵的部分。SPEC §1.5 说 TLS 在这套栈里最贵的是
**iPXE 需要重新编译**（带 crypto、用 \`TRUST=\` 嵌内网 CA），之后每次证书轮换都要
重编重发引导文件。机器面保持 HTTP 意味着 **iPXE 完全不碰**，那个成本整块消失。

安全含义要在文档里写清楚，避免将来有人误读成"安全性打了折"：
- 机器面的防线本来就在协议层，不在传输层：§9.1 的三道闸门（默认拒绝、
  SN + MAC 双因子、attempt 封顶）与 token 都与 scheme 无关。
- 人面上 HTTPS 换来的是实质保护：控制台的 access token 与登录凭据不再明文
  经过管理网。这是两个信任域不同风险画像的合理差异，不是妥协。
- SPEC §1.5 关于明文的风险缓解仍然有效且仍然必要（root 不设可复用密码、
  注入 SSH 公钥、unattend.xml 不放域管凭据）—— **机器面仍是明文，那些措施
  一条都不能松**。

## 仓库现状

\`dev\` 分支：17 个 Go 包 \`go test ./...\` 全绿，\`gofmt\` 干净。
四条端到端脚本 exit 0（e2e.sh 18 PASS、e2e-debian 18、e2e-ubuntu 20，另有
e2e-windows 19、e2e-esxi 20；e2e-suse 正由另一轨补完阶段 6-9）。

**前端已经是同源相对路径**：\`web/apps/web-ele/.env.production\` 的
\`VITE_GLOB_API_URL=/console/v1\`。浏览器跟着页面的 scheme 走，
**所以前端零改动** —— 不要去改 \`web/\` 下任何文件。

\`deploy/nginx/lober.conf\` 里已经有一段完整的 TLS 备选段（注释状态），
**但它是「两面都上 TLS」的方案**：里面机器面那部分依赖 iPXE 重编译，
还写了「先编译带 crypto 的 iPXE，后收紧 nginx 配置」这类现在已经不做的步骤。
**你要把它改成只上人面。**

## 你拥有的文件（只有这些）

  deploy/nginx/lober.conf
  deploy/README.md
  deploy/lober.yaml.example
  deploy/ca/README.md（证书签发仍然需要 —— 人面证书要签，但用途收窄到只签人面）
  docs/tls-与安全启动-范围决定.md（**新建** —— 把这两个决定与理由写清，供接手人看）

**不要碰**：
- 任何 Go 代码（\`internal/\`、\`cmd/\`）—— 人面 HTTPS 是纯 nginx 配置变更，Go 侧不需要改。
  若你发现 Go 侧确实有东西挡着（比如某处硬编码了 \`http://\`），**写进 findings 与
  contractGaps 让编排方处理**，不要自己改。
- \`web/\` 下任何文件（前端零改动，见上）。
- \`deploy/ipxe/\`（iPXE 重编译已放弃 —— 但**不要删除那个目录**，见下面「怎么处理已放弃
  的产物」）。
- \`docs/secureboot-poc/\`（同上，不删）。
- \`docs/phase4-tls-前提验证.md\` 与 \`docs/phase4-mtls-决策.md\`（同上，不删）。
- 其它 e2e 脚本与 \`internal/\` 下的一切。

## 要做的事

### 1. nginx 人面 server 块加 HTTPS

现有人面 server 块在 \`listen __MANAGEMENT_NET_IP__:80\`。改成支持 HTTPS，要点：

- **\`listen\` 仍写具体 IP，不是 \`0.0.0.0\`**（§1.4 第 1 项：写成通配时一切功能正常、
  测试全过，只是装机 VLAN 上的裸机获得了访问控制台 API 的能力 —— 信任域被静默合并）。
- 人面的客户端是运维的浏览器，**可控** —— 可以只开较新的 TLS 版本（1.2 起，
  或 1.3 起，你判断并说明理由）。这一点与原方案不同：原方案要迁就 iPXE 的
  有限 crypto 支持，现在机器面不上 TLS，人面完全不必受那个约束。
- **HTTP→HTTPS 重定向**：人面可以做（浏览器跟随重定向的行为是确定的）。
  给出配置并说明。
- **SSE 端点的配置在 HTTPS 下必须仍然生效**：关 \`proxy_buffering\` 与
  \`proxy_cache\`、放长 \`proxy_read_timeout\`（§1.4 第 6 项：默认缓冲会让装机日志
  看起来完全冻住）。确认 TLS 不影响这几行。
- 证书路径用醒目的占位符，注释说明必须改。
- **机器面 server 块一个字都不要动** —— 它保持明文 HTTP。在它上方加一段注释
  说明这是**刻意的终局决定**，不是待办项，理由见下面第 3 点要写的文档。

### 2. 把原有的「两面都上 TLS」备选段改成只上人面

现有那段（大约在文件第 240-300 行，先完整读它）包含：
- 机器面的 TLS server 块 → **删除**（iPXE 重编译已放弃）
- 「启用步骤」里第 3 条改 dnsmasq 的 \`http://\` → \`https://\` → **删除**
  （机器面保持 HTTP，dnsmasq 那行不动）
- 第 4 条改 \`gateway_base\` / \`artifact_base\` 为 https → **删除**
  （那两个基址是机器面用的）
- 「机器面与人面的协议版本要求不同」那一节关于 iPXE crypto 的讨论 → **删除**
- 「先编译带 crypto 的 iPXE，后收紧 nginx 配置」 → **删除**

保留并改写的：人面的证书签发与启用步骤、签名链接与 scheme 无关那一节
（\`secure_link\` 在机器面，与人面 HTTPS 无关，确认这一点仍然成立并说明）。

**改动后重读整段确认没有残留矛盾** —— 半改的文档比没改更危险。

### 3. 写 \`docs/tls-与安全启动-范围决定.md\`

这份文档的读者是**接手这个项目的人**，不是现在的开发者。它要回答：

- **哪些不做、为什么**：TLS 的全量方案（机器面 TLS + mTLS）与 Secure Boot 已放弃。
  理由是成本集中在 iPXE 重编译与签名链，而它们换来的保护在这套威胁模型下不划算
  （§1.5 的威胁模型：交换网络上被动嗅探需要端口镜像或装机 VLAN 上已有失陷主机，
  风险面是内部人员与已失陷主机，不是任意旁观者；前提是该 VLAN 真正隔离、
  不路由到办公网）。
- **哪些做了**：浏览器 ↔ 控制台的 HTTPS。它保护的是 access token 与登录凭据。
- **机器面明文的安全论证**：三道闸门与 token 都在协议层，与 scheme 无关；
  §1.5 的缓解措施（root 无可复用密码、注入 SSH 公钥、unattend 不放域管凭据）
  **仍然必要且不可放松**。
- **已放弃部分留下的产物怎么看**（见下一点）。
- **将来若改变主意要做什么**：不要让这份文档变成"永久关闭"的死胡同 ——
  写清重新启用的入口在哪（deploy/ipxe/README.md 与 docs/secureboot-poc/ 仍在），
  以及为什么那时仍然贵。

### 4. 怎么处理已放弃的产物：保留但明确标注，不要删

\`deploy/ipxe/\`（iPXE 重编译）、\`docs/secureboot-poc/\`（五份 PoC 调研）、
\`docs/phase4-tls-前提验证.md\`、\`docs/phase4-mtls-决策.md\` —— 这些是真实的调研成果，
删掉等于把当初核实过的事实一起丢掉（比如 Secure Boot PoC 里那些已核实的签名链机制、
TLS 前提验证里「scheme 完全来自配置」的实证结论）。

**做法**：不删文件，但在 \`docs/tls-与安全启动-范围决定.md\` 里列一份清单，
逐个说明「这份文档描述的能力已放弃、它的价值是什么、什么情况下会用到它」。
你不拥有那些文件所以不能在它们内部加标注 —— **在 findings 里写清哪几个文件
需要在顶部加一行"本能力已放弃，见 docs/tls-与安全启动-范围决定.md"**，
由编排方接线。

### 5. \`deploy/README.md\` 与 \`deploy/lober.yaml.example\`

README 的部署顺序与核对清单要反映新范围：人面要签证书、机器面不用。
配置样例里若有暗示机器面将来会上 TLS 的注释，改成明确的终局表述。

## 验证

本机没有 nginx，\`nginx -t\` **跑不了** —— 做静态自检：
- 逐行核对 HTTPS server 块的必需指令（证书、协议版本、SSE 那几行）
- 括号与分号配平（用一个小脚本数 \`{\` 与 \`}\`）
- 确认机器面 server 块的 diff 是**零**（\`git diff\` 看，它一个字都不该变）
- 确认改动后的文档里没有残留的「机器面 TLS」「iPXE 重编译」类表述
  （grep 自查）

在 verifyOutcome 里明确写出哪条验证因缺工具无法执行、需在目标主机补做
（\`nginx -t\`、证书实际可用性、浏览器实测 HTTPS）。

另外跑一次 \`cd ${WT} && go test ./... -count=1\` 与 \`bash scripts/e2e.sh\`
确认你没碰坏任何东西（你不该碰 Go 代码，这是确认）。

## 铁律

1. **严禁 rm -rf，严禁删除任何目录或文件**（本轮明确要求保留已放弃部分的产物）。
   临时文件写在 \`${WT}/.tmp/\` 下，用完只删你建的那个。
2. git 只允许 \`add\` / \`commit\` / \`status\` / \`diff\` / \`log\`。**严禁** checkout / switch /
   branch / merge / rebase / reset / stash / clean / push / worktree / 带 -f 或 --hard 的命令。
3. 禁止改 go.mod / go.sum。
4. 配置文件里所有占位值用醒目形式并在注释里说明必须改，**不要写一个看起来能直接用的假值**。
5. 中文注释与文档。
6. **完成前必须真的执行验证并看到输出。不要声称通过。**

全绿后 commit。`, { label: 'n10:console-https', phase: 'Scope', schema: R, effort: 'high' })

return r ? {
  files: r.files, commits: r.commits, outcome: r.verifyOutcome,
  findings: r.findings, gaps: r.contractGaps, unfinished: r.unfinished, notes: r.notes,
} : '失败'
