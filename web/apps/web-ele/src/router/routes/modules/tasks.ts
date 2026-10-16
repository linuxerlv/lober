import type { RouteRecordRaw } from 'vue-router';

import { List } from '@vben/icons';

/** 菜单可见性由后端角色配置决定，操作权限由当前会话权限码决定。 */
const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: List,
      order: 10,
      title: '任务',
      menuId: 'tasks',
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
          menuId: 'tasks',
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
          permission: 'task:create',
          menuId: 'tasks',
        },
      },
      {
        name: 'TasksDetail',
        path: ':id',
        component: () => import('#/views/tasks/detail.vue'),
        meta: {
          hideInMenu: true,
          title: '任务详情',
          menuId: 'tasks',
        },
      },
    ],
  },
];

export default routes;
