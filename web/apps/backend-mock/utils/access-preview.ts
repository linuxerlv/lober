const readPermissions = [
  'host:read',
  'iso:read',
  'hook:read',
  'template:read',
  'task:read',
];
const operatorPermissions = ['task:create', 'task:cancel', 'task:retry'];
const permissions = {
  viewer: readPermissions,
  operator: [...readPermissions, ...operatorPermissions],
  'hook-author': [...readPermissions, 'hook:write', 'template:write'],
  'hook-approver': [...readPermissions, 'hook:approve', 'template:approve'],
  admin: [
    ...readPermissions,
    ...operatorPermissions,
    'user:manage',
    'audit:read',
  ],
};
type Role = keyof typeof permissions;
type Account = {
  username: string;
  displayName: string;
  role: Role;
  disabled: boolean;
  rev: number;
  createdAt: number;
  password: string;
};
const now = () => Math.floor(Date.now() / 1000);
const accounts = new Map<string, Account>();
for (const [username, displayName, role] of [
  ['admin', '预览管理员', 'admin'],
  ['operator', '装机操作员', 'operator'],
  ['viewer', '只读用户', 'viewer'],
  ['author', '脚本与模板作者', 'hook-author'],
  ['approver', '脚本与模板审批人', 'hook-approver'],
] as const) {
  accounts.set(username, {
    username,
    displayName,
    role,
    disabled: false,
    rev: 1,
    createdAt: now(),
    password: 'lober-preview',
  });
}

const businessMenus = [
  ['tasks', '任务', '任务列表', 'task:read'],
  ['hosts', '主机清单', '主机清单', 'host:read'],
  ['templates', '模板', '模板', 'template:read'],
  ['hooks', 'Hook', 'Hook 管理与审批', 'hook:read'],
  ['isos', '制品管理', '系统镜像', 'iso:read'],
];
const menus = [
  ...businessMenus.flatMap(([id, group, title, permission]) => [
    {
      id: id + '-group',
      title: group,
      parentId: '',
      permission: '',
      required: false,
    },
    { id, title, parentId: id + '-group', permission, required: false },
  ]),
  {
    id: 'access',
    title: '权限管理',
    parentId: '',
    permission: '',
    required: false,
  },
  {
    id: 'users',
    title: '账号管理',
    parentId: 'access',
    permission: 'user:manage',
    required: false,
  },
  {
    id: 'role-menus',
    title: '角色管理',
    parentId: 'access',
    permission: 'user:manage',
    required: true,
  },
];
const roles = (Object.keys(permissions) as Role[]).map((role) => {
  const availableMenuIds = menus
    .filter(
      (menu) => menu.parentId && permissions[role].includes(menu.permission!),
    )
    .map((menu) => menu.id!);
  return { role, availableMenuIds, menuIds: [...availableMenuIds], rev: 0 };
});
function view(account: Account) {
  const settings = roles.find((role) => role.role === account.role)!;
  return {
    username: account.username,
    displayName: account.displayName,
    role: account.role,
    disabled: account.disabled,
    rev: account.rev,
    createdAt: account.createdAt,
    avatar: '',
    bio: '',
    email: '',
    passwordChangeRequired: false,
    permissions: permissions[account.role],
    menuIds: settings.menuIds,
    menuRev: settings.rev,
  };
}
const sessions = new Map<string, string>([['lober-local-preview', 'operator']]);
const expiry = new Map<string, number>();
export function loginPreview(username: unknown, password: unknown) {
  const account =
    typeof username === 'string' ? accounts.get(username) : undefined;
  if (!account || account.disabled || password !== account.password) return;
  const token = crypto.randomUUID();
  const expiresAt = now() + 86_400;
  sessions.set(token, account.username);
  expiry.set(token, expiresAt);
  return { token, expiresAt, user: view(account) };
}
export function previewUser(token: string) {
  if ((expiry.get(token) ?? Infinity) <= now()) {
    logoutPreview(token);
    return;
  }
  const username = sessions.get(token);
  const account = username ? accounts.get(username) : undefined;
  if (account && !account.disabled) return view(account);
}
export function logoutPreview(token: string) {
  sessions.delete(token);
  expiry.delete(token);
}
// 账号清单不返回菜单配置，实际菜单需从角色配置读取。
export const previewUsers = () =>
  [...accounts.values()].map((account) => ({
    ...view(account),
    menuIds: [],
    menuRev: 0,
  }));
export const previewMenus = () => ({ menus, roles });
export function savePreviewMenus(
  role: string,
  input: { menuIds?: unknown; rev?: unknown } | null,
) {
  const config = roles.find((item) => item.role === role);
  if (!config) return { status: 404, message: '角色不存在' } as const;
  if (input?.rev !== config.rev)
    return { status: 409, message: '角色菜单版本已变化' } as const;
  const ids = input?.menuIds;
  if (
    !Array.isArray(ids) ||
    new Set(ids).size !== ids.length ||
    ids.some(
      (id) => typeof id !== 'string' || !config.availableMenuIds.includes(id),
    ) ||
    menus.some(
      (menu) =>
        menu.required &&
        config.availableMenuIds.includes(menu.id!) &&
        !ids.includes(menu.id),
    )
  ) {
    return { status: 422, message: '请检查菜单权限和必选项' } as const;
  }
  config.menuIds = config.availableMenuIds.filter((id) => ids.includes(id));
  config.rev++;
  return { data: config };
}
export function savePreviewAccount(
  username: string | undefined,
  input: Partial<Account> | null,
) {
  const name = username ?? input?.username?.trim();
  if (
    !name ||
    !/^[a-z0-9][a-z0-9_.-]{0,63}$/i.test(name) ||
    !input ||
    !input.role ||
    !Object.hasOwn(permissions, input.role) ||
    typeof input.displayName !== 'string' ||
    typeof input.disabled !== 'boolean' ||
    (!username && !input.password)
  ) {
    return { status: 422, message: '请检查账号信息' } as const;
  }
  const current = accounts.get(name);
  if (!username && current)
    return { status: 409, message: '用户名已存在' } as const;
  if (username && !current)
    return { status: 404, message: '账号不存在' } as const;
  if (
    current &&
    (current.rev !== input.rev ||
      (name === 'admin' && (input.role !== 'admin' || input.disabled)))
  ) {
    return {
      status: 409,
      message: '账号版本已变化，或不能禁用预览管理员',
    } as const;
  }
  const next = {
    username: name,
    displayName: input.displayName,
    role: input.role,
    disabled: input.disabled,
    password: input.password || current?.password || '',
    rev: (current?.rev ?? 0) + 1,
    createdAt: current?.createdAt ?? now(),
  };
  accounts.set(name, next);
  return { data: view(next) };
}
