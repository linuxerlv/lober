-- 001_init.sql：Lober MVP 初始 schema（计划任务 13，依据 SPEC §2.1-§2.7、§3.5、§3.6、§4.3、§4.4）。
--
-- 字段与 internal/core/model 的类型一一对应：时间列存 Unix 秒（INTEGER）；
-- 结构化字段（制品路径、表单参数、requires、params_schema）存 JSON 文本列（TEXT），
-- 序列化/反序列化是 Store 实现（M2）的职责，本层只保证列存在、类型正确。
--
-- 两条安全索引（见文件末尾）：
--   1. idx_tasks_sn_active：tasks 按 sn 的 partial unique index，条件是 state 不在
--      终态集合内。保证同一 SN 只有一个非终态任务——没有它，网关查到同一主机的两个
--      pending 任务时无法决定渲染哪种引导形态（可能是 wimboot 与 mboot.efi 这种完全
--      不同的东西）。终态任务不占用这条索引，装完的机器可以再建新任务。
--   2. idx_task_events_task_seq：task_events 上 (task_id, seq) 的唯一索引，是信号
--      去重的唯一依据。

-- schema_version：记录已应用的迁移文件版本号，供迁移执行器判断哪些文件已跑过。
CREATE TABLE schema_version (
    version     INTEGER NOT NULL PRIMARY KEY,
    applied_at  INTEGER NOT NULL
);

-- hosts：已发现主机清单（model.Host，SPEC §4.3）。
-- sn 故意不建唯一索引——白牌机 SN 可能留空、填 "To Be Filled By O.E.M."、
-- 甚至整批重复；mac 是网卡地址，可以唯一。arch 与 firmware 是必填列
-- （SPEC §4.3 末条：建任务过滤与引导时再校验都依赖这两列非空）。
CREATE TABLE hosts (
    sn              TEXT NOT NULL,
    mac             TEXT NOT NULL,
    uuid            TEXT NOT NULL DEFAULT '',
    arch            TEXT NOT NULL,
    firmware        TEXT NOT NULL,
    rack            TEXT NOT NULL DEFAULT '',
    last_seen_at    INTEGER NOT NULL,
    PRIMARY KEY (mac)
);

-- artifact_sets：一次 ISO 导入产出的制品集（model.ArtifactSet，SPEC §2.1）。
-- firmware 是必填列（Linux 四家取 any，Windows 取具体值，ESXi 只有 uefi）。
-- paths 是 ArtifactPaths 的 JSON 序列化（kernel/initrd/repo/extra）。
CREATE TABLE artifact_sets (
    id                  TEXT NOT NULL PRIMARY KEY,
    name                TEXT NOT NULL,
    os_family           TEXT NOT NULL,
    os_version          TEXT NOT NULL,
    arch                TEXT NOT NULL,
    firmware            TEXT NOT NULL,
    source_iso_sha256   TEXT NOT NULL DEFAULT '',
    state               TEXT NOT NULL,
    manifest_sha256     TEXT NOT NULL DEFAULT '',
    paths_json          TEXT NOT NULL
);

-- hooks：用户注册的机器侧脚本（model.Hook，SPEC §2.2、§2.7）。
-- 主键是 (id, version)——同一 id 的每个版本都是独立行，digest 反查渲染时用。
-- os_families 存 JSON 数组；requires 是 HookRequires 的 JSON 序列化。
CREATE TABLE hooks (
    id                  TEXT NOT NULL,
    version             INTEGER NOT NULL,
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    stage               TEXT NOT NULL,
    os_families_json    TEXT NOT NULL DEFAULT '[]',
    interpreter         TEXT NOT NULL,
    body                TEXT NOT NULL,
    body_sha256         TEXT NOT NULL,
    params_schema       TEXT NOT NULL DEFAULT '',
    requires_json       TEXT NOT NULL DEFAULT '{}',
    required            INTEGER NOT NULL DEFAULT 0,
    timeout_sec         INTEGER NOT NULL DEFAULT 0,
    state               TEXT NOT NULL,
    author              TEXT NOT NULL DEFAULT '',
    approver            TEXT NOT NULL DEFAULT '',
    approved_at         INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id, version)
);

-- templates：组装单元（model.Template，SPEC §2.3），必须版本化。
-- hook_refs 拆到独立表 template_hook_refs（下方），本表只留模板自身字段。
-- required_form_fields 存 JSON 数组。
CREATE TABLE templates (
    id                      TEXT NOT NULL,
    version                 INTEGER NOT NULL,
    name                    TEXT NOT NULL,
    artifact_set_id         TEXT NOT NULL,
    answer_template         TEXT NOT NULL,
    kernel_args             TEXT NOT NULL DEFAULT '',
    disk_policy             TEXT NOT NULL,
    on_failure              TEXT NOT NULL,
    required_form_fields_json TEXT NOT NULL DEFAULT '[]',
    state                   TEXT NOT NULL,
    PRIMARY KEY (id, version)
);

-- template_hook_refs：模板对某个 hook 版本的引用（model.HookRef，SPEC §2.3、§2.7）。
-- pin 住的是 digest 不是 id/version——hook_id + hook_digest 才是不可变引用。
-- params_defaults 存 JSON 对象。
CREATE TABLE template_hook_refs (
    template_id         TEXT NOT NULL,
    template_version    INTEGER NOT NULL,
    hook_id             TEXT NOT NULL,
    hook_digest         TEXT NOT NULL,
    stage               TEXT NOT NULL,
    ref_order           INTEGER NOT NULL,
    required            INTEGER NOT NULL DEFAULT 0,
    params_defaults_json TEXT NOT NULL DEFAULT '{}'
);

-- tasks：一次装机任务的完整记录（model.Task，SPEC §2.4）。
-- form_data / resolved_params 存 JSON 对象；hook_digests 存 JSON 数组；
-- failure_detail 是 *FailureDetail 的 JSON 序列化，NULL 表示未设置。
CREATE TABLE tasks (
    id                      TEXT NOT NULL PRIMARY KEY,
    created_at              INTEGER NOT NULL,
    created_by              TEXT NOT NULL DEFAULT '',

    host_sn                 TEXT NOT NULL DEFAULT '',
    host_mac                TEXT NOT NULL DEFAULT '',
    host_uuid               TEXT NOT NULL DEFAULT '',

    template_id             TEXT NOT NULL,
    template_version        INTEGER NOT NULL,
    artifact_set_id         TEXT NOT NULL,
    hook_digests_json       TEXT NOT NULL DEFAULT '[]',

    form_data_json          TEXT NOT NULL DEFAULT '{}',
    resolved_params_json    TEXT NOT NULL DEFAULT '{}',

    state                   TEXT NOT NULL,
    state_updated_at        INTEGER NOT NULL,
    version                 INTEGER NOT NULL DEFAULT 1,

    current_phase           TEXT NOT NULL,
    phase_entered_at        INTEGER NOT NULL,

    attempt_count           INTEGER NOT NULL DEFAULT 0,
    last_seq                INTEGER NOT NULL DEFAULT 0,
    last_signal_at          INTEGER NOT NULL DEFAULT 0,

    token                   TEXT NOT NULL DEFAULT '',
    token_expires_at        INTEGER NOT NULL DEFAULT 0,
    finalize_token          TEXT NOT NULL DEFAULT '',
    finalize_expires_at     INTEGER NOT NULL DEFAULT 0,

    failure_reason          TEXT NOT NULL DEFAULT '',
    failure_detail_json     TEXT
);

-- task_events：任务事件表，append-only（model.TaskEvent，SPEC §2.5）。
-- (task_id, seq) 的唯一索引是信号去重的唯一依据，见下方索引定义。
CREATE TABLE task_events (
    id              INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    task_id         TEXT NOT NULL,
    seq             INTEGER NOT NULL,
    ts              INTEGER NOT NULL,
    phase           TEXT NOT NULL,
    level           TEXT NOT NULL,
    message         TEXT NOT NULL DEFAULT '',
    rc              INTEGER NOT NULL DEFAULT 0,
    hook_digest     TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL
);

-- task_facts：facts KV（SPEC §5.5）。跨阶段、跨重启传递结构化数据的通用原语，
-- 不对应 model 里的具名类型——语义是纯粹的 (task_id, key) -> value 存储。
CREATE TABLE task_facts (
    task_id     TEXT NOT NULL,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL DEFAULT '',
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (task_id, key)
);

-- audit_log：人面操作审计（SPEC §9.4 权限分级：建任务、hook 增删改、hook 激活审批、
-- provider 配置变更等敏感操作留痕）。不对应 model 里的具名类型。
CREATE TABLE audit_log (
    id          INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    actor       TEXT NOT NULL,
    action      TEXT NOT NULL,
    target      TEXT NOT NULL DEFAULT '',
    detail_json TEXT NOT NULL DEFAULT '{}'
);

-- ========== 索引 ==========

-- 安全索引 1：同一 SN 只允许一个非终态任务（SPEC §3.6、§4.7 约束 2）。
-- 没有它，网关查到同一主机的两个 pending 任务时无法决定渲染哪种引导形态。
-- 终态（done/failed）任务不占用这条索引，装完的机器可以再建新任务。
CREATE UNIQUE INDEX idx_tasks_sn_active ON tasks (host_sn)
    WHERE state NOT IN ('done', 'failed') AND host_sn != '';

-- 安全索引 2：(task_id, seq) 唯一——信号去重的唯一依据（SPEC §2.5、§3.6）。
CREATE UNIQUE INDEX idx_task_events_task_seq ON task_events (task_id, seq);

-- 两个 token 各自的唯一索引，条件是非空——避免 token 冲突被误发给两个任务。
CREATE UNIQUE INDEX idx_tasks_token ON tasks (token) WHERE token != '';
CREATE UNIQUE INDEX idx_tasks_finalize_token ON tasks (finalize_token) WHERE finalize_token != '';

-- 查找与扫描索引：
-- 按身份查 pending/非终态任务（网关 /v1/boot 按 SN → MAC → UUID 回退匹配，SPEC §4.3）。
CREATE INDEX idx_tasks_host_mac ON tasks (host_mac);
CREATE INDEX idx_tasks_host_uuid ON tasks (host_uuid);

-- 按阶段进入时间扫描超时（M3 状态机的超时扫描循环，SPEC §3.3）。
CREATE INDEX idx_tasks_phase_entered ON tasks (current_phase, phase_entered_at);

-- 按 digest 反查 hook（渲染时用 hook_digest 找回具体 hook 内容，SPEC §2.7）。
CREATE INDEX idx_hooks_digest ON hooks (body_sha256);

-- hosts 上 sn 的普通索引（非唯一——见上方设计说明）。
CREATE INDEX idx_hosts_sn ON hosts (sn);

-- template_hook_refs 按模板版本查询其 hook 引用列表（M4 渲染器装配 RenderContext）。
CREATE INDEX idx_template_hook_refs_template ON template_hook_refs (template_id, template_version);

-- task_events 按 task_id 查询某任务的全部事件（M6 控制台任务详情页）。
CREATE INDEX idx_task_events_task ON task_events (task_id, ts);
