<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';
import type { Task } from '#/api/console/task';

import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { RouterLink, useRoute } from 'vue-router';

import { Page } from '@vben/common-ui';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import { errorMessage } from '#/api/console/problem';
import { taskAPI } from '#/api/console/task';
import ListPagination from '#/components/console/ListPagination.vue';
import { NumberedPage } from '#/composables/numbered-page';

import { taskStateLabels } from './task-state';

const route = useRoute();
const batchId = computed(() => String(route.params.id ?? ''));
const batch = ref<Schemas['TaskBatchSummary']>();
const pagination = reactive(new NumberedPage<Task>());
const error = ref('');
const loading = ref(false);
const failedOnly = ref(false);
let request = 0;

const summary = computed(() => [
  { label: '任务总数', value: batch.value?.total },
  { label: '等待引导', value: batch.value?.pending },
  { label: '进行中', value: batch.value?.active },
  { label: '已完成', value: batch.value?.done },
  {
    label: '失败',
    value: batch.value?.failed,
    className: 'text-[var(--el-color-danger)]',
  },
  { label: '已取消', value: batch.value?.cancelled },
]);
async function load() {
  const current = ++request;
  const id = batchId.value;
  const query: Record<string, number | string> = { batch_id: id };
  if (failedOnly.value) query.state = 'failed';
  error.value = '';
  loading.value = true;
  try {
    const result = await taskAPI.batch(id);
    if (current !== request) return;
    batch.value = result;
    await pagination.load((params) =>
      taskAPI.list({ ...query, ...params }),
    );
  } catch (reason) {
    if (current === request) error.value = errorMessage(reason);
  } finally {
    if (current === request) loading.value = false;
  }
}

function setFailedOnly(value: boolean) {
  if (failedOnly.value === value) return;
  failedOnly.value = value;
  pagination.reset();
  pagination.items = [];
  pagination.total = 0;
  void load();
}

function changePage(page: number) {
  pagination.setPage(page);
  void load();
}

function changeSize(size: number) {
  pagination.setPageSize(size);
  void load();
}

watch(
  batchId,
  () => {
    batch.value = undefined;
    failedOnly.value = false;
    pagination.reset();
    pagination.items = [];
    pagination.total = 0;
    void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  request++;
  pagination.reset();
});
</script>

<template>
  <Page title="批次跟踪" :description="`批次编号 ${batchId}`">
    <template #extra>
      <RouterLink class="text-primary" to="/tasks">返回任务列表</RouterLink>
    </template>
    <div class="grid gap-4">
      <ElAlert v-if="error" :title="error" type="error" :closable="false" />
      <ElCard v-if="batch" shadow="never">
        <div
          class="flex flex-wrap gap-x-6 gap-y-2 text-sm text-muted-foreground"
        >
          <span>提交人 {{ batch.createdBy }}</span>
          <span>提交时间 {{ new Date(batch.createdAt * 1000).toLocaleString() }}</span>
          <span>系统镜像 {{ batch.isoName }}</span>
          <span>安装模板 {{ batch.templateName }} · v{{
              batch.templateVersion
            }}</span>
        </div>
        <dl class="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
          <div
            v-for="item in summary"
            :key="item.label"
            class="rounded-lg border border-border px-4 py-3"
          >
            <dt class="text-sm text-muted-foreground">{{ item.label }}</dt>
            <dd
              class="mt-1 text-2xl font-semibold tabular-nums"
              :class="item.className"
            >
              {{ item.value }}
            </dd>
          </div>
        </dl>
      </ElCard>
      <ElCard shadow="never">
        <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div v-if="batch">
            <ElButton
              :type="!failedOnly ? 'primary' : undefined"
              :aria-pressed="!failedOnly"
              @click="setFailedOnly(false)"
              >
全部任务
</ElButton>
            <ElButton
              :type="failedOnly ? 'primary' : undefined"
              :aria-pressed="failedOnly"
              @click="setFailedOnly(true)"
              >
失败任务
</ElButton>
          </div>
          <ElButton class="ml-auto" :loading="loading" @click="load">
刷新状态
</ElButton>
        </div>
        <ElTable
          v-if="!error || batch"
          v-loading="loading"
          :data="pagination.items"
          :empty-text="
            loading
              ? '正在读取批次…'
              : failedOnly
                ? '该批次没有失败任务'
                : '找不到该批次'
          "
          class="[--el-table-header-bg-color:var(--el-fill-color-light)]"
          row-key="id"
        >
          <ElTableColumn label="主机序列号" prop="hostSN" min-width="150" />
          <ElTableColumn label="任务编号" prop="id" min-width="210" />
          <ElTableColumn label="状态" min-width="105">
            <template #default="{ row }">
              <ElTag
                :type="
                  row.state === 'failed'
                    ? 'danger'
                    : row.state === 'done'
                      ? 'success'
                      : 'info'
                "
              >
                {{ taskStateLabels[row.state as Task['state']] }}
              </ElTag>
            </template>
          </ElTableColumn>
          <ElTableColumn label="操作" min-width="115">
            <template #default="{ row }">
              <RouterLink
                class="text-primary"
                :to="{
                  name: 'TasksDetail',
                  params: { id: row.id },
                  query: { fromBatch: batchId },
                }"
              >
                {{ row.state === 'failed' ? '查看失败原因' : '查看任务' }}
              </RouterLink>
            </template>
          </ElTableColumn>
        </ElTable>
        <ListPagination
          v-if="pagination.total"
          :current-page="pagination.page"
          :page-size="pagination.pageSize"
          :page-count="pagination.items.length"
          :total="pagination.total"
          :disabled="loading"
          @page-change="changePage"
          @size-change="changeSize"
        />
      </ElCard>
    </div>
  </Page>
</template>
