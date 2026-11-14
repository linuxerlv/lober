<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';

import { onBeforeUnmount, reactive, ref, watch } from 'vue';

import { ElAlert, ElButton, ElTable, ElTableColumn } from 'element-plus';

import { listAudit } from '#/api/console/audit';
import { errorMessage } from '#/api/console/problem';
import { NumberedPage } from '#/composables/numbered-page';

import ListPagination from './ListPagination.vue';

const props = defineProps<{
  targetId: string;
  targetKind: 'iso' | 'task' | 'user';
}>();
const page = reactive(new NumberedPage<Schemas['AuditItem']>());
const error = ref('');
let request = 0;

const labels: Record<string, string> = {
  'iso.cancel': '取消获取',
  'iso.create': '登记镜像',
  'iso.delete': '删除镜像',
  'iso.deprecate': '弃用镜像',
  'iso.reimport': '重新导入镜像',
  'iso.retry': '重试获取',
  'iso.source.register': '登记镜像来源',
  'task.cancel': '取消任务',
  'task.create': '创建任务',
  'task.retry': '重试任务',
  'user.create': '创建账号',
  'user.disable': '停用账号',
  'user.password.change': '修改密码',
  'user.profile.update': '更新个人资料',
  'user.update': '编辑账号',
};

async function load() {
  const current = ++request;
  error.value = '';
  const { targetId, targetKind } = props;
  try {
    await page.load((params) =>
      listAudit({ ...params, target_id: targetId, target_kind: targetKind }),
    );
  } catch (reason) {
    if (current === request) error.value = errorMessage(reason);
  }
}

function changePage(next: number) {
  page.setPage(next);
  void load();
}
function changeSize(size: number) {
  page.setPageSize(size);
  void load();
}

watch(
  () => [props.targetKind, props.targetId],
  () => {
    page.reset();
    page.items = [];
    page.total = 0;
    void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  request++;
  page.reset();
});
</script>

<template>
  <section aria-label="变更记录">
    <div class="mb-3 flex items-center justify-between gap-2">
      <h2 class="font-semibold">变更记录</h2>
      <ElButton :loading="page.loading" @click="load">刷新记录</ElButton>
    </div>
    <ElAlert
      v-if="error"
      :title="error"
      type="error"
      :closable="false"
      class="mb-3"
    />
    <ElTable
      v-if="!error || page.items.length"
      v-loading="page.loading"
      :data="page.items"
      :empty-text="page.loading ? '正在读取记录…' : '暂无变更记录'"
      class="[--el-table-header-bg-color:var(--el-fill-color-light)]"
      row-key="id"
    >
      <ElTableColumn label="时间" width="165">
        <template #default="{ row }">
{{
          new Date(row.at * 1000).toLocaleString()
        }}
</template>
      </ElTableColumn>
      <ElTableColumn label="操作人" prop="actor" min-width="100" />
      <ElTableColumn label="操作" min-width="125">
        <template #default="{ row }">
{{
          labels[row.action] ?? row.action
        }}
</template>
      </ElTableColumn>
      <ElTableColumn label="说明" min-width="135">
        <template #default="{ row }">
<span class="break-all">{{ row.detail || '—' }}</span>
</template>
      </ElTableColumn>
    </ElTable>
    <ListPagination
      v-if="page.total > 0"
      :current-page="page.page"
      :page-size="page.pageSize"
      :page-count="page.items.length"
      :total="page.total"
      :disabled="page.loading"
      @page-change="changePage"
      @size-change="changeSize"
    />
  </section>
</template>
