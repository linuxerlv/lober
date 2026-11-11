import type { RouteRecordRaw } from 'vue-router';

import { Settings } from '@vben/icons';
const routes: RouteRecordRaw[] = [
  {
    name: 'Access',
    path: '/access',
    meta: {
      title: '权限管理',
      permission: 'user:manage',
      icon: Settings,
      order: 80,
    },
    children: [
      {
        name: 'Accounts',
        path: 'users',
        component: () => import('#/views/access/users.vue'),
        meta: {
          title: '账号管理',
          menuId: 'users',
          permission: 'user:manage',
        },
      },
      {
        name: 'RoleMenus',
        path: 'role-menus',
        component: () => import('#/views/access/role-menus.vue'),
        meta: {
          title: '角色与菜单',
          menuId: 'role-menus',
          permission: 'user:manage',
        },
      },
    ],
  },
];
export default routes;
