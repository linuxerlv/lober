import type { RouteRecordRaw } from 'vue-router';

import { Settings } from '@vben/icons';

/**
 * 工具路由（SPEC §12.6）。磁盘策略 dry-run：贴 facts，出布局。
 *
 * 与 templates 的 preview 同精神：把装机模板调试周期从「上电等十分钟」压到
 * 秒级（SPEC §8.2）。dryrun 本身不落地、不写入任何数据，纯计算，因此不像
 * /templates 或 /artifact-sets 那样限定给 admin——SPEC §12.5 要求这类调试
 * 工具要显眼、做成一等公民，别埋进二级菜单，故仅按角色表的 viewer/admin
 * 「全部」两行给出可见性（与其它非 tasks/hosts 页面保持同一收紧口径，不
 * 单独为它放宽给 operator，避免角色可见范围出现无依据的不一致）。
 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: Settings,
      order: 60,
      title: '工具',
      authority: ['viewer', 'admin'],
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
          authority: ['viewer', 'admin'],
        },
      },
    ],
  },
];

export default routes;
