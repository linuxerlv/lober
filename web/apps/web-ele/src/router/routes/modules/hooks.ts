import type { RouteRecordRaw } from 'vue-router';

import { InspectionPanel } from '@vben/icons';

/** 菜单可见性由后端角色配置决定，操作权限由当前会话权限码决定。 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: InspectionPanel,
      order: 40,
      title: 'hook',
      menuId: 'hooks',
    },
    name: 'Hooks',
    path: '/hooks',
    children: [
      {
        name: 'HooksIndex',
        path: '',
        component: () => import('#/views/hooks/index.vue'),
        meta: {
          title: 'hook 管理与审批',
          menuId: 'hooks',
        },
      },
    ],
  },
];

export default routes;
