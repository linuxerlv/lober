import type { RouteRecordRaw } from 'vue-router';

import { Inbox } from '@vben/icons';

/** 菜单可见性由后端角色配置决定，操作权限由当前会话权限码决定。 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      hideChildrenInMenu: true,
      icon: Inbox,
      order: 30,
      title: '系统镜像',
    },
    name: 'ArtifactSets',
    path: '/artifact-sets',
    children: [
      {
        name: 'ArtifactSetsIndex',
        path: '',
        component: () => import('#/views/artifact-sets/index.vue'),
        meta: {
          title: '系统镜像',
          menuId: 'isos',
        },
      },
    ],
  },
];

export default routes;
