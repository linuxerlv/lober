import type { RouteRecordRaw } from 'vue-router';

import { List } from '@vben/icons';

/**
 * 任务路由（SPEC §12.6）。任务列表是主战场（app.defaultHomePath = /tasks）。
 *
 * 角色可见性（SPEC §12.5 角色表）：viewer / operator / admin 三个角色可见。
 * hook-author / hook-approver 未列在可见路由里，不放宽（§12.6「就这些，不
 * 扩张」同样适用于角色可见范围，不只是页面数量）。
 *
 * task:arm 权限码不存在（SPEC 0.4 删掉了 armed 状态），本路由树不出现
 * arm/disarm 相关的任何入口——这不是遗漏，是设计上刻意不做。
 *
 * 不显式设置顶层 component：generateAccessible 对含 children 的顶层路由会
 * 删除其 component（避免多层 BasicLayout），实际渲染内容由 path: '' 的
 * TasksIndex 子路由承担（同一地址下"列表即父路由自身"的既定模式，见
 * packages/effects/access/src/accessible.ts 的 fork 定制注释）。
 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: List,
      order: 10,
      title: '任务',
      authority: ['viewer', 'operator', 'admin'],
    },
    name: 'Tasks',
    path: '/tasks',
    children: [
      {
        name: 'TasksIndex',
        path: '',
        component: () => import('#/views/tasks/index.vue'),
        meta: {
          title: '任务列表',
          authority: ['viewer', 'operator', 'admin'],
        },
      },
      {
        name: 'TasksNew',
        path: 'new',
        component: () => import('#/views/tasks/new.vue'),
        meta: {
          // 建任务是唯一的授权动作（SPEC §3.2、§12.5），viewer 的角色定义是
          // “全部只读”（SPEC §12.5 角色表），因此这里按角色整页拦截而不是
          // 只做按钮级权限码禁用——viewer 本就不该到达这个页面，不是“看得见
          // 但做不了”，是“压根不该看见入口”。task:create 权限码仍会在页面
          // 内的提交按钮上再校验一层，作为 operator/admin 之间更细的区分
          // （如需要）。
          hideInMenu: true,
          title: '建任务',
          authority: ['operator', 'admin'],
        },
      },
      {
        name: 'TasksDetail',
        path: ':id',
        component: () => import('#/views/tasks/detail.vue'),
        meta: {
          hideInMenu: true,
          title: '任务详情',
          authority: ['viewer', 'operator', 'admin'],
        },
      },
    ],
  },
];

export default routes;
