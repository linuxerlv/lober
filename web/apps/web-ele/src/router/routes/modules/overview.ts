import type { RouteRecordRaw } from 'vue-router';

import { Grid, InspectionPanel, LayoutGrid } from '@vben/icons';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: LayoutGrid,
      order: 1,
      title: '概览',
    },
    name: 'Overview',
    path: '/overview',
    children: [
      {
        name: 'OverviewAnalytics',
        path: 'analytics',
        component: () => import('#/views/overview/analytics.vue'),
        meta: {
          affixTab: true,
          icon: InspectionPanel,
          menuId: 'overview-analytics',
          title: '分析页',
        },
      },
      {
        name: 'OverviewWorkspace',
        path: 'workspace',
        component: () => import('#/views/overview/workspace.vue'),
        meta: {
          icon: Grid,
          menuId: 'overview-workspace',
          title: '工作台',
        },
      },
    ],
  },
];

export default routes;
