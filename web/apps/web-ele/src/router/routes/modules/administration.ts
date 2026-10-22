import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'Users',
    path: '/users',
    redirect: '/access/users',
    meta: {
      title: '用户管理',
      hideInMenu: true,
      hideInTab: true,
      menuId: 'users',
      permission: 'user:manage',
    },
  },
  {
    name: 'Audit',
    path: '/audit',
    redirect: '/reports',
    meta: {
      title: '审计',
      hideInMenu: true,
      hideInTab: true,
      menuId: 'reports',
      permission: 'audit:read',
    },
  },
  {
    name: 'LegacyAccountPassword',
    path: '/account/change-password',
    component: () => import('#/views/account/password.vue'),
    meta: {
      title: '修改密码',
      order: 80,
      authority: [
        'viewer',
        'operator',
        'hook-author',
        'hook-approver',
        'admin',
      ],
    },
  },
];
export default routes;
