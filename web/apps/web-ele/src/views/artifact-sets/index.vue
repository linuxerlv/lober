<script lang="ts" setup>
/**
 * 制品集导入页（P2-05 任务 4）。
 *
 * 导入是异步 job（§2.1）：POST /artifact-sets 立即返回 importing 状态，
 * 真实进度体现在轮询 GET /artifact-sets 的 state 字段变化上——这里没有
 * SSE（MVP 未定义制品集专属事件流，契约文档「3. 制品集」小节已注明）。
 *
 * 气隙环境下 ISO 以「登记服务器本地路径」为主（scp 到指定目录再登记），
 * WebUI 上传是 Phase 2 才补的辅助能力、且契约本身没冻结上传的入参形态——
 * 本页把本地路径登记做成唯一入口，并在界面上解释原因，不做上传按钮
 * （做了会让运维以为这是主路径，产生「几 GB 走浏览器」的糟糕体验）。
 */
import { onBeforeUnmount, ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';

import {
  ElAlert,
  ElButton,
  ElInput,
  ElMessage,
  ElMessageBox,
  ElTag,
  ElTooltip,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { requestClient } from '#/api/request';

defineOptions({ name: 'ArtifactSetsIndex' });

/** 对应 model.ArtifactSetState */
type ArtifactSetState = 'deprecated' | 'failed' | 'importing' | 'ready';

interface ArtifactSetPaths {
  kernel: string;
  initrd: string[];
  repo: string;
  extra: Record<string, string>;
}

interface ArtifactSetItem {
  id: string;
  name: string;
  osFamily: string;
  osVersion: string;
  arch: string;
  firmware: string;
  sourceIsoSha256: string;
  state: ArtifactSetState;
  manifestSha256: string;
  paths: ArtifactSetPaths;
  /** 导入失败原因。契约的 ArtifactSetItem 结构未列出专门的错误字段名，
   * 但 M7 任务 9 的说明是"失败原因要原样展示"——若服务端字段名与此不同，
   * 以 fallback 到 paths.extra.reason 兜底展示，避免运维看到空白。 */
  importError?: string;
}

const STATE_TAG_TYPE: Record<
  ArtifactSetState,
  'danger' | 'info' | 'success' | 'warning'
> = {
  importing: 'warning',
  ready: 'success',
  failed: 'danger',
  deprecated: 'info',
};

const STATE_LABEL: Record<ArtifactSetState, string> = {
  importing: '导入中',
  ready: '就绪',
  failed: '失败',
  deprecated: '已停用',
};

/** MVP 不支持的家族识别出来后也会置 failed（M7 任务 9）——
 * 用错误文案里的关键词把"限制"和"真出错"区分开，避免运维误以为自己的 ISO 坏了。 */
const UNSUPPORTED_FAMILY_HINTS = [
  'windows',
  'esxi',
  'vmware esxi',
  'mvp 不支持',
  'mvp限制',
];

function isUnsupportedFamilyFailure(reason: string) {
  const lower = reason.toLowerCase();
  return UNSUPPORTED_FAMILY_HINTS.some((hint) => lower.includes(hint));
}

function failureReasonOf(row: ArtifactSetItem): string {
  return row.importError || row.paths?.extra?.reason || '';
}

async function queryArtifactSets() {
  const items = await requestClient.get<ArtifactSetItem[]>('/artifact-sets');
  return { items };
}

const [Grid, gridApi] = useVbenVxeGrid<ArtifactSetItem>({
  gridOptions: {
    columns: [
      { field: 'name', title: '名字', minWidth: 160 },
      { field: 'osFamily', title: 'OS 家族', width: 100 },
      { field: 'osVersion', title: '版本', width: 100 },
      { field: 'arch', title: '架构', width: 90 },
      {
        field: 'firmware',
        title: '固件',
        width: 110,
        slots: { default: 'firmware' },
      },
      {
        field: 'state',
        title: '状态',
        width: 100,
        slots: { default: 'state' },
      },
      {
        field: 'importError',
        title: '导入错误',
        minWidth: 220,
        slots: { default: 'importError' },
      },
      {
        field: 'action',
        title: '操作',
        width: 180,
        fixed: 'right',
        slots: { default: 'action' },
      },
    ],
    height: 'auto',
    keepSource: true,
    proxyConfig: {
      ajax: { query: async () => queryArtifactSets() },
    },
    rowConfig: { keyField: 'id' },
    toolbarConfig: { refresh: true },
  },
});

/** 导入是分钟级异步 job，不需要 SSE，轮询刷新即可（契约「3. 制品集」小节）。 */
const POLL_INTERVAL_MS = 5000;
let pollTimer: null | ReturnType<typeof setInterval> = null;

function hasImportingRows() {
  const fullData = gridApi.grid?.getTableData?.()?.fullData as
    | ArtifactSetItem[]
    | undefined;
  return fullData?.some((row) => row.state === 'importing') ?? false;
}

function startPollingIfNeeded() {
  if (pollTimer) return;
  pollTimer = setInterval(() => {
    if (hasImportingRows()) {
      gridApi.reload();
    } else if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }, POLL_INTERVAL_MS);
}

onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer);
});

/** 登记表单：入参是服务器本地路径，不是文件上传（契约 POST /artifact-sets）。 */
const registerForm = ref({ sourcePath: '', name: '' });
const [RegisterModal, registerModalApi] = useVbenModal({
  async onConfirm() {
    if (
      !registerForm.value.sourcePath.trim() ||
      !registerForm.value.name.trim()
    ) {
      ElMessage.error('名字与服务器本地路径均为必填');
      return;
    }
    await requestClient.post<ArtifactSetItem>('/artifact-sets', {
      sourcePath: registerForm.value.sourcePath.trim(),
      name: registerForm.value.name.trim(),
    });
    ElMessage.success('已登记，进入导入中状态，稍后自动刷新进度');
    registerModalApi.close();
    gridApi.reload();
    startPollingIfNeeded();
  },
  onCancel() {
    registerModalApi.close();
  },
});

function openRegister() {
  registerForm.value = { sourcePath: '', name: '' };
  registerModalApi.open();
}

async function verifyIntegrity(row: ArtifactSetItem) {
  await requestClient.post(`/artifact-sets/${row.id}/verify`);
  ElMessage.success('已提交完整性校验，结果体现在下次刷新的状态列');
  startPollingIfNeeded();
}

async function removeArtifactSet(row: ArtifactSetItem) {
  try {
    await ElMessageBox.confirm(
      `确认删除制品集「${row.name}」？只有 deprecated 且未被任何模板引用时才能删除。`,
      '删除确认',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  try {
    await requestClient.delete(`/artifact-sets/${row.id}`);
    ElMessage.success('已删除');
    gridApi.reload();
  } catch (error: any) {
    // 服务端拒绝时的两种情形（state != deprecated / 仍被引用）——
    // 契约要求 UI 提示原因，这里把拦截器已解析出的 message 原样展示。
    const message =
      error?.response?.data?.message || error?.message || '删除被拒绝';
    ElMessage.error(message);
  }
}

const firmwareTooltip =
  'Linux 四家（rhel/debian/ubuntu/suse）固件填 any 即通用；Windows 同一版本 UEFI 与 BIOS 引导方式不同，需要分别登记两个制品集（§2.1）。看到两个名字相似的 Windows 制品集是正常的，不是重复登记。';
</script>

<template>
  <Page
    description="导入是异步 job，登记后进入导入中状态，进度靠轮询刷新观察。"
    title="制品集导入"
  >
    <template #extra>
      <ElButton type="primary" @click="openRegister">登记制品集</ElButton>
    </template>

    <ElAlert
      :closable="false"
      class="mb-3"
      title="为什么没有文件上传入口"
      type="info"
    >
      气隙环境下 ISO 通常几 GB 起步，走浏览器上传体验很差；运维 scp
      到登记服务器指定目录后，
      在这里填入该本地路径即可，这是本系统设计上唯一的登记方式（§2.1）。
    </ElAlert>

    <Grid>
      <template #firmware="{ row }">
        <ElTooltip :content="firmwareTooltip" placement="top">
          <span>{{ row.firmware }}</span>
        </ElTooltip>
      </template>
      <template #state="{ row }">
        <ElTag :type="STATE_TAG_TYPE[row.state as ArtifactSetState]">
          {{ STATE_LABEL[row.state as ArtifactSetState] }}
        </ElTag>
      </template>
      <template #importError="{ row }">
        <template v-if="row.state === 'failed'">
          <ElTag
            v-if="isUnsupportedFamilyFailure(failureReasonOf(row))"
            size="small"
            type="info"
          >
            MVP 限制
          </ElTag>
          <ElTag v-else size="small" type="danger">导入失败</ElTag>
          <span class="ml-1">{{
            failureReasonOf(row) || '（服务端未提供具体原因）'
          }}</span>
        </template>
        <span v-else class="text-gray-400">—</span>
      </template>
      <template #action="{ row }">
        <ElButton link size="small" @click="verifyIntegrity(row)">
          校验完整性
        </ElButton>
        <ElButton
          :disabled="row.state !== 'deprecated'"
          link
          size="small"
          type="danger"
          @click="removeArtifactSet(row)"
        >
          删除
        </ElButton>
      </template>
    </Grid>

    <RegisterModal class="w-[520px]" title="登记制品集">
      <label class="mb-1 block text-sm">名字</label>
      <ElInput v-model="registerForm.name" class="mb-3" />
      <label class="mb-1 block text-sm">
        服务器本地路径（运维已 scp 到指定目录后的绝对路径）
      </label>
      <ElInput
        v-model="registerForm.sourcePath"
        placeholder="/data/lober/iso/xxx.iso"
      />
    </RegisterModal>
  </Page>
</template>
