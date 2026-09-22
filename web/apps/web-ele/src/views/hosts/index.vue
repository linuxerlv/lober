<script lang="ts" setup>
import type { VbenFormSchema } from '@vben/common-ui';

/**
 * 主机清单页（SPEC §12.6、契约「2. 主机清单」端点 5-6）。
 *
 * 这是 SN 的唯一来源（§4.3、契约「约定 1」）：建任务页的 host-select 组件
 * 从 GET /hosts 取值，不接受自由文本；本页是运维核对与维护这份清单的地方。
 *
 * q 文本搜索不是便利功能——主机数超过一页而运维找不到目标机器时，若没有
 * 搜索手段就会想办法绕过这个约束（比如去改前端传参直接拼 SN 自由文本）。
 * 这是删掉 armed 状态后唯一的人工确认点在主机规模超过一页时仍然可用的
 * 必要条件（SPEC §9.1、§10 失败模式 2b），因此这里的搜索走服务端 q 参数，
 * 不是纯客户端过滤。
 *
 * POST /hosts 是手工录入兜底（SPEC §7.4：discovery 尚未覆盖到的机器），
 * arch/firmware/sn/mac 必填，uuid 可选；MAC 入库前服务端会做 NormalizeMAC
 * 归一化，格式不合法返回 CodeMACInvalid（前端不重复实现这份校验，只原样
 * 展示服务端的错误信息）。
 *
 * 列表页统一用 useVbenVxeGrid（§12.6 末句），沿用 hooks/artifact-sets 页
 * 已确立的写法：本地类型 + requestClient 直连 + proxyConfig.ajax.query
 * 把裸数组包成 { items }（契约约定 2）。
 */
import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';

import {
  ElButton,
  ElInput,
  ElMessage,
  ElOption,
  ElSelect,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { requestClient } from '#/api/request';

defineOptions({ name: 'HostsIndex' });

/** 对应 model.Arch（inventory.go） */
type Arch = 'arm64' | 'x86_64';
/** 机器可上报的固件子集，不含 model.FirmwareMode 的 any（那只在 ArtifactSet 上出现） */
type Firmware = 'bios' | 'uefi';

/** 主机清单条目，字段名与 model.Host 逐一对应，不另起名字（契约「2. 主机清单」节）。 */
interface HostListItem {
  arch: Arch;
  firmware: Firmware;
  id: string;
  lastSeenAt: number;
  mac: string;
  rack: string;
  sn: string;
  uuid: string;
}

const ARCH_OPTIONS: Arch[] = ['x86_64', 'arm64'];
const FIRMWARE_OPTIONS: Firmware[] = ['uefi', 'bios'];

/** 搜索表单：q 走服务端 GET /hosts?q= 子串搜索（匹配 sn/mac/rack，不区分大小写）。 */
const filterFormSchema: VbenFormSchema[] = [
  {
    component: 'Input',
    componentProps: {
      clearable: true,
      placeholder: '按 SN / MAC / 机架位子串搜索',
    },
    fieldName: 'q',
    label: '搜索',
  },
];

async function queryHosts(formValues: Record<string, any>) {
  const params: Record<string, string> = {};
  if (formValues.q) params.q = formValues.q;
  const items = await requestClient.get<HostListItem[]>('/hosts', { params });
  return { items };
}

const [Grid, gridApi] = useVbenVxeGrid<HostListItem>({
  formOptions: {
    schema: filterFormSchema,
  },
  gridOptions: {
    columns: [
      { field: 'sn', title: 'SN', width: 180 },
      { field: 'mac', title: 'MAC', width: 160 },
      { field: 'uuid', title: 'UUID', minWidth: 220 },
      { field: 'arch', title: '架构', width: 100, slots: { default: 'arch' } },
      {
        field: 'firmware',
        title: '固件',
        width: 100,
        slots: { default: 'firmware' },
      },
      { field: 'rack', title: '机架位', width: 140 },
      {
        field: 'lastSeenAt',
        title: '上次被发现时间',
        width: 180,
        formatter: ({ cellValue }: { cellValue: number }) =>
          cellValue ? new Date(cellValue * 1000).toLocaleString() : '从未',
      },
    ],
    height: 'auto',
    keepSource: true,
    proxyConfig: {
      ajax: {
        query: async (_params: any, formValues: Record<string, any>) =>
          queryHosts(formValues),
      },
    },
    rowConfig: { keyField: 'id' },
    toolbarConfig: { refresh: true, search: true },
  },
});

/** 手工录入表单（SPEC §7.4 兜底），提交前不做 MAC 归一化——那是服务端职责。 */
const createForm = ref({
  sn: '',
  mac: '',
  uuid: '',
  arch: 'x86_64' as Arch,
  firmware: 'uefi' as Firmware,
  rack: '',
});

function resetCreateForm() {
  createForm.value = {
    sn: '',
    mac: '',
    uuid: '',
    arch: 'x86_64',
    firmware: 'uefi',
    rack: '',
  };
}

const [CreateModal, createModalApi] = useVbenModal({
  async onConfirm() {
    if (!createForm.value.sn.trim() || !createForm.value.mac.trim()) {
      ElMessage.error('SN 与 MAC 均为必填');
      return;
    }
    try {
      await requestClient.post<HostListItem>('/hosts', {
        sn: createForm.value.sn.trim(),
        mac: createForm.value.mac.trim(),
        uuid: createForm.value.uuid.trim() || undefined,
        arch: createForm.value.arch,
        firmware: createForm.value.firmware,
        rack: createForm.value.rack.trim() || undefined,
      });
      ElMessage.success('已录入');
      createModalApi.close();
      gridApi.reload();
    } catch (error: any) {
      // MAC 格式不合法时服务端返回 CodeMACInvalid，message 直接展示，
      // 不在前端重新实现归一化校验逻辑。
      const message =
        error?.response?.data?.message || error?.message || '录入失败';
      ElMessage.error(message);
    }
  },
  onCancel() {
    createModalApi.close();
  },
});

function openCreate() {
  resetCreateForm();
  createModalApi.open();
}
</script>

<template>
  <Page
    description="已发现主机清单，是建任务页 SN 字段的唯一数据源；discovery 未覆盖到的机器可在此手工录入兜底（§7.4）。"
    title="主机清单"
  >
    <template #extra>
      <ElButton type="primary" @click="openCreate">手工录入主机</ElButton>
    </template>

    <Grid>
      <template #arch="{ row }">
        <ElTag size="small">{{ row.arch }}</ElTag>
      </template>
      <template #firmware="{ row }">
        <ElTag size="small" type="info">{{ row.firmware }}</ElTag>
      </template>
    </Grid>

    <CreateModal class="w-[480px]" title="手工录入主机">
      <div class="grid grid-cols-2 gap-4">
        <div class="col-span-2">
          <label class="mb-1 block text-sm">SN（必填）</label>
          <ElInput v-model="createForm.sn" />
        </div>
        <div class="col-span-2">
          <label class="mb-1 block text-sm">MAC（必填，入库前会归一化）</label>
          <ElInput v-model="createForm.mac" placeholder="AA:BB:CC:DD:EE:FF" />
        </div>
        <div class="col-span-2">
          <label class="mb-1 block text-sm">UUID（可选）</label>
          <ElInput v-model="createForm.uuid" />
        </div>
        <div>
          <label class="mb-1 block text-sm">架构（必填）</label>
          <ElSelect v-model="createForm.arch" class="w-full">
            <ElOption
              v-for="opt in ARCH_OPTIONS"
              :key="opt"
              :label="opt"
              :value="opt"
            />
          </ElSelect>
        </div>
        <div>
          <label class="mb-1 block text-sm">固件（必填）</label>
          <ElSelect v-model="createForm.firmware" class="w-full">
            <ElOption
              v-for="opt in FIRMWARE_OPTIONS"
              :key="opt"
              :label="opt"
              :value="opt"
            />
          </ElSelect>
        </div>
        <div class="col-span-2">
          <label class="mb-1 block text-sm">机架位（可选）</label>
          <ElInput v-model="createForm.rack" />
        </div>
      </div>
    </CreateModal>
  </Page>
</template>
