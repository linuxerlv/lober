import type { RouteRecordRaw } from 'vue-router';

import { Settings } from '@vben/icons';

/** 菜单可见性由后端角色配置决定，操作权限由当前会话权限码决定。 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: Settings,
      order: 60,
      title: '工具',
      menuId: 'tools',
    },
    name: 'Tools',
    path: '/tools',
    children: [
      {
        name: 'ToolsDiskDryrun',
        path: 'disk-dryrun',
        component: () => import('#/views/tools/disk-dryrun.vue'),
        meta: {
          title: '磁盘策略 dry-run',
          menuId: 'tools',
        },
      },
    ],
  },
];

export default routes;
