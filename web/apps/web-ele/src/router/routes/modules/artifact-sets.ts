import type { RouteRecordRaw } from 'vue-router';

import { Inbox } from '@vben/icons';

/** 菜单可见性由后端角色配置决定，操作权限由当前会话权限码决定。 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: Inbox,
      order: 50,
      title: '制品集',
    },
    name: 'ArtifactSets',
    path: '/artifact-sets',
    children: [
      {
        name: 'ArtifactSetsIndex',
        path: '',
        component: () => import('#/views/artifact-sets/index.vue'),
        meta: {
          title: '制品集导入',
          menuId: 'isos',
        },
      },
    ],
  },
];

export default routes;
