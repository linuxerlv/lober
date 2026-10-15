import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    name: 'Users',
    path: '/users',
    component: () => import('#/views/users/index.vue'),
    meta: { title: '用户管理', order: 60, authority: ['admin'] },
  },
  {
    name: 'Audit',
    path: '/audit',
    component: () => import('#/views/audit/index.vue'),
    meta: { title: '审计', order: 70, authority: ['admin'] },
  },
  {
    name: 'AccountPassword',
    path: '/account/password',
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
