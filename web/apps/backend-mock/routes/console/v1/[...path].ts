import { summarizeBatch } from '../../../utils/batch-data';
import { randomUUID } from 'node:crypto';

import {
  defineEventHandler,
  getHeader,
  getMethod,
  getQuery,
  getRequestURL,
  readBody,
  setHeader,
  setResponseStatus,
} from 'h3';

import {
  loginPreview,
  logoutPreview,
  previewMenus,
  previewUser,
  previewUsers,
  savePreviewAccount,
  savePreviewMenus,
} from '../../../utils/access-preview';

const now = () => Math.floor(Date.now() / 1000);
const hosts = Array.from({ length: 34 }, (_, index) => ({
  sn: 'LAB-SRV-' + String(index + 1).padStart(3, '0'),
  arch: index >= 28 ? 'aarch64' : 'x86_64',
  firmware: 'uefi',
  rev: 1,
  attrs: {
    rack: index < 15 ? 'A03' : 'B02',
    purpose: '装机验证',
    ...(index % 3 === 0
      ? {
          inv_cpu_model: 'Intel Xeon Silver 4310',
          inv_cpu_cores: '24',
          inv_mem_mib: '65536',
          inv_system_model: 'Lober Lab R240',
          inv_system_disk: '/dev/sda',
          inv_disks:
            index % 2 === 0
              ? 'sda:480000000000:Intel SSD D3-S4510\nsdb:960000000000:Samsung PM893'
              : 'nvme0n1:1920000000000:Samsung PM9A3\nnvme1n1:3840000000000:Micron 7450 PRO',
          ...(index < 12
            ? {
                inv_nics: JSON.stringify([
                  {
                    device: 'eno1',
                    model: 'Intel I350 Gigabit Network',
                    mac:
                      '02:00:00:00:01:' +
                      (index + 1).toString(16).padStart(2, '0'),
                  },
                  {
                    device: 'eno2',
                    model: 'Intel I350 Gigabit Network',
                    mac:
                      '02:00:00:00:02:' +
                      (index + 1).toString(16).padStart(2, '0'),
                  },
                ]),
              }
            : {}),
          inv_collected_at: String(now() - 3600),
        }
      : {}),
  },
}));
const images = [
  {
    id: 'rocky',
    name: 'Rocky Linux 9.6',
    version: '9.6',
    arch: 'x86_64',
    family: 'linux',
    series: 'rocky',
  },
  {
    id: 'ubuntu',
    name: 'Ubuntu Server 24.04 LTS',
    version: '24.04',
    arch: 'x86_64',
    family: 'linux',
    series: 'ubuntu',
  },
  {
    id: 'ubuntu-arm',
    name: 'Ubuntu Server 24.04 LTS',
    version: '24.04',
    arch: 'aarch64',
    family: 'linux',
    series: 'ubuntu',
  },
];
const stringParam = (name: string, label: string, value: string) => ({
  name,
  label,
  default: value,
  type: 'string',
  list: false,
  required: true,
  options: [],
  offer: '',
  min: null,
  max: null,
});
const initSlot = {
  name: 'startup-check',
  timing: 'firstboot',
  exec: true,
  line: 8,
  col: 1,
};
const templates = images.map((iso) => ({
  id: iso.id + '-standard',
  name:
    (iso.series === 'rocky' ? 'Rocky Linux' : 'Ubuntu Server') + ' 标准装机',
  version: 4,
  builtin: true,
  usable: true,
  problems: [],
  state: 'approved',
  family: iso.family,
  series: [iso.series],
  author: 'system',
  approver: 'system',
  rev: 1,
  body: [
    '#cloud-config',
    'hostname: lab-server',
    'timezone: Asia/Shanghai',
    'package_update: false',
    'packages:',
    '  - chrony',
    'runcmd:',
    '  - [systemctl, enable, --now, chronyd]',
    '',
  ].join('\n'),
  assembly: {
    [initSlot.name]: [
      {
        hookID: 'inventory-check',
        hookVersion: 1,
        required: false,
        timeoutSec: 60,
        bindings: {},
      },
    ],
  },
  bindings: {
    timezone: {
      source: 'fixed',
      value: 'Asia/Shanghai',
      attr: '',
      editable: true,
    },
  },
  params: [
    stringParam('hostname', '主机名', 'lab-server'),
    stringParam('timezone', '时区', 'Asia/Shanghai'),
  ],
  derivedFrom: null,
}));
const hooks = [
  {
    id: 'inventory-check',
    name: '初始化检查',
    version: 1,
    rev: 1,
    state: 'approved',
    builtin: true,
    author: 'system',
    approver: 'system',
    description: '装机完成后检查系统是否正常启动。',
    required: false,
    timeoutSec: 60,
    params: [stringParam('service', '检查的服务', 'chronyd')],
    problems: [],
    impls: [
      {
        family: 'linux',
        series: ['rocky', 'ubuntu'],
        kind: 'script',
        interpreter: 'sh',
        timings: ['firstboot'],
        body: '#!/bin/sh\nset -eu\nservice_name="${1:-chronyd}"\nsystemctl is-active --quiet "$service_name"\nprintf "Service %s is active\\n" "$service_name"\n',
      },
    ],
  },
];
const catalogImages = [
  ...images.map((iso) => ({ ...iso, state: 'ready' })),
  {
    ...images[0]!,
    id: 'rocky-staging',
    name: 'Rocky Linux 9.6 待入库',
    state: 'acquiring',
  },
  {
    ...images[1]!,
    id: 'ubuntu-incomplete',
    name: 'Ubuntu Server 24.04 LTS 导入检查',
    state: 'failed',
  },
].map((iso) => ({
  ...iso,
  rev: 1,
  offers: null,
  sha256: '',
  artifacts:
    iso.state === 'ready' ? { iso: '/fixtures/' + iso.id + '.iso' } : {},
}));
const problem = (message: string, path = '') => ({
  code: 'invalid_param_value',
  message,
  next: '',
  subject: { kind: 'param', path },
});
function makeTask(
  id: string,
  host: (typeof hosts)[number],
  image: (typeof images)[number],
  state: string,
  createdAt: number,
  createdBy = 'operator',
  batchID?: string,
) {
  const template = templates[images.indexOf(image)]!;
  return {
    id,
    batchID,
    hostSN: host.sn,
    state,
    createdAt,
    createdBy,
    rev: 1,
    isoID: image.id,
    templateID: template.id,
    templateVersion: template.version,
    snapshot: { host, iso: image, template, slots: [] },
    canCancel: state === 'pending' ? [] : [problem('仅等待引导的任务可以取消')],
    canRetry: state === 'failed' ? [] : [problem('仅失败的任务可以重试')],
  };
}
const states = [
  'installing',
  'failed',
  'pending',
  'done',
  'initializing',
  'done',
  'cancelled',
  'rebooting',
];
const previewBatchAt = now() - 780;
const previewBatchRows = new Set([1, 3, 7, 9]);
const tasks = Array.from({ length: 25 }, (_, i) =>
  makeTask(
    'TSK-' + String(1048 - i),
    hosts[i]!,
    images[i % 2]!,
    states[i % states.length]!,
    previewBatchRows.has(i) ? previewBatchAt : now() - i * 780,
    'operator',
    previewBatchRows.has(i) ? 'batch-lab-ubuntu' : undefined,
  ),
);
const taskEvents = new Map<string, ReturnType<typeof initialEvents>>();
tasks.push(
  makeTask('TSK-1002', hosts[1]!, images[0]!, 'done', now() - 86_400),
  makeTask('TSK-1001', hosts[1]!, images[1]!, 'cancelled', now() - 172_800),
);
function initialEvents(task: (typeof tasks)[number]) {
  const kinds = ['task.created'];
  if (!['pending', 'cancelled'].includes(task.state))
    kinds.push('machine.boot', 'machine.installing');
  if (['rebooting', 'initializing', 'done'].includes(task.state))
    kinds.push('machine.reboot');
  if (['initializing', 'done'].includes(task.state)) kinds.push('machine.init');
  if (task.state === 'done') kinds.push('machine.done');
  if (task.state === 'failed') {
    kinds.push(
      ...Array.from({ length: 20 }, () => 'installer.syslog'),
      'machine.failed',
    );
  }
  if (task.state === 'cancelled') kinds.push('machine.cancel');
  return kinds.map((kind, index) => ({
    seq: index + 1,
    kind,
    key: kind,
    taskID: task.id,
    at: task.createdAt + index,
    fields:
      kind === 'machine.failed'
        ? {
            reason: '安装程序未找到系统磁盘 /dev/sda',
            log_base64: Buffer.from(
              '扫描块设备：仅发现 /dev/nvme0n1\n系统磁盘 /dev/sda 不存在，安装已停止。',
            ).toString('base64'),
          }
        : kind === 'installer.syslog'
          ? {
              log_base64: Buffer.from(
                '安装程序正在检测硬件和存储设备。',
              ).toString('base64'),
            }
          : {},
  }));
}
for (const task of tasks) taskEvents.set(task.id, initialEvents(task));

const auditRecords: {
  action: string;
  actor: string;
  at: number;
  detail: string;
  id: number;
  targetID: string;
  targetKind: string;
}[] = [];
let nextAuditID = 1;

function recordAudit(
  actor: string,
  action: string,
  targetKind: string,
  targetID: string,
  at = now(),
  detail = '',
) {
  auditRecords.unshift({
    id: nextAuditID++,
    actor,
    action,
    at,
    targetKind,
    targetID,
    detail,
  });
}
for (const task of [...tasks].reverse()) {
  recordAudit(task.createdBy, 'task.create', 'task', task.id, task.createdAt);
}
for (const image of catalogImages) {
  recordAudit('admin', 'iso.create', 'iso', image.id, now() - 172_800);
  recordAudit(
    'admin',
    'iso.source.register',
    'iso',
    image.id,
    now() - 86_400,
    '登记主镜像来源',
  );
}
for (const account of previewUsers()) {
  recordAudit('admin', 'user.create', 'user', account.username, now() - 86_400);
}

function page<T>(items: T[], query: Record<string, unknown>) {
  const size = Math.max(1, Math.min(200, Number(query.page_size) || 20));
  const offset = (Math.max(1, Number(query.page) || 1) - 1) * size;
  return {
    items: items.slice(offset, offset + size),
    total: items.length,
    next: '',
  };
}
function openTask(sn: string) {
  return tasks.some(
    (t) =>
      t.hostSN === sn && !['done', 'failed', 'cancelled'].includes(t.state),
  );
}
const knownHosts = (sns: string[]) => hosts.filter((h) => sns.includes(h.sn));
const eligible = (sns: string[], iso: (typeof images)[number]) => {
  const selected = knownHosts(sns);
  return (
    selected.length > 0 &&
    selected.length === sns.length &&
    selected.every((h) => h.arch === iso.arch)
  );
};
type Entered = Record<string, string>;
const enteredByTask = new Map<string, Entered>();
function params(sn: string, entered: Entered = {}, validate = false) {
  const fields = [
    { key: 'hostname', label: '主机名', prefill: sn.toLowerCase() },
    { key: 'disk', label: '系统磁盘', prefill: '/dev/sda' },
    { key: 'timezone', label: '时区', prefill: 'Asia/Shanghai' },
    { key: 'firmware', label: '固件模式', prefill: 'UEFI', locked: true },
  ].map((f) => ({
    locked: false,
    required: true,
    list: false,
    options: [],
    type: 'string',
    ...f,
  }));
  const problems = [];
  if (validate) {
    if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(entered.hostname ?? ''))
      problems.push(problem('请输入有效的主机名', 'hostname'));
    if (!/^\/dev\/[a-z0-9_/-]+$/i.test(entered.disk ?? ''))
      problems.push(problem('请输入磁盘设备路径，例如 /dev/sda', 'disk'));
    try {
      new Intl.DateTimeFormat('zh-CN', { timeZone: entered.timezone ?? '' });
    } catch {
      problems.push(problem('请输入有效时区，例如 Asia/Shanghai', 'timezone'));
    }
  }
  return { sn, fields, problems };
}
const ok = (data: unknown) => ({ code: 0, data, message: '' });
let sequence = 1049;

export default defineEventHandler(async (event) => {
  const path = getRequestURL(event).pathname.replace(/^\/console\/v1/, '');
  const method = getMethod(event);
  const query = getQuery(event);
  const fail = (status: number, message: string, data: unknown = null) => {
    setResponseStatus(event, status);
    return { code: status, data, message };
  };
  if (path === '/auth/login' && method === 'POST') {
    const login = await readBody(event);
    const session = loginPreview(login?.username, login?.password);
    return session ? ok(session) : fail(401, '用户名或密码不正确');
  }
  const token =
    /^Bearer (.+)$/.exec(getHeader(event, 'authorization') ?? '')?.[1] ?? '';
  const user = previewUser(token);
  if (!user) return fail(401, '请先登录');
  if (path === '/user/info' && method === 'GET') return ok(user);
  if (path === '/auth/logout' && method === 'POST') {
    logoutPreview(token);
    return ok(null);
  }
  if (path === '/audit' && method === 'GET') {
    if (!user.permissions.includes('audit:read'))
      return fail(403, '没有查看变更记录的权限');
    return ok(
      page(
        auditRecords.filter(
          (record) =>
            (!query.target_kind || record.targetKind === query.target_kind) &&
            (!query.target_id || record.targetID === query.target_id) &&
            (!query.actor || record.actor === query.actor) &&
            (!query.action || record.action === query.action) &&
            (!query.from || record.at >= Number(query.from)) &&
            (!query.to || record.at <= Number(query.to)),
        ),
        query,
      ),
    );
  }
  if (/^\/(users|roles)(\/|$)/.test(path)) {
    if (!user.permissions.includes('user:manage'))
      return fail(403, '没有权限管理账号和角色菜单');
    if (path === '/users' && method === 'GET')
      return ok(page(previewUsers(), query));
    if (path === '/roles/menus' && method === 'GET') return ok(previewMenus());
    const role = /^\/roles\/([^/]+)\/menus$/.exec(path)?.[1];
    if (role && method === 'PUT') {
      const result = savePreviewMenus(role, await readBody(event));
      return result.status
        ? fail(result.status, result.message)
        : ok(result.data);
    }
    const username = /^\/users\/([^/]+)$/.exec(path)?.[1];
    if (
      (path === '/users' && method === 'POST') ||
      (username && method === 'PUT')
    ) {
      const result = savePreviewAccount(username, await readBody(event));
      if (result.status) return fail(result.status, result.message);
      recordAudit(
        user.username,
        username ? 'user.update' : 'user.create',
        'user',
        result.data.username,
      );
      return ok(result.data);
    }
    return fail(404, '找不到权限管理接口');
  }
  if (path.startsWith('/wizard/') && !user.permissions.includes('task:create'))
    return fail(403, '没有创建任务权限');
  if (
    method === 'POST' &&
    /^\/tasks\/[^/]+\/(cancel|retry)$/.test(path) &&
    !user.permissions.includes('task:' + path.split('/').at(-1))
  )
    return fail(403, '没有任务操作权限');
  if (method === 'GET' && /^\/hosts\/[^/]+$/.test(path)) {
    const host = hosts.find((row) => row.sn === path.slice('/hosts/'.length));
    return host ? ok(host) : fail(404, '找不到主机');
  }
  const imageOperation = /^\/isos\/([^/]+)\/(cancel|retry|reimport)$/.exec(
    path,
  );
  if (imageOperation && method === 'POST') {
    if (!user.permissions.includes('iso:write'))
      return fail(403, '没有修改镜像的权限');
    const iso = catalogImages.find((item) => item.id === imageOperation[1]);
    if (!iso) return fail(404, '找不到镜像');
    const operation = imageOperation[2];
    if (
      (operation === 'cancel' && iso.state !== 'acquiring') ||
      (operation === 'retry' && !['failed', 'cancelled'].includes(iso.state)) ||
      (operation === 'reimport' && iso.state !== 'ready')
    )
      return fail(409, '当前状态不能执行此操作');
    iso.state = operation === 'cancel' ? 'cancelled' : 'acquiring';
    iso.rev++;
    recordAudit(user.username, 'iso.' + operation, 'iso', iso.id);
    return ok(null);
  }
  if (method === 'GET') {
    if (path === '/isos')
      return ok(
        page(
          catalogImages.filter(
            (iso) =>
              (!query.family || query.family === iso.family) &&
              (!query.state || query.state === iso.state),
          ),
          query,
        ),
      );
    const imageMatch = /^\/isos\/([^/]+)(\/progress)?$/.exec(path);
    if (imageMatch) {
      const iso = catalogImages.find((item) => item.id === imageMatch[1]);
      if (!iso) return fail(404, '找不到镜像');
      if (!imageMatch[2]) return ok(iso);
      const totalSize = 4 * 1024 ** 3;
      const fetched =
        iso.state === 'ready'
          ? totalSize
          : iso.state === 'acquiring'
            ? 2 * 1024 ** 3
            : 0;
      return ok({
        id: iso.id,
        rev: iso.rev,
        state: iso.state,
        sources: [
          {
            kind: 'url',
            role: '',
            rev: 1,
            state: iso.state,
            totalSize,
            fetched,
            ranges: fetched ? [{ start: 0, length: fetched }] : [],
            error:
              iso.state === 'failed'
                ? '来源文件校验失败，请核对镜像文件与校验值。'
                : '',
          },
        ],
      });
    }
    if (path === '/templates' || path === '/hooks') {
      const items = path === '/templates' ? templates : hooks;
      return ok(
        page(
          [...items].filter(
            (row) =>
              (!query.state || row.state === query.state) &&
              (!query.q || row.name.includes(String(query.q))),
          ),
          query,
        ),
      );
    }
    const version =
      /^\/(templates|hooks)\/([^/]+)\/versions(?:\/(\d+)(?:\/(slots|source-diff))?)?$/.exec(
        path,
      );
    if (version) {
      const items = version[1] === 'templates' ? templates : hooks;
      const row = items.find(
        (item) =>
          item.id === version[2] &&
          (!version[3] || item.version === Number(version[3])),
      );
      if (!row) return fail(404, '找不到该版本');
      if (!version[3]) return ok({ items: [row] });
      if (version[4] === 'slots')
        return ok({ slots: [initSlot], problems: [] });
      if (version[4] === 'source-diff')
        return ok({ changed: false, current: null, original: null });
      return ok(row);
    }
  }
  const batchMatch = /^\/task-batches\/([^/]+)$/.exec(path);
  if (batchMatch && method === 'GET') {
    if (!user.permissions.includes('task:read'))
      return fail(403, '没有查看任务权限');
    const id = batchMatch[1]!;
    if (!/^[a-z0-9][a-z0-9-]*$/.test(id))
      return fail(400, '任务查询参数无效');
    const rows = tasks.filter((task) => task.batchID === id);
    const first = rows.reduce<(typeof rows)[number] | undefined>(
      (earliest, task) => !earliest || task.createdAt < earliest.createdAt ? task : earliest,
      undefined,
    );
    if (!first) return fail(404, '找不到该批次');
    return ok({
      ...summarizeBatch(rows),
      total: rows.length,
      createdAt: first.createdAt,
      createdBy: first.createdBy,
      isoName: first.snapshot.iso.name,
      templateName: first.snapshot.template.name,
      templateVersion: first.templateVersion,
    });
  }
  if (path === '/tasks' && method === 'GET')
    return ok(
      page(
        tasks.filter(
          (t) =>
            (!query.state || t.state === query.state) &&
            (!query.batch_id || t.batchID === query.batch_id) &&
            (!query.host_sn || t.hostSN === query.host_sn) &&
            (!query.created_from ||
              t.createdAt >= Number(query.created_from)) &&
            (!query.created_to || t.createdAt <= Number(query.created_to)),
        ),
        query,
      ),
    );
  if ((path === '/hosts' || path === '/wizard/hosts') && method === 'GET') {
    const prefix = String(query.q || query.prefix || '');
    const items = hosts.filter(
      (h) =>
        h.sn.toLowerCase().startsWith(prefix.toLowerCase()) &&
        (!query.attr_key ||
          Object.entries(h.attrs).some(
            ([key, value]) =>
              key === query.attr_key &&
              (!query.attr_value || value === query.attr_value),
          )),
    );
    return ok(
      page(
        path === '/hosts'
          ? items
          : items.map((h) => {
              const selectable = !openTask(h.sn);
              return {
                ...h,
                selectable,
                problems: selectable ? [] : [problem('已有进行中的装机任务')],
              };
            }),
        query,
      ),
    );
  }
  if (path.startsWith('/wizard/') && method === 'POST') {
    const body = await readBody(event);
    const iso = images.find((i) => i.id === body.isoID);
    if (path === '/wizard/isos')
      return ok(
        page(
          images.map((i) => ({
            ...i,
            usable: eligible(body.hostSNs ?? [], i),
            problems: eligible(body.hostSNs ?? [], i)
              ? []
              : [problem('所选主机与镜像架构不匹配')],
          })),
          body,
        ),
      );
    if (path === '/wizard/templates')
      return ok(
        page(
          templates.filter((t) => t.id === body.isoID + '-standard'),
          body,
        ),
      );
    const template = templates.find(
      (t) =>
        t.id === body.templateID &&
        t.id === body.isoID + '-standard' &&
        t.version === body.templateVersion,
    );
    if (!iso || !template) return fail(422, '请选择有效的安装方案');
    if (path === '/wizard/params')
      return ok({
        common: [],
        hosts: (body.hostSNs as string[]).map((sn) =>
          params(
            sn,
            body.entered?.[sn],
            Object.keys(body.entered?.[sn] ?? {}).length > 0,
          ),
        ),
      });
    if (path === '/wizard/tasks') {
      const selection = body.hosts as { sn: string; entered: Entered }[];
      const sns = selection.map((h) => h.sn);
      if (!eligible(sns, iso) || sns.some(openTask))
        return fail(409, '主机已不符合创建条件，请返回检查');
      const byHost = Object.fromEntries(
        selection
          .map((h) => [h.sn, params(h.sn, h.entered, true).problems])
          .filter(([, problems]) => problems.length),
      );
      if (Object.keys(byHost).length)
        return fail(422, '有参数需要修改', { byHost });
      const batchID = randomUUID();
      const submittedAt = now();
      const created = knownHosts(sns).map((h) =>
        makeTask(
          'TSK-' + sequence++,
          h,
          iso,
          'pending',
          submittedAt,
          user.username,
          batchID,
        ),
      );
      for (const task of created) {
        recordAudit(user.username, 'task.create', 'task', task.id, submittedAt);
        taskEvents.set(task.id, initialEvents(task));
        enteredByTask.set(
          task.id,
          selection.find((host) => host.sn === task.hostSN)!.entered,
        );
      }
      tasks.unshift(...created);
      return ok({ tasks: created });
    }
  }
  const templatePreview =
    /^\/templates\/([^/]+)\/versions\/(\d+)\/preview(-params)?$/.exec(path);
  if (method === 'POST' && templatePreview?.[3]) {
    if (
      !templates.some(
        (item) =>
          item.id === templatePreview[1] &&
          item.version === Number(templatePreview[2]),
      )
    ) {
      return fail(404, '找不到模板版本');
    }
    const body = await readBody(event);
    const { fields } = params(body?.hostSN || 'lab-server');
    return ok({ fields, sample: [], problems: [] });
  }
  const match = /^\/tasks\/([^/]+)(?:\/([^/]+))?$/.exec(path);
  if (match) {
    const task = tasks.find((t) => t.id === match[1]);
    if (!task) return fail(404, '找不到任务');
    const events = taskEvents.get(task.id)!;
    if (!match[2] && method === 'GET') return ok(task);
    if (match[2] === 'events' && method === 'GET')
      return ok({
        items: events
          .filter((e) => e.seq > Number(query.after_seq || 0))
          .slice(0, Math.max(1, Math.min(200, Number(query.limit) || 200))),
      });
    if (match[2] === 'stream' && method === 'GET') {
      const response = event.node.res;
      setHeader(event, 'Content-Type', 'text/event-stream');
      setHeader(event, 'Cache-Control', 'no-cache');
      response.flushHeaders();
      let last = Number(getHeader(event, 'last-event-id') || 0);
      const send = () => {
        response.write(': heartbeat\n\n');
        for (const e of events.filter((e) => e.seq > last)) {
          response.write(
            'id: ' +
              e.seq +
              '\nevent: task\ndata: ' +
              JSON.stringify(e) +
              '\n\n',
          );
          last = e.seq;
        }
      };
      send();
      const timer = setInterval(send, 1500);
      await new Promise<void>((resolve) =>
        response.once('close', () => {
          clearInterval(timer);
          resolve();
        }),
      );
      return;
    }
    if (method === 'POST' && (match[2] === 'cancel' || match[2] === 'retry')) {
      const retry = match[2] === 'retry';
      if (task.state !== (retry ? 'failed' : 'pending'))
        return fail(409, '当前状态不能执行此操作');
      task.state = retry ? 'pending' : 'cancelled';
      task.canCancel = retry ? [] : [problem('任务已取消')];
      task.canRetry = [problem('仅失败的任务可以重试')];
      events.push({
        seq: events.length + 1,
        kind: retry ? 'machine.retry' : 'machine.cancel',
        key: retry ? 'retried' : 'cancelled',
        taskID: task.id,
        at: now(),
        fields: {},
      });
      recordAudit(
        user.username,
        retry ? 'task.retry' : 'task.cancel',
        'task',
        task.id,
      );
      return ok(task);
    }
  }
  if (method === 'POST' && (templatePreview || match?.[2] === 'preview')) {
    const body = await readBody(event);
    const task = match ? tasks.find((row) => row.id === match[1]) : undefined;
    const entered = task ? enteredByTask.get(task.id) : body?.entered;
    return ok({
      sample: [],
      problems: [],
      files: [
        {
          kind: 'config',
          name: 'install.yaml',
          slot: '',
          body:
            'hostname: ' +
            JSON.stringify(
              entered?.hostname ??
                body?.hostSN ??
                task?.hostSN.toLowerCase() ??
                'lab-server',
            ) +
            '\nstorage:\n  disk: ' +
            JSON.stringify(entered?.disk ?? '/dev/sda') +
            '\n',
        },
        ...(templatePreview
          ? [
              {
                kind: 'script',
                name: 'startup-check.sh',
                slot: initSlot.name,
                body: hooks[0]!.impls[0]!.body,
              },
            ]
          : []),
      ],
    });
  }
  return fail(404, '模拟服务未提供此接口');
});
