<script lang="ts" setup>
import type { AttributeRow } from './inventory-state';

import type { HostListItem } from '#/api/console/host';

import { computed, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { Page } from '@vben/common-ui';
import { useAccessStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCheckbox,
  ElDialog,
  ElDrawer,
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
  getHostApi,
  getHostListApi,
  previewHostCSVApi,
  requestHostInventoryApi,
  updateHostApi,
} from '#/api/console/host';
import { errorMessage, problemsOf, referencesOf } from '#/api/console/problem';
import ListPagination from '#/components/console/ListPagination.vue';
import ProblemList from '#/components/console/ProblemList.vue';
import { downloadSelection } from '#/components/console/selection-export';
import { NumberedPage } from '#/composables/numbered-page';
import { useConsoleAccess } from '#/composables/use-console-access';
import { useRowSelection } from '#/composables/use-row-selection';

import { diskRows, networkRows } from './hardware-details';
import HostTaskHistory from './HostTaskHistory.vue';
import {
  attributesOf,
  CsvImportSession,
  isInventoryControlAttribute,
} from './inventory-state';

defineOptions({ name: 'HostsIndex' });
const access = useAccessStore();
const { hasMenu } = useConsoleAccess();
const canWrite = computed(() => access.accessCodes.includes('host:write'));
const failure = ref<unknown>();
const problems = computed(() => problemsOf(failure.value));
const references = computed(() => referencesOf(failure.value));
const filters = reactive({ prefix: '', attr_key: '', attr_value: '' });
const appliedFilters = reactive({ ...filters });
const page = reactive(new NumberedPage<HostListItem>());
const router = useRouter();
const route = useRoute();
const selection = useRowSelection(
  (host: HostListItem) => host.sn,
  () => page.items,
);
const { rows: selectedHosts, all, partial } = selection;
const detailOpen = ref(false);
const detail = ref<HostListItem>();
const detailLoading = ref(false);
const detailError = ref('');
let detailRun = 0;
const hardwareNames: Record<string, string> = {
  inv_cpu_model: '处理器',
  inv_cpu_cores: '处理器核心数',
  inv_mem_mib: '内存（MiB）',
  inv_system_model: '设备型号',
  inv_system_disk: '系统磁盘',
};
const disks = computed(() => diskRows(detail.value?.attrs?.inv_disks));
const network = computed(() => networkRows(detail.value?.attrs?.inv_nics));
const hardware = computed(() =>
  Object.entries(detail.value?.attrs ?? {}).filter(
    ([key, value]) => Object.hasOwn(hardwareNames, key) && value.trim(),
  ),
);
const registered = computed(() =>
  Object.entries(detail.value?.attrs ?? {}).filter(
    ([key]) =>
      !isInventoryControlAttribute(key) &&
      !Object.hasOwn(hardwareNames, key) &&
      key !== 'inv_disks' &&
      key !== 'inv_nics',
  ),
);
const inventoryStatus = computed(() => {
  const attrs = detail.value?.attrs;
  if (attrs?.inv_in_flight_at) return '正在采集';
  if (attrs?.inv_requested_at || attrs?.inv_request_id)
    return '已请求，等待主机执行';
  const collected = Number(attrs?.inv_collected_at);
  return collected > 0
    ? '最近采集于 ' + new Date(collected * 1000).toLocaleString()
    : '暂无采集记录';
});
async function viewHost(row: HostListItem | string) {
  const run = ++detailRun;
  const sn = typeof row === 'string' ? row : row.sn;
  detail.value = typeof row === 'string' ? undefined : row;
  detailOpen.value = true;
  detailLoading.value = true;
  detailError.value = '';
  try {
    const item = await getHostApi(sn);
    if (run === detailRun) detail.value = item;
  } catch (reason) {
    if (run === detailRun) detailError.value = errorMessage(reason);
  } finally {
    if (run === detailRun) detailLoading.value = false;
  }
}
watch(
  () => (route.path === '/hosts' ? route.query.host : undefined),
  (sn) => {
    if (typeof sn === 'string' && sn) void viewHost(sn);
  },
  { immediate: true },
);
function closeHostDetails() {
  if (route.path === '/hosts' && route.query.host)
    void router.replace('/hosts');
}
function exportHosts() {
  downloadSelection('主机清单.csv', [
    ['主机序列号', '架构', '固件', '登记属性'],
    ...selectedHosts.value.map((host) => [
      host.sn,
      host.arch,
      host.firmware,
      JSON.stringify(
        Object.fromEntries(
          Object.entries(host.attrs ?? {}).filter(
            ([key]) => !isInventoryControlAttribute(key),
          ),
        ),
      ),
    ]),
  ]);
}
const [Grid, gridApi] = useVbenVxeGrid<HostListItem>({
  class: 'h-auto',
  gridOptions: {
    columns: [
      { width: 48, slots: { header: 'select-all', default: 'select-row' } },
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
      { title: '操作', width: 310, slots: { default: 'action' } },
    ],
    maxHeight: 520,
    pagerConfig: { enabled: false },
    rowConfig: { keyField: 'sn' },
    proxyConfig: {
      ajax: {
        query: async () => {
          try {
            const query = { ...appliedFilters };
            const result = await page.load((pagination) =>
              getHostListApi({ ...query, ...pagination }),
            );
            selection.refresh(result.items);
            return result;
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
  selection.clear();
  failure.value = undefined;
  Object.assign(appliedFilters, filters);
  page.reset();
  void gridApi.reload();
}
function resetFilters() {
  Object.assign(filters, { prefix: '', attr_key: '', attr_value: '' });
  search();
}
function changePage(value: number) {
  page.setPage(value);
  void gridApi.reload();
}
function changePageSize(value: number) {
  page.setPageSize(value);
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
  attrs.value = Object.entries(host?.attrs ?? {})
    .filter(([key]) => !isInventoryControlAttribute(key))
    .map(([key, value]) => ({ key, value }));
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
    void gridApi.reload();
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
  if (!canWrite.value) return;
  try {
    await deleteHostApi(host.sn, host.rev);
    void gridApi.reload();
  } catch (error) {
    failure.value = error;
  }
}
const inventoryRequests = reactive(new Set<string>());
async function requestInventory(sn: string) {
  if (!canWrite.value || inventoryRequests.has(sn)) return;
  inventoryRequests.add(sn);
  failure.value = undefined;
  try {
    await requestHostInventoryApi(sn);
    ElMessage.success('硬件采集请求已登记');
    void gridApi.reload();
  } catch (error) {
    failure.value = error;
  } finally {
    inventoryRequests.delete(sn);
  }
}
const csvOpen = ref(false);
const csvText = ref('');
const csvName = ref('');
const csvBusy = ref(false);
const confirmed = ref(false);
const csv = reactive(new CsvImportSession());
const canCommitCSV = computed(
  () =>
    csvOpen.value &&
    canWrite.value &&
    !csvBusy.value &&
    confirmed.value &&
    csv.canConfirm,
);
watch(
  csvOpen,
  (open) => {
    if (open) return;
    csv.select();
    confirmed.value = false;
    csvBusy.value = false;
  },
  { flush: 'sync' },
);
function openCSV() {
  csvText.value = '';
  csvName.value = '';
  failure.value = undefined;
  csvOpen.value = true;
}
function changeCSVText(value: string) {
  csvName.value = '';
  confirmed.value = false;
  failure.value = undefined;
  csv.select(new TextEncoder().encode(value));
}
async function selectCSV(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  input.value = '';
  csvText.value = '';
  csvName.value = file.name;
  confirmed.value = false;
  failure.value = undefined;
  const reading = csv.selectFile(() => file.arrayBuffer());
  const version = csv.version;
  try {
    await reading;
  } catch (error) {
    if (version === csv.version) failure.value = error;
  }
}
async function previewCSV() {
  if (!csvOpen.value || !csv.ready || csvBusy.value) return;
  const version = csv.version;
  csvBusy.value = true;
  failure.value = undefined;
  confirmed.value = false;
  try {
    await csv.preview(previewHostCSVApi);
  } catch (error) {
    if (version === csv.version) failure.value = error;
  } finally {
    if (version === csv.version) csvBusy.value = false;
  }
}
async function commitCSV() {
  if (!canCommitCSV.value) return;
  const version = csv.version;
  csvBusy.value = true;
  failure.value = undefined;
  try {
    await csv.commit(commitHostCSVApi, confirmed.value);
    if (version === csv.version) {
      csvOpen.value = false;
      ElMessage.success('CSV 已整体导入');
    }
    void gridApi.reload();
  } catch (error) {
    if (version === csv.version) failure.value = error;
  } finally {
    if (version === csv.version) csvBusy.value = false;
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
    <section class="overflow-hidden rounded-lg border border-border bg-card">
      <form
        class="flex flex-wrap items-end gap-3 px-5 py-4"
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
        <div class="ml-auto flex flex-wrap items-center justify-end gap-3">
          <ElButton native-type="submit" type="primary">查询</ElButton>
          <ElButton class="!ml-0" @click="resetFilters">重置</ElButton>
          <ElButton
            :disabled="page.loading || !selectedHosts.length"
            @click="exportHosts"
            >
导出所选
</ElButton>
        </div>
      </form>
      <Grid>
        <template #select-all>
          <ElCheckbox
            aria-label="选择本页主机"
            :model-value="all"
            :indeterminate="partial"
            :disabled="page.loading || !page.items.length"
            @change="selection.togglePage"
          />
        </template>
        <template #select-row="{ row }">
          <ElCheckbox
            :aria-label="`选择主机 ${row.sn}`"
            :model-value="selection.has(row)"
            :disabled="page.loading"
            @change="selection.toggle(row, $event)"
          />
        </template>
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
          <ElButton link type="primary" @click="viewHost(row)">
查看详情
</ElButton>
          <ElButton v-if="canWrite" link @click="openHost(row)">编辑</ElButton>
          <ElButton
            v-if="canWrite"
            link
            :loading="inventoryRequests.has(row.sn)"
            @click="requestInventory(row.sn)"
          >
            请求硬件采集
          </ElButton>
          <ElButton v-if="canWrite" link type="danger" @click="removeHost(row)">
            删除
          </ElButton>
        </template>
      </Grid>
      <div class="px-4 pb-3">
        <ListPagination
          :current-page="page.page"
          :page-size="page.pageSize"
          :total="page.total"
          :page-count="page.items.length"
          :selected-count="selectedHosts.length"
          :disabled="page.loading"
          @page-change="changePage"
          @size-change="changePageSize"
        />
      </div>
    </section>
    <ElDrawer
      v-model="detailOpen"
      :title="detail?.sn || '主机详情'"
      size="min(580px, 100vw)"
      @close="closeHostDetails"
    >
      <p v-if="!detail && detailLoading" role="status">正在读取主机…</p>
      <ElAlert
        v-if="!detail && detailError"
        :title="detailError"
        type="error"
        :closable="false"
      />
      <div v-if="detail" class="grid gap-5">
        <p v-if="detailLoading" class="text-sm text-muted-foreground">
          正在读取最新信息…
        </p>
        <ElAlert
          v-if="detailError"
          type="error"
          :title="detailError"
          :closable="false"
        >
          <ElButton link @click="viewHost(detail)">重试</ElButton>
        </ElAlert>
        <section class="rounded-lg border border-border p-4">
          <h3 class="mb-3 font-semibold">基本信息</h3>
          <dl class="grid grid-cols-2 gap-3 text-sm">
            <div>
              <dt class="text-muted-foreground">架构</dt>
              <dd>{{ detail.arch || '未登记' }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">固件</dt>
              <dd>{{ detail.firmware?.toUpperCase() || '未登记' }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">序列号</dt>
              <dd>{{ detail.sn }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">修订</dt>
              <dd>{{ detail.rev }}</dd>
            </div>
          </dl>
        </section>
        <HostTaskHistory
          v-if="detailOpen && hasMenu('tasks')"
          :sn="detail.sn"
        />
        <section class="rounded-lg border border-border p-4">
          <h3 class="mb-2 font-semibold">硬件采集</h3>
          <p class="mb-3 text-sm text-muted-foreground">
            {{ inventoryStatus }}
          </p>
          <dl v-if="hardware.length" class="grid gap-3 text-sm">
            <div v-for="[key, value] in hardware" :key="key">
              <dt class="text-muted-foreground">{{ hardwareNames[key] }}</dt>
              <dd class="break-all">{{ value }}</dd>
            </div>
          </dl>
          <p v-else class="text-sm text-muted-foreground">未采集</p>
        </section>
        <section class="min-w-0 rounded-lg border border-border p-4">
          <h3 class="mb-3 font-semibold">磁盘</h3>
          <div v-if="disks.length" class="overflow-x-auto">
            <table aria-label="磁盘清单" class="w-full text-left text-sm">
              <thead class="bg-muted text-muted-foreground">
                <tr>
                  <th scope="col" class="p-2">设备名</th>
                  <th scope="col" class="p-2">型号</th>
                  <th scope="col" class="p-2 text-right">容量</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="(disk, index) in disks"
                  :key="index"
                  class="border-b border-border"
                >
                  <td class="p-2 font-mono">{{ disk.device }}</td>
                  <td class="break-words p-2">{{ disk.model || '未采集' }}</td>
                  <td
                    class="whitespace-nowrap p-2 text-right tabular-nums"
                    :title="`${disk.bytes } 字节`"
                  >
                    {{ disk.capacity }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else class="text-sm text-muted-foreground">未采集</p>
        </section>
        <section class="min-w-0 rounded-lg border border-border p-4">
          <h3 class="mb-3 font-semibold">网卡</h3>
          <div v-if="network.length" class="overflow-x-auto">
            <table aria-label="网卡清单" class="w-full text-left text-sm">
              <thead class="bg-muted text-muted-foreground">
                <tr>
                  <th scope="col" class="p-2">接口名</th>
                  <th scope="col" class="p-2">型号</th>
                  <th scope="col" class="p-2">网卡地址</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="(nic, index) in network"
                  :key="index"
                  class="border-b border-border"
                >
                  <td class="p-2 font-mono">{{ nic.device }}</td>
                  <td class="break-words p-2">{{ nic.model || '未采集' }}</td>
                  <td class="whitespace-nowrap p-2 font-mono">
                    {{ nic.mac || '未采集' }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else class="text-sm text-muted-foreground">未采集</p>
        </section>
        <section class="rounded-lg border border-border p-4">
          <h3 class="mb-3 font-semibold">登记属性</h3>
          <dl v-if="registered.length" class="grid gap-2 text-sm">
            <div
              v-for="[key, value] in registered"
              :key="key"
              class="grid grid-cols-[1fr_2fr] gap-3"
            >
              <dt class="break-all text-muted-foreground">{{ key }}</dt>
              <dd class="break-all">{{ value }}</dd>
            </div>
          </dl>
          <p v-else class="text-sm text-muted-foreground">暂无登记属性。</p>
        </section>
        <div
          v-if="access.accessCodes.includes('task:create') && hasMenu('tasks')"
        >
          <ElButton
            @click="
              detailOpen = false;
              router.push({ name: 'TasksNew', query: { host: detail.sn } });
            "
            >
为这台主机创建任务
</ElButton>
        </div>
      </div>
    </ElDrawer>
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
      <ElButton
        class="my-3"
        :disabled="!csv.ready"
        :loading="csvBusy"
        @click="previewCSV"
      >
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
        :disabled="csvBusy || !csv.canConfirm"
      >
        已核对 {{ csv.plan.total }} 行，确认整体导入本次预览的 CSV
      </ElCheckbox>
      <template #footer>
        <ElButton :disabled="csvBusy" @click="csvOpen = false">关闭</ElButton>
        <ElButton
          type="primary"
          :disabled="!canCommitCSV"
          :loading="csvBusy"
          @click="commitCSV"
        >
          确认导入
        </ElButton>
      </template>
    </ElDialog>
  </Page>
</template>
