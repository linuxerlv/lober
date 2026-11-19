<script lang="ts" setup>
import type { DiscoveredHost } from '#/api/console/discovery';

import { computed, ref, watch } from 'vue';

import { ElAlert, ElButton, ElDrawer, ElInput } from 'element-plus';

import {
  getDiscoveryApi,
  getDiscoveryListApi,
  registerDiscoveryApi,
} from '#/api/console/discovery';
import { errorMessage } from '#/api/console/problem';

const props = defineProps<{ canCreateTask?: boolean; canWrite: boolean }>();
const emit = defineEmits<{ createTask: [sn: string]; registered: [sn: string] }>();
const open = defineModel<boolean>({ required: true });

const hostStatus = {
  received: '已收到硬件报告',
  timed_out: '硬件报告未收到',
  unsupported: '没有可用的采集镜像',
  waiting: '等待硬件报告',
};
const sectionStatus = {
  absent: '未安装',
  partial: '部分读取',
  read: '已读取',
  unread: '未读取',
};

const items = ref<DiscoveredHost[]>([]);
const cursor = ref<string>();
const nextCursor = ref<string>();
const selectedHost = ref<DiscoveredHost>();
const incompleteSections = computed(
  () =>
    selectedHost.value?.report?.sections.filter(
      ({ status }) => status === 'partial' || status === 'unread',
    ).length ?? 0,
);
const serial = ref('');
const listError = ref('');
const detailError = ref('');
const registerError = ref('');
const pending = ref(false);
const detailLoading = ref(false);
const saving = ref(false);
let detailRun = 0;

function asDate(seconds: number) {
  return new Date(seconds * 1000).toLocaleString();
}

function viewHost(host: DiscoveredHost) {
  selectedHost.value = host;
  serial.value = host.sn ?? '';
  registerError.value = '';
  void loadDetails();
}

function resetDetails() {
  detailRun++;
  selectedHost.value = undefined;
  detailLoading.value = false;
}

function backToList() {
  resetDetails();
  void load(cursor.value);
}

async function loadDetails() {
  const host = selectedHost.value;
  if (!host || saving.value || detailLoading.value) return;
  const run = ++detailRun;
  detailLoading.value = true;
  detailError.value = '';
  try {
    const full = await getDiscoveryApi(host.mac);
    if (run === detailRun) selectedHost.value = full;
  } catch (error) {
    if (run === detailRun) detailError.value = errorMessage(error);
  } finally {
    if (run === detailRun) detailLoading.value = false;
  }
}

async function load(target?: string) {
  if (pending.value || saving.value) return;
  pending.value = true;
  listError.value = '';
  try {
    const result = await getDiscoveryListApi(target);
    items.value = result.items;
    nextCursor.value = result.next_cursor;
    cursor.value = target;
  } catch (error) {
    listError.value = errorMessage(error);
  } finally {
    pending.value = false;
  }
}

watch(
  open,
  (visible, _, onCleanup) => {
    if (!visible) {
      resetDetails();
      return;
    }
    void load();
    const interval = setInterval(() => {
      if (selectedHost.value?.status === 'waiting') void loadDetails();
      if (!selectedHost.value && !cursor.value) void load();
    }, 10_000);
    onCleanup(() => clearInterval(interval));
  },
  { immediate: true },
);

async function confirmRegistration() {
  const host = selectedHost.value;
  const sn = serial.value.trim();
  if (
    !props.canWrite ||
    detailLoading.value ||
    saving.value ||
    !host ||
    host.registered_sn ||
    !sn
  ) {
    return;
  }

  saving.value = true;
  registerError.value = '';
  try {
    const registered = await registerDiscoveryApi(host.mac, sn);
    if (selectedHost.value?.mac === host.mac) {
      selectedHost.value = { ...selectedHost.value, registered_sn: registered.sn };
    }
    emit('registered', registered.sn);
  } catch (error) {
    registerError.value = errorMessage(error);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <ElDrawer v-model="open" title="主机自动发现" size="min(900px, 100vw)">
    <div class="grid gap-4">
      <div class="flex flex-wrap justify-end gap-2">
        <ElButton v-if="selectedHost" :disabled="saving" @click="backToList">
          返回列表
        </ElButton>
        <ElButton v-else-if="cursor" :disabled="pending || saving" @click="load()">
          返回第一页
        </ElButton>
        <ElButton
          :disabled="pending || detailLoading || saving"
          @click="selectedHost ? loadDetails() : load(cursor)"
        >
          刷新
        </ElButton>
        <ElButton
          v-if="nextCursor && !selectedHost"
          :disabled="pending || saving"
          @click="load(nextCursor)"
        >
          下一页
        </ElButton>
      </div>
      <ElAlert
        v-if="listError"
        :title="listError"
        :closable="false"
        type="error"
      />
      <p v-if="!items.length && pending" role="status">正在读取发现记录…</p>
      <p v-else-if="!items.length && !listError" class="text-muted-foreground">
        暂无发现记录。
      </p>
      <div v-if="items.length && !selectedHost" class="overflow-x-auto rounded border border-border">
        <table aria-label="发现记录" class="w-full text-left text-sm">
          <thead class="bg-muted text-muted-foreground">
            <tr>
              <th scope="col" class="p-2">启动网口地址</th>
              <th scope="col" class="p-2">整机型号</th>
              <th scope="col" class="p-2">序列号</th>
              <th scope="col" class="p-2">最近报到</th>
              <th scope="col" class="p-2">硬件报告</th>
              <th scope="col" class="p-2">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in items"
              :key="row.mac"
              class="border-t border-border"
            >
              <td class="whitespace-nowrap p-2 font-mono">{{ row.mac }}</td>
              <td class="p-2">{{ row.system_model || '未读取' }}</td>
              <td class="p-2">{{ row.registered_sn || row.sn || '未读取' }}</td>
              <td class="p-2">{{ asDate(row.last_seen_at) }}</td>
              <td class="p-2">{{ hostStatus[row.status] }}</td>
              <td class="p-2">
                <ElButton link type="primary" :disabled="saving" @click="viewHost(row)">
                  查看详情
                </ElButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <section v-if="selectedHost" class="grid gap-4 rounded-lg border border-border p-4">
        <div>
          <h3 class="mb-2 font-semibold">发现详情</h3>
          <dl class="grid grid-cols-2 gap-2 text-sm">
            <div>
              <dt class="text-muted-foreground">启动网口地址</dt>
              <dd class="font-mono">{{ selectedHost.mac }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">报告的序列号</dt>
              <dd class="break-all">{{ selectedHost.sn || '未读取' }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">架构与启动方式</dt>
              <dd>{{ selectedHost.arch }} / {{ selectedHost.firmware.toUpperCase() }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">首次发现</dt>
              <dd>{{ asDate(selectedHost.first_seen_at) }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">最近报到</dt>
              <dd>{{ asDate(selectedHost.last_seen_at) }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">硬件报告</dt>
              <dd>{{ hostStatus[selectedHost.status] }}</dd>
            </div>
          </dl>
        </div>
        <ElAlert
          v-if="selectedHost.reason"
          :title="selectedHost.reason"
          :closable="false"
          type="info"
        />
        <ElAlert
          v-if="detailError"
          :title="detailError"
          :closable="false"
          type="error"
        />
        <div v-if="selectedHost.registered_sn" class="flex flex-wrap items-center gap-3">
          <p>已登记为 {{ selectedHost.registered_sn }}</p>
          <ElButton
            v-if="canCreateTask"
            type="primary"
            @click="emit('createTask', selectedHost.registered_sn)"
          >
            为这台主机创建装机任务
          </ElButton>
        </div>
        <form
          v-else-if="canWrite"
          class="flex flex-wrap items-end gap-3"
          @submit.prevent="confirmRegistration"
        >
          <label class="grid gap-1">
            核对主机序列号
            <ElInput v-model="serial" :disabled="saving" required />
          </label>
          <ElButton
            :disabled="!serial.trim() || detailLoading"
            :loading="saving"
            native-type="submit"
            type="primary"
          >
            确认登记
          </ElButton>
          <ElAlert
            v-if="registerError"
            :title="registerError"
            :closable="false"
            type="error"
          />
        </form>
        <div>
          <h3 class="mb-2 font-semibold">部件清单</h3>
          <p v-if="detailLoading" class="mb-2 text-sm" role="status">正在读取部件清单…</p>
          <p v-if="selectedHost.report_at" class="mb-2 text-sm text-muted-foreground">
            {{ selectedHost.status === 'received' ? '报告时间' : '保留的上次报告' }}：
            {{ asDate(selectedHost.report_at) }}
          </p>
          <p v-if="incompleteSections" class="mb-2 text-sm">
            {{ incompleteSections }} 类部件未完整读取。
          </p>
          <div v-if="selectedHost.report" class="grid gap-2">
            <details
              v-for="section in selectedHost.report.sections"
              :key="section.key"
              class="rounded border border-border p-3"
              open
            >
              <summary class="cursor-pointer">
                {{ section.title }} · {{ sectionStatus[section.status] }}
              </summary>
              <p v-if="section.reason" class="mt-2 text-sm text-muted-foreground">
                {{ section.reason }}
              </p>
              <div
                v-for="(item, index) in section.items"
                :key="index"
                class="mt-3 border-t border-border pt-2 text-sm"
              >
                <h4 class="mb-1 font-medium">{{ item.name }}</h4>
                <dl class="grid gap-1">
                  <div
                    v-for="(value, key) in item.fields"
                    :key="key"
                    class="grid grid-cols-[1fr_2fr] gap-3"
                  >
                    <dt class="break-all text-muted-foreground">{{ key }}</dt>
                    <dd class="break-all">{{ value || '未读取' }}</dd>
                  </div>
                </dl>
              </div>
            </details>
          </div>
          <p v-else-if="!detailLoading && !detailError" class="text-sm text-muted-foreground">
            暂无部件清单。
          </p>
        </div>
      </section>
    </div>
  </ElDrawer>
</template>
