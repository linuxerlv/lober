<script setup lang="ts">
import { computed } from 'vue';
import { useRouter } from 'vue-router';

import { useUserStore } from '@vben/stores';

import { ElButton, ElTag } from 'element-plus';

import { roleLabels } from '#/api/console/accounts';
import { listAudit } from '#/api/console/audit';
import { getDiscoveryListApi } from '#/api/console/discovery';
import { getHookListApi } from '#/api/console/hook';
import { taskAPI } from '#/api/console/task';
import { getTemplateListApi } from '#/api/console/template';
import { useConsoleAccess } from '#/composables/use-console-access';
import { useAuthStore } from '#/store';
import { taskStateLabels } from '#/views/tasks/task-state';

import { useOverviewData } from './use-overview-data';

defineOptions({ name: 'OverviewWorkspace' });

const router = useRouter();
const userStore = useUserStore();
const authStore = useAuthStore();
const { can, hasMenu } = useConsoleAccess();
const recentTasks = useOverviewData(() => can('task:read'), () => taskAPI.list({ page: 1, page_size: 20 }));
const discoveries = useOverviewData(() => can('host:read'), () => getDiscoveryListApi());
const audits = useOverviewData(() => hasMenu('reports'), () => listAudit({ page: 1, page_size: 6 }));
const reviewQuery = { state: 'review', page: 1, page_size: 1 } as const;
const templates = useOverviewData(() => can('template:read'), () => getTemplateListApi(reviewQuery));
const hooks = useOverviewData(() => can('hook:read'), () => getHookListApi(reviewQuery));
const pendingReviewCount = computed(() => (templates.value?.total ?? 0) + (hooks.value?.total ?? 0));
const tasks = computed(() => recentTasks.value?.items ?? []);

const failedTasks = computed(() =>
  tasks.value.filter((t) => t.state === 'failed'),
);
const activeTasks = computed(() =>
  tasks.value.filter((t) =>
    ['booting', 'initializing', 'installing', 'pending', 'rebooting'].includes(
      t.state,
    ),
  ),
);
const unregisteredDiscoveries = computed(() =>
  discoveries.value?.items.filter((d) => !d.registered_sn) ?? [],
);

const shortcuts = computed(() => [
  {
    title: '批量创建装机',
    desc: '选主机、镜像与模板一键下发',
    path: '/tasks/new',
    color: 'text-blue-500 bg-blue-500/10',
    show: hasMenu('tasks') && can('task:create'),
  },
  {
    title: '装机任务监控',
    desc: '查看实时流水线与排障日志',
    path: '/tasks',
    color: 'text-emerald-500 bg-emerald-500/10',
    show: hasMenu('tasks'),
  },
  {
    title: '主机资产与发现',
    desc: '管理已登记主机与硬件采集',
    path: '/hosts',
    color: 'text-amber-500 bg-amber-500/10',
    show: hasMenu('hosts'),
  },
  {
    title: '系统镜像库',
    desc: '导入 Linux / ESXi / Windows 镜像',
    path: '/artifact-sets',
    color: 'text-purple-500 bg-purple-500/10',
    show: hasMenu('isos'),
  },
  {
    title: '装机模板编排',
    desc: '编辑应答模板与插槽装配',
    path: '/templates',
    color: 'text-cyan-500 bg-cyan-500/10',
    show: hasMenu('templates'),
  },
  {
    title: '脚本组件管理',
    desc: '维护网络、磁盘与安全加固脚本',
    path: '/hooks',
    color: 'text-rose-500 bg-rose-500/10',
    show: hasMenu('hooks'),
  },
].filter((item) => item.show));
</script>

<template>
  <div class="p-5">
    <!-- 顶部欢迎与值班状态卡片 -->
    <div
      class="flex flex-wrap items-center justify-between gap-4 rounded-xl border border-border bg-card p-6"
    >
      <div class="flex items-center gap-4">
        <img
          :src="userStore.userInfo?.avatar || '/avatar.svg'"
          alt="头像"
          class="size-14 rounded-full border border-border bg-muted p-1"
        />
        <div>
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="text-lg font-semibold text-foreground">
              早安，{{
                userStore.userInfo?.realName ||
                authStore.sessionUser?.username ||
                '运维工程师'
              }}，祝你今天装机顺利！
            </h2>
            <ElTag v-if="authStore.sessionUser" size="small" type="success">
              {{ roleLabels[authStore.sessionUser.role] }}
            </ElTag>
          </div>
          <p class="mt-1 text-sm text-muted-foreground">
            当前共有 {{ activeTasks.length }} 台机器正在执行装机流程，{{
              failedTasks.length
            }}
            台异常待排障。<span v-if="can('host:read')">{{ unregisteredDiscoveries.length }} 台新上报物理机待登记。</span>
          </p>
        </div>
      </div>
      <div class="flex items-center gap-6 text-right">
        <div>
          <div class="text-xs text-muted-foreground">活跃任务</div>
          <div class="mt-1 text-xl font-bold tabular-nums">
            {{ activeTasks.length }}
          </div>
        </div>
        <div v-if="can('host:read')">
          <div class="text-xs text-muted-foreground">待登记主机</div>
          <div class="mt-1 text-xl font-bold tabular-nums text-amber-500">
            {{ unregisteredDiscoveries.length }}
          </div>
        </div>
        <div v-if="can('template:read') || can('hook:read')">
          <div class="text-xs text-muted-foreground">待审批组件</div>
          <div class="mt-1 text-xl font-bold tabular-nums text-blue-500">
            {{ pendingReviewCount }}
          </div>
        </div>
      </div>
    </div>

    <div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
      <!-- 左侧两列：待处理异常 & 正在执行的装机任务 -->
      <div class="grid gap-4 lg:col-span-2">
        <!-- 待办与异常提醒 -->
        <div class="rounded-xl border border-border bg-card p-5">
          <div class="mb-4 flex items-center justify-between">
            <h3 class="text-base font-semibold text-foreground">
              待办与异常速办
            </h3>
            <span class="text-xs text-muted-foreground">
              集中汇总需人工介入的装机、发现与审批事项
            </span>
          </div>
          <div class="grid gap-3">
            <div
              v-for="task in failedTasks"
              :key="task.id"
              class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-500/30 bg-red-500/5 px-4 py-3"
            >
              <div>
                <div class="flex items-center gap-2">
                  <ElTag size="small" type="danger">装机失败</ElTag>
                  <strong class="font-mono text-sm">{{ task.hostSN }}</strong>
                  <span class="text-xs text-muted-foreground">
                    {{ task.snapshot?.iso?.name }} ·
                    {{ task.snapshot?.template?.name }}
                  </span>
                </div>
                <p class="mt-1 text-xs text-muted-foreground">
                  任务编号 {{ task.id }}
                </p>
              </div>
              <ElButton
                v-if="hasMenu('tasks')"
                size="small"
                type="danger"
                plain
                @click="router.push(`/tasks/${encodeURIComponent(task.id)}`)"
              >
                去排障
              </ElButton>
            </div>

            <div
              v-for="disc in unregisteredDiscoveries"
              :key="disc.mac"
              class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-500/30 bg-amber-500/5 px-4 py-3"
            >
              <div>
                <div class="flex items-center gap-2">
                  <ElTag size="small" type="warning">新发现物理机</ElTag>
                  <strong class="font-mono text-sm">{{
                    disc.sn || disc.mac
                  }}</strong>
                  <span class="text-xs text-muted-foreground">
                    {{ disc.system_model || '未知型号' }} · {{ disc.arch }} /
                    {{ disc.firmware.toUpperCase() }}
                  </span>
                </div>
                <p class="mt-1 text-xs text-muted-foreground">
                  启动网口 {{ disc.mac }} · 已上报硬件部件清单，核对序列号后可登记装机
                </p>
              </div>
              <ElButton
                v-if="hasMenu('hosts')"
                size="small"
                type="warning"
                plain
                @click="router.push('/hosts')"
              >
                {{ can('host:write') ? '去主机清单登记' : '查看主机清单' }}
              </ElButton>
            </div>

            <div
              v-if="pendingReviewCount > 0"
              class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-blue-500/30 bg-blue-500/5 px-4 py-3"
            >
              <div>
                <div class="flex items-center gap-2">
                  <ElTag size="small" type="primary">待审批</ElTag>
                  <strong class="text-sm">
                    有 {{ pendingReviewCount }} 个装机模板或脚本组件等待审批
                  </strong>
                </div>
                <p class="mt-1 text-xs text-muted-foreground">
                  审批通过后即可在创建装机任务向导中选用
                </p>
              </div>
              <div class="flex gap-2">
                <ElButton
                  v-if="hasMenu('templates')"
                  size="small"
                  type="primary"
                  plain
                  @click="router.push('/templates')"
                >
                  查看模板
                </ElButton>
                <ElButton
                  v-if="hasMenu('hooks')"
                  size="small"
                  plain
                  @click="router.push('/hooks')"
                >
                  查看脚本
                </ElButton>
              </div>
            </div>
          </div>
        </div>

        <!-- 最新装机任务进度 -->
        <div class="rounded-xl border border-border bg-card p-5">
          <div class="mb-4 flex items-center justify-between">
            <h3 class="text-base font-semibold text-foreground">
              活跃装机任务
            </h3>
            <ElButton v-if="hasMenu('tasks')" link type="primary" @click="router.push('/tasks')">
              查看全部任务 →
            </ElButton>
          </div>
          <div class="divide-y divide-border">
            <div
              v-for="task in tasks.slice(0, 5)"
              :key="task.id"
              class="flex flex-wrap items-center justify-between gap-3 py-3"
            >
              <div>
                <div class="flex items-center gap-2">
                  <strong class="font-mono text-sm">{{ task.hostSN }}</strong>
                  <ElTag
                    size="small"
                    :type="
                      task.state === 'failed'
                        ? 'danger'
                        : task.state === 'done'
                          ? 'success'
                          : task.state === 'pending'
                            ? 'warning'
                            : 'primary'
                    "
                  >
                    {{ taskStateLabels[task.state] }}
                  </ElTag>
                </div>
                <div class="mt-1 text-xs text-muted-foreground">
                  {{ task.snapshot?.iso?.name }} ·
                  {{ task.snapshot?.template?.name }} v{{ task.templateVersion }}
                </div>
              </div>
              <ElButton
                v-if="hasMenu('tasks')"
                link
                type="primary"
                @click="router.push(`/tasks/${encodeURIComponent(task.id)}`)"
              >
                详情
              </ElButton>
            </div>
          </div>
        </div>
      </div>

      <!-- 右侧一列：快捷导航 & 最近审计动态 -->
      <div class="grid content-start gap-4">
        <div v-if="shortcuts.length" class="rounded-xl border border-border bg-card p-5">
          <h3 class="mb-4 text-base font-semibold text-foreground">
            快捷导航
          </h3>
          <div class="grid grid-cols-2 gap-3">
            <button
              v-for="nav in shortcuts"
              :key="nav.title"
              type="button"
              class="rounded-xl border border-border p-3.5 text-left transition hover:border-primary/50 hover:bg-muted/40"
              @click="router.push(nav.path)"
            >
              <div class="text-sm font-semibold text-foreground">
                {{ nav.title }}
              </div>
              <div class="mt-1 text-xs text-muted-foreground">
                {{ nav.desc }}
              </div>
            </button>
          </div>
        </div>

        <div v-if="hasMenu('reports')" class="rounded-xl border border-border bg-card p-5">
          <div class="mb-4 flex items-center justify-between">
            <h3 class="text-base font-semibold text-foreground">
              最近操作动态
            </h3>
            <ElButton link type="primary" @click="router.push('/reports')">
              更多 →
            </ElButton>
          </div>
          <div class="divide-y divide-border">
            <div
              v-for="item in audits?.items"
              :key="item.id"
              class="py-2.5 text-xs"
            >
              <div class="flex items-center justify-between text-muted-foreground">
                <span class="font-medium text-foreground">{{ item.actor }}</span>
                <span>{{ new Date(item.at * 1000).toLocaleTimeString() }}</span>
              </div>
              <p class="mt-1 text-muted-foreground">{{ item.detail }}</p>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
