import type { RouteRecordRaw } from 'vue-router';

import { SquareCode } from '@vben/icons';

/** 菜单可见性由后端角色配置决定，操作权限由当前会话权限码决定。 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      hideChildrenInMenu: true,
      icon: SquareCode,
      order: 40,
      title: '装机模板',
    },
    name: 'Templates',
    path: '/templates',
    children: [
      {
        name: 'TemplatesIndex',
        path: '',
        component: () => import('#/views/templates/index.vue'),
        meta: {
          title: '模板',
          menuId: 'templates',
        },
      },
    ],
  },
];

export default routes;
