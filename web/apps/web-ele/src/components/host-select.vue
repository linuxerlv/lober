<script setup lang="ts">
/**
 * 主机选择控件（SPEC §4.3、§12.5、§12.7 的 x-widget: host-select）。
 *
 * 删掉 armed 状态之后，建任务是唯一一次人工确认机器身份的机会：运维打错一个
 * 字符、恰好撞上某台生产机的 SN，那台机器某天网络启动就会被擦盘（SPEC §10
 * 失败模式 2b）。所以 SN 字段绝不能是自由文本 input，必须从 `GET /hosts`
 * 选择，并且要显示 SN、机架位、上次被发现时间三项供人核对（SPEC §3.2 硬要求，
 * 从原 arm 界面搬来）。
 *
 * 文本搜索不是便利功能：主机数超过一页而选不到目标机器时，运维会想办法绕过
 * 这个约束（比如去改前端传参直接拼 SN 自由文本）——search 走服务端 `q` 参数
 * （GET /hosts?q=），不是纯客户端过滤，避免"看着没有其实在后面"的假阴性。
 *
 * v-model 绑定的是主机清单表的自有主键 `id`（建任务的入参，SPEC 约定 3），
 * 不是 sn/mac/uuid —— 这样即便 SN 后续被修正，任务仍能稳定引用同一条主机记录。
 *
 * 同时把选中主机的 arch / firmware 通过 `hostChange` 事件透出：建任务页要用
 * 这两项过滤出可装的 OS 列表（SPEC §4.3 末条），不在建任务时拦，等机器实际
 * 引导时才会被 §4.6 拒绝，那时运维已经在机房了。
 */
import type { HostListItem } from '#/api/console/host';

import { computed, onMounted, ref } from 'vue';

import { useDebounceFn } from '@vueuse/core';
import dayjs from 'dayjs';
import { ElSelectV2 } from 'element-plus';

import { getHostListApi } from '#/api/console/host';

defineOptions({ name: 'HostSelect' });

const props = withDefaults(
  defineProps<{
    disabled?: boolean;
    placeholder?: string;
  }>(),
  {
    disabled: false,
    placeholder: '按 SN / 机架位搜索并选择主机',
  },
);

const emit = defineEmits<{
  /** 选中主机变化时透出完整记录，供建任务页读取 arch / firmware 过滤 OS 列表 */
  hostChange: [host: HostListItem | undefined];
}>();

const modelValue = defineModel<string | undefined>({ default: undefined });

const loading = ref(false);
const hosts = ref<HostListItem[]>([]);

interface HostOption {
  value: string;
  /** 收起状态下显示的文案：仅 SN 不够辨识，加上机架位 */
  label: string;
  host: HostListItem;
}

const options = computed<HostOption[]>(() =>
  hosts.value.map((host) => ({
    value: host.id,
    label: host.rack ? `${host.sn}（${host.rack}）` : host.sn,
    host,
  })),
);

const selectedHost = computed(() =>
  hosts.value.find((host) => host.id === modelValue.value),
);

async function fetchHosts(q?: string) {
  loading.value = true;
  try {
    hosts.value = await getHostListApi(q ? { q } : undefined);
  } finally {
    loading.value = false;
  }
}

const debouncedFetch = useDebounceFn((q: string) => fetchHosts(q), 300);

function handleRemoteSearch(q: string) {
  // 空字符串也要发一次请求：拉回默认（无 q）清单，不能停留在上一次搜索结果上
  debouncedFetch(q);
}

function handleChange(hostId: string | undefined) {
  // 清空时 ElSelectV2 传回空字符串，统一成 undefined 以匹配 v-model 的声明类型
  const normalized = hostId || undefined;
  modelValue.value = normalized;
  emit('hostChange', normalized ? selectedHost.value : undefined);
}

function formatLastSeen(lastSeenAt: number): string {
  if (!lastSeenAt) {
    return '从未上报';
  }
  // 契约字段是 unix 秒（例如 1695000000），dayjs 默认吃毫秒，要 *1000
  return dayjs(lastSeenAt * 1000).format('YYYY-MM-DD HH:mm:ss');
}

onMounted(() => {
  fetchHosts();
});

defineExpose({
  /** 建任务页读取当前选中主机的完整记录（含 arch / firmware），无需自行再查一次 */
  getSelectedHost: () => selectedHost.value,
  /** 手动触发刷新清单（例如手工录入主机后立即重新拉取） */
  refresh: () => fetchHosts(),
});
</script>

<template>
  <ElSelectV2
    :model-value="modelValue"
    :options="options"
    :loading="loading"
    :disabled="props.disabled"
    :placeholder="props.placeholder"
    filterable
    remote
    clearable
    :remote-method="handleRemoteSearch"
    class="w-full"
    @update:model-value="handleChange"
  >
    <template #default="{ item }">
      <div class="flex flex-col gap-0.5 py-1">
        <span class="font-medium">{{ item.host.sn }}</span>
        <span class="text-muted-foreground text-xs">
          机架位：{{ item.host.rack || '未登记' }} 上次发现：{{
            formatLastSeen(item.host.lastSeenAt)
          }}
        </span>
      </div>
    </template>
  </ElSelectV2>
</template>
