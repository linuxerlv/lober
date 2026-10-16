import type { RouteRecordRaw } from 'vue-router';

import { BookOpenText } from '@vben/icons';

/** 菜单可见性由后端角色配置决定，操作权限由当前会话权限码决定。 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      // 图标只能从 @vben/icons 已导出的那张白名单里选（见
      // web/packages/@core/base/icons/src/lucide.ts）——BarChart 不在其中，
      // 而 web/packages/ 下的上游代码按 gin 迁移契约 §8 不改（改了升级会冲突）。
      // BookOpenText 读作「台账」，是白名单里语义最接近报表的一个，且未被其它
      // 路由模块占用。
      icon: BookOpenText,
      order: 70,
      title: '报表与审计',
      menuId: 'reports',
    },
    name: 'Reports',
    path: '/reports',
    children: [
      {
        name: 'ReportsIndex',
        path: '',
        component: () => import('#/views/reports/index.vue'),
        meta: {
          title: '报表与审计',
          menuId: 'reports',
        },
      },
    ],
  },
];

export default routes;
