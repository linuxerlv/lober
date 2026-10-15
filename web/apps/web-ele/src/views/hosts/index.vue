<script lang="ts" setup>
import type { AttributeRow } from './inventory-state';

import type { HostListItem } from '#/api/console/host';

import { computed, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';
import { useAccessStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCheckbox,
  ElDialog,
  ElInput,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElTable,
  ElTableColumn,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  commitHostCSVApi,
  createHostApi,
  deleteHostApi,
  getHostListApi,
  previewHostCSVApi,
  updateHostApi,
} from '#/api/console/host';
import { errorMessage, problemsOf, referencesOf } from '#/api/console/problem';
import ProblemList from '#/components/console/ProblemList.vue';

import { attributesOf, CsvImportSession, CursorPage } from './inventory-state';

defineOptions({ name: 'HostsIndex' });
const access = useAccessStore();
const canWrite = computed(() => access.accessCodes.includes('host:write'));
const failure = ref<unknown>();
const problems = computed(() => problemsOf(failure.value));
const references = computed(() => referencesOf(failure.value));
const filters = reactive({ prefix: '', attr_key: '', attr_value: '' });
const page = reactive(new CursorPage<HostListItem>());
const [Grid, gridApi] = useVbenVxeGrid<HostListItem>({
  gridOptions: {
    columns: [
      { field: 'sn', title: 'SN', minWidth: 180 },
      { field: 'arch', title: '架构', width: 100 },
      { field: 'firmware', title: '固件', width: 100 },
      {
        field: 'attrs',
        title: '登记属性',
        minWidth: 280,
        slots: { default: 'attrs' },
      },
      { field: 'rev', title: '修订', width: 80 },
      { title: '操作', width: 160, slots: { default: 'action' } },
    ],
    height: 'auto',
    pagerConfig: { enabled: false },
    rowConfig: { keyField: 'sn' },
    proxyConfig: {
      ajax: {
        query: async () => {
          try {
            return await page.load((cursor) =>
              getHostListApi({ ...filters, cursor, limit: 50 }),
            );
          } catch (error) {
            failure.value = error;
            return { items: page.items };
          }
        },
      },
    },
    toolbarConfig: { refresh: true },
  },
});
function search() {
  failure.value = undefined;
  page.reset();
  void gridApi.reload();
}
function nextPage() {
  page.advance();
  void gridApi.reload();
}
function previousPage() {
  page.back();
  void gridApi.reload();
}
const editorOpen = ref(false);
const saving = ref(false);
const editingSN = ref<string>();
const form = reactive<{
  sn: string;
  arch: '' | HostListItem['arch'];
  firmware: '' | HostListItem['firmware'];
  attrs: Record<string, string>;
  rev: number;
}>({ sn: '', arch: '', firmware: '', attrs: {}, rev: 0 });
const attrs = ref<AttributeRow[]>([]);
function openHost(host?: HostListItem) {
  failure.value = undefined;
  editingSN.value = host?.sn;
  Object.assign(form, {
    sn: host?.sn ?? '',
    arch: host?.arch ?? '',
    firmware: host?.firmware ?? '',
    attrs: {},
    rev: host?.rev ?? 0,
  });
  attrs.value = Object.entries(host?.attrs ?? {}).map(([key, value]) => ({
    key,
    value,
  }));
  editorOpen.value = true;
}
async function saveHost() {
  if (!canWrite.value || saving.value) return;
  failure.value = undefined;
  saving.value = true;
  try {
    const data = {
      sn: form.sn,
      rev: form.rev,
      ...(form.arch ? { arch: form.arch } : {}),
      ...(form.firmware ? { firmware: form.firmware } : {}),
      attrs: attributesOf(attrs.value),
    };
    if (editingSN.value) await updateHostApi(editingSN.value, data);
    else await createHostApi(data);
    editorOpen.value = false;
    ElMessage.success('主机已保存');
    search();
  } catch (error) {
    failure.value = error;
  } finally {
    saving.value = false;
  }
}
async function removeHost(host: HostListItem) {
  try {
    await ElMessageBox.confirm(`确认删除主机 ${host.sn}？`, '删除主机', {
      type: 'warning',
    });
  } catch {
    return;
  }
  try {
    await deleteHostApi(host.sn, host.rev);
    search();
  } catch (error) {
    failure.value = error;
  }
}
const csvOpen = ref(false);
const csvText = ref('');
const csvName = ref('');
const csvBusy = ref(false);
const confirmed = ref(false);
const csv = reactive(new CsvImportSession());
function openCSV() {
  csvText.value = '';
  csvName.value = '';
  csv.select(new Uint8Array());
  confirmed.value = false;
  failure.value = undefined;
  csvOpen.value = true;
}
function changeCSVText(value: string) {
  csvName.value = '';
  csv.select(new TextEncoder().encode(value));
  confirmed.value = false;
}
async function selectCSV(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;
  csvText.value = '';
  csvName.value = file.name;
  csv.select(new Uint8Array(await file.arrayBuffer()));
  confirmed.value = false;
}
async function previewCSV() {
  csvBusy.value = true;
  failure.value = undefined;
  confirmed.value = false;
  try {
    await csv.preview(previewHostCSVApi);
  } catch (error) {
    failure.value = error;
  } finally {
    csvBusy.value = false;
  }
}
async function commitCSV() {
  csvBusy.value = true;
  failure.value = undefined;
  try {
    await csv.commit(commitHostCSVApi, confirmed.value);
    csvOpen.value = false;
    ElMessage.success('CSV 已整体导入');
    search();
  } catch (error) {
    failure.value = error;
  } finally {
    csvBusy.value = false;
  }
}
</script>

<template>
  <Page
    title="主机清单"
    description="登记主机 SN、可选架构固件和任意属性；建任务时从这里选择主机。"
  >
    <template #extra>
      <ElButton v-if="canWrite" @click="openCSV">CSV 导入</ElButton>
      <ElButton v-if="canWrite" type="primary" @click="openHost()">
        登记主机
      </ElButton>
    </template>
    <ElAlert
      v-if="failure && !editorOpen && !csvOpen"
      :title="errorMessage(failure)"
      type="error"
      :closable="false"
      class="mb-3"
    />
    <ProblemList
      v-if="!editorOpen && !csvOpen"
      :problems="problems"
      :references="references"
    />
    <form
      class="mb-4 flex flex-wrap items-center gap-3"
      @submit.prevent="search"
    >
      <ElInput
        v-model="filters.prefix"
        aria-label="SN 前缀"
        placeholder="SN 前缀"
        class="!w-52"
        clearable
      />
      <ElInput
        v-model="filters.attr_key"
        aria-label="属性名"
        placeholder="属性名，例如 rack"
        class="!w-44"
        clearable
      />
      <ElInput
        v-model="filters.attr_value"
        aria-label="属性值"
        placeholder="属性值"
        class="!w-52"
        clearable
      />
      <ElButton native-type="submit" type="primary">查询</ElButton>
    </form>
    <Grid>
      <template #attrs="{ row }">
        <span
          v-for="(value, key) in row.attrs"
          :key="key"
          class="mr-4 inline-block"
        >
          {{ key }}：{{ value }}
        </span>
      </template>
      <template #action="{ row }">
        <ElButton v-if="canWrite" link @click="openHost(row)">编辑</ElButton>
        <ElButton v-if="canWrite" link type="danger" @click="removeHost(row)">
          删除
        </ElButton>
      </template>
    </Grid>
    <div class="mt-3 flex justify-end gap-3">
      <ElButton :disabled="!page.history.length" @click="previousPage">
        上一页
      </ElButton>
      <ElButton :disabled="!page.next" @click="nextPage">下一页</ElButton>
    </div>
    <ElDialog
      v-model="editorOpen"
      :title="editingSN ? '编辑主机' : '登记主机'"
      width="640px"
      :close-on-click-modal="false"
    >
      <ElAlert
        v-if="failure"
        :title="errorMessage(failure)"
        type="error"
        :closable="false"
        class="mb-3"
      />
      <ProblemList :problems="problems" :references="references" />
      <form class="grid gap-4" @submit.prevent="saveHost">
        <label>
          SN（必填）
          <ElInput
            v-model="form.sn"
            aria-label="主机 SN"
            :disabled="!!editingSN || saving"
          />
        </label>
        <div class="grid grid-cols-2 gap-4">
          <label>
            架构（可选）
            <ElSelect
              v-model="form.arch"
              aria-label="主机架构"
              clearable
              class="w-full"
            >
              <ElOption label="x86_64" value="x86_64" />
              <ElOption label="aarch64" value="aarch64" />
            </ElSelect>
          </label>
          <label>
            固件（可选）
            <ElSelect
              v-model="form.firmware"
              aria-label="主机固件"
              clearable
              class="w-full"
            >
              <ElOption label="UEFI" value="uefi" />
              <ElOption label="BIOS" value="bios" />
            </ElSelect>
          </label>
        </div>
        <div>
          <div class="mb-2 flex items-center justify-between">
            <span>登记属性（例如 ip、mac、rack）</span>
            <ElButton
              :disabled="saving"
              @click="attrs.push({ key: '', value: '' })"
            >
              添加属性
            </ElButton>
          </div>
          <div
            v-for="(attr, index) in attrs"
            :key="index"
            class="mb-2 flex gap-2"
          >
            <ElInput
              v-model="attr.key"
              :aria-label="`属性名 ${index + 1}`"
              placeholder="属性名"
              :disabled="saving"
            />
            <ElInput
              v-model="attr.value"
              :aria-label="`属性值 ${index + 1}`"
              placeholder="属性值"
              :disabled="saving"
            />
            <ElButton :disabled="saving" @click="attrs.splice(index, 1)">
              移除
            </ElButton>
          </div>
        </div>
      </form>
      <template #footer>
        <ElButton :disabled="saving" @click="editorOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="saving" @click="saveHost">
          保存主机
        </ElButton>
      </template>
    </ElDialog>
    <ElDialog
      v-model="csvOpen"
      title="CSV 导入：预览后确认"
      width="800px"
      :close-on-click-modal="false"
    >
      <ElAlert
        v-if="failure"
        :title="errorMessage(failure)"
        type="error"
        :closable="false"
        class="mb-3"
      />
      <ProblemList :problems="problems" :references="references" />
      <p class="mb-3">
        首行使用 sn、arch、firmware 或属性名。已登记 SN
        将更新，任何错误都会阻止整批提交。
      </p>
      <label class="mb-3 block">
        选择 CSV 文件
        <input
          type="file"
          accept=".csv,text/csv"
          :disabled="csvBusy"
          aria-label="CSV 文件"
          @change="selectCSV"
        />
      </label>
      <p v-if="csvName" class="mb-2">当前文件：{{ csvName }}</p>
      <ElInput
        v-model="csvText"
        type="textarea"
        :rows="5"
        :disabled="csvBusy"
        aria-label="CSV 文本"
        placeholder="sn,arch,firmware,rack&#10;SN001,,,R1"
        @input="changeCSVText"
      />
      <ElButton class="my-3" :loading="csvBusy" @click="previewCSV">
        预览 CSV（不写入）
      </ElButton>
      <ElTable v-if="csv.plan" :data="csv.plan.rows ?? []" max-height="320">
        <ElTableColumn prop="row" label="行号" width="70" />
        <ElTableColumn prop="sn" label="SN" />
        <ElTableColumn prop="action" label="处理" width="100" />
        <ElTableColumn label="逐行诊断" min-width="260">
          <template #default="{ row }">
            <ProblemList :problems="row.problems ?? []" />
          </template>
        </ElTableColumn>
      </ElTable>
      <ElCheckbox
        v-if="csv.plan"
        v-model="confirmed"
        class="mt-3"
        :disabled="
          csvBusy || (csv.plan.rows ?? []).some((row) => row.action === 'error')
        "
      >
        已核对 {{ csv.plan.total }} 行，确认整体导入本次预览的 CSV
      </ElCheckbox>
      <template #footer>
        <ElButton :disabled="csvBusy" @click="csvOpen = false">关闭</ElButton>
        <ElButton
          type="primary"
          :disabled="!confirmed || !csv.plan"
          :loading="csvBusy"
          @click="commitCSV"
        >
          确认导入
        </ElButton>
      </template>
    </ElDialog>
  </Page>
</template>
