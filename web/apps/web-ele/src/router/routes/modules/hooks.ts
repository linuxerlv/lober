import type { RouteRecordRaw } from 'vue-router';

import { InspectionPanel } from '@vben/icons';

/**
 * hook 路由（SPEC §12.6）。hook 列表、版本、validate、审批流转。
 *
 * 角色可见性（SPEC §12.5 角色表）：viewer / hook-author / hook-approver /
 * admin 四个角色可见。operator 未列出，不放宽。
 *
 * 按钮级权限码（hook:write / hook:approve）在页面内部用权限码控制，不在
 * 路由层再拆分子路由——同一个列表页里既有作者能做的操作（新建/建新版本）
 * 也有审批人能做的操作（approve/reject），拆成两个路由反而割裂了「一个
 * hook 从草稿到激活」的完整流转视图。
 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: InspectionPanel,
      order: 40,
      title: 'hook',
      authority: [
        'viewer',
        'operator',
        'hook-author',
        'hook-approver',
        'admin',
      ],
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
