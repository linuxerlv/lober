import type { RouteRecordRaw } from 'vue-router';

import { LayoutGrid } from '@vben/icons';

/**
 * 主机清单路由（SPEC §12.6）。已发现主机清单，SN 的唯一来源（§4.3）。
 *
 * 角色可见性（SPEC §12.5 角色表）：viewer / operator / admin 三个角色可见，
 * 与 /tasks 相同（operator 的可见路由表里 /hosts 与 /tasks 成对出现）。
 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: LayoutGrid,
      order: 20,
      title: '主机清单',
      authority: [
        'viewer',
        'operator',
        'hook-author',
        'hook-approver',
        'admin',
      ],
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
