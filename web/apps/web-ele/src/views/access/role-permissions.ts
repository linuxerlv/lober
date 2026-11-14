import type { ConsoleRole } from '#/api/console/session';

export const roleDescriptions: Record<ConsoleRole, string> = {
  viewer: '查看任务、主机和装机资源',
  operator: '维护主机，创建、取消和重试装机任务',
  'hook-author': '编写并提交脚本和模板',
  'hook-approver': '审批脚本和模板',
  admin: '管理账号、菜单和装机业务',
};

// 与服务端 internal/domain/access/permissions.go 的内置角色规则对应。
const allowedRoles: Record<string, ConsoleRole[]> = {
  'host:write': ['operator'],
  'task:create': ['operator'],
  'task:cancel': ['operator'],
  'task:retry': ['operator'],
  'hook:write': ['hook-author'],
  'template:write': ['hook-author'],
  'hook:approve': ['hook-approver'],
  'template:approve': ['hook-approver'],
};

const groups: { name: string; actions: [string, string][] }[] = [
  {
    name: '装机任务',
    actions: [
      ['task:read', '查看任务'],
      ['task:create', '创建任务'],
      ['task:cancel', '取消任务'],
      ['task:retry', '重试任务'],
    ],
  },
  {
    name: '主机',
    actions: [
      ['host:read', '查看主机'],
      ['host:write', '维护主机及请求硬件采集'],
    ],
  },
  {
    name: '系统镜像',
    actions: [
      ['iso:read', '查看镜像'],
      ['iso:write', '登记和管理镜像'],
    ],
  },
  {
    name: '模板',
    actions: [
      ['template:read', '查看模板'],
      ['template:write', '编辑并提交模板'],
      ['template:approve', '审批模板'],
    ],
  },
  {
    name: 'Hook 脚本',
    actions: [
      ['hook:read', '查看脚本'],
      ['hook:write', '编辑并提交脚本'],
      ['hook:approve', '审批脚本'],
    ],
  },
  {
    name: '系统管理',
    actions: [
      ['user:manage', '管理账号与角色菜单'],
      ['audit:read', '查看操作记录'],
    ],
  },
];

function permissionGroups(allows: (code: string) => boolean) {
  return groups.map(({ name, actions }) => ({
    name,
    actions: actions.map(([code, label]) => ({
      code,
      label,
      allowed: allows(code),
    })),
  }));
}

export function rolePermissionGroups(role: ConsoleRole) {
  return permissionGroups(
    (code) =>
      role === 'admin' ||
      (code.endsWith(':read') && code !== 'audit:read') ||
      !!allowedRoles[code]?.includes(role),
  );
}

export function accountPermissionGroups(codes: string[]) {
  const result = permissionGroups((code) => codes.includes(code));
  const known = new Set(
    groups.flatMap((group) => group.actions.map(([code]) => code)),
  );
  const other = [...new Set(codes)].filter((code) => !known.has(code));
  if (other.length)
    result.push({
      name: '其他操作',
      actions: other.map((code) => ({ code, label: code, allowed: true })),
    });
  return result;
}
