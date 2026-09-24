import type { RouteRecordRaw } from 'vue-router';

import { BookOpenText } from '@vben/icons';

/**
 * 报表与审计路由（P4-04 任务 5，端点 30-37）。
 *
 * **只给 admin。** 审计记录含全体操作人的动作明细，报表含全局装机数据，两者
 * 都不该对 operator 可见。P4-04 任务 1 讨论过引入「审计角色」，但 SPEC §9.4
 * 没有定义它，新增角色是契约变更——先只给管理员，等真有专职审计人员时再议。
 *
 * 与其它页面的定位差异：这一页不参与日常装机流程，它是**事故之后**用的。
 * §3.2 承认「建任务即授权」的残留风险没有被消除只是被缓解，选错 SN 的后果是
 * 一台生产机被擦盘（§10 失败模式 2b），所以事故迟早会发生一次；那时要回答
 * 「发生了什么」与「怎么防止再次发生」，靠的分别是任务溯源与失败分布报表。
 *
 * order 放在工具（60）之后：日常用不到，不该挤在前面抢注意力。
 */
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
      authority: ['admin'],
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
          authority: ['admin'],
        },
      },
    ],
  },
];

export default routes;
