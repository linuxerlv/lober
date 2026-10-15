import type { RouteRecordRaw } from 'vue-router';

import { Inbox } from '@vben/icons';

/**
 * 制品集路由（SPEC §12.6）。ISO 登记与导入进度（异步 job）。
 *
 * 角色可见性（SPEC §12.5 角色表）：仅 viewer / admin——admin 持有
 * artifact:import 权限码，operator/hook-author/hook-approver 的可见路由表
 * 都没有列出 /artifact-sets，不放宽。
 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: Inbox,
      order: 50,
      title: 'ISO',
      authority: [
        'viewer',
        'operator',
        'hook-author',
        'hook-approver',
        'admin',
      ],
    },
    name: 'ArtifactSets',
    path: '/isos',
    children: [
      {
        name: 'ArtifactSetsIndex',
        path: '',
        component: () => import('#/views/artifact-sets/index.vue'),
        meta: {
          title: 'ISO 来源与导入',
          authority: [
            'viewer',
            'operator',
            'hook-author',
            'hook-approver',
            'admin',
          ],
        },
      },
    ],
  },
];

export default routes;
