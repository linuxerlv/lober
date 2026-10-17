import type { RouteRecordRaw } from 'vue-router';

import { LayoutGrid } from '@vben/icons';

/** 菜单可见性由后端角色配置决定，操作权限由当前会话权限码决定。 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: LayoutGrid,
      order: 20,
      title: '主机清单',
      menuId: 'hosts',
    },
    name: 'Hosts',
    path: '/hosts',
    children: [
      {
        name: 'HostsIndex',
        path: '',
        component: () => import('#/views/hosts/index.vue'),
        meta: {
          title: '主机清单',
          menuId: 'hosts',
        },
      },
    ],
  },
];

export default routes;
