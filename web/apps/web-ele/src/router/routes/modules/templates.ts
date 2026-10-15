import type { RouteRecordRaw } from 'vue-router';

import { SquareCode } from '@vben/icons';

/**
 * 模板路由（SPEC §12.6）。模板列表、编辑、版本；右侧常驻 preview 面板。
 *
 * 角色可见性（SPEC §12.5 角色表）：仅 viewer / admin——operator 与两个 hook
 * 角色的可见路由表里都没有列出 /templates，不放宽（§12.6「就这些，不扩张」）。
 *
 * preview 不是独立路由：SPEC §12.6 要求它是模板页内的常驻面板，不是二级菜单
 * 入口（与 dryrun 同精神，二者都要求「一等公民」但不是靠新增路由实现的，
 * 是页面内部布局的要求，因此路由树里不出现 /templates/:id/preview）。
 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: SquareCode,
      order: 30,
      title: '模板',
      authority: [
        'viewer',
        'operator',
        'hook-author',
        'hook-approver',
        'admin',
      ],
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
