<script setup lang="ts">
/**
 * hook 审计记录时间线（P2-... 审计查询 UI，端点 29
 * `GET /hooks/{id}/audit`，见 docs/contracts/http-console.md「4. hook」节）。
 *
 * 存在的理由：`POST /hooks/{id}/reject` 的驳回理由此前只在驳回当次的
 * 响应里可见，作者若当时不在场就永远查不到——这是被独立核实过的契约
 * 缺口（docs/contracts/http-console.md「变更记录 2」）。本组件让理由
 * 随时可查，不再要求作者必须在驳回发生的那一刻在场。
 *
 * 权限边界：本端点只对 hook-author（hook:write）或 hook-approver
 * （hook:approve）开放（admin 通过聚合权限码天然满足），因为审计记录
 * 含操作人信息，与这个 hook 无关的角色不应看到谁做了什么（SPEC §9.4）。
 * 无权限时服务端返回 `code=1003`（HTTP 状态码仍是 200，响应包裹格式
 * 约定如此），本组件识别这个具体错误码，展示「无权限查看」而不是笼统的
 * 「加载失败」——两者对使用者的含义完全不同：前者是「换个有权限的人来看」，
 * 后者会被误当成网络或服务故障去重试。
 */
import type { AuditRecordItem } from '#/api/console/audit';

import { onMounted, ref, watch } from 'vue';

import { ElEmpty, ElTimeline, ElTimelineItem } from 'element-plus';

import { getHookAuditApi } from '#/api/console/audit';

defineOptions({ name: 'AuditTimeline' });

const props = defineProps<{
  /** 目标 hook id，变化时自动重新拉取 */
  hookId: string;
}>();

const loading = ref(false);
/** 区分「无权限」与其它失败：403（code=1003）以外的错误也要展示具体原因，
 * 不能笼统吞成一句「加载失败」——运维需要知道是网络问题还是服务端故障。 */
const errorMessage = ref('');
const forbidden = ref(false);
const records = ref<AuditRecordItem[]>([]);

/** 与服务端 apierr.CodeForbidden 的数值同步，见 internal/apierr/apierr.go */
const CODE_FORBIDDEN = 1003;

async function fetchAudit() {
  if (!props.hookId) return;
  loading.value = true;
  errorMessage.value = '';
  forbidden.value = false;
  try {
    records.value = await getHookAuditApi(props.hookId);
  } catch (error: any) {
    // defaultResponseInterceptor 对非零 code 统一 throw
    // Object.assign({}, response, {response})，因此失败详情落在
    // error.response.data 里（与全局 errorMessageResponseInterceptor
    // 读取的路径一致），不是 error.message。
    const data = error?.response?.data;
    if (data?.code === CODE_FORBIDDEN) {
      forbidden.value = true;
      errorMessage.value =
        data?.message || '你没有权限查看这个 hook 的审计记录';
    } else {
      errorMessage.value = data?.message || '审计记录加载失败，请重试';
    }
    records.value = [];
  } finally {
    loading.value = false;
  }
}

function formatTs(ts: number): string {
  // 契约字段是 unix 秒，与 HookItem.approvedAt 同单位（hooks/index.vue
  // 已有的 new Date(v.approvedAt * 1000) 是同一约定）。
  return ts ? new Date(ts * 1000).toLocaleString() : '（无时间）';
}

watch(() => props.hookId, fetchAudit);
onMounted(fetchAudit);

defineExpose({
  /** 驳回等操作之后需要看到最新记录时，父组件手动触发刷新 */
  reload: fetchAudit,
});
</script>

<template>
  <div>
    <div v-if="loading" class="py-6 text-center text-sm text-gray-500">
      加载中…
    </div>
    <div
      v-else-if="forbidden"
      class="rounded border border-yellow-300 bg-yellow-50 p-3 text-sm text-yellow-700"
    >
      {{ errorMessage }}
    </div>
    <div
      v-else-if="errorMessage"
      class="rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700"
    >
      {{ errorMessage }}
    </div>
    <ElEmpty v-else-if="records.length === 0" description="暂无审计记录" />
    <ElTimeline v-else>
      <ElTimelineItem
        v-for="(record, idx) in records"
        :key="`${record.ts}-${idx}`"
        :timestamp="formatTs(record.ts)"
        :type="record.action === 'hook.reject' ? 'danger' : 'primary'"
      >
        <div class="font-bold">{{ record.actor }} · {{ record.action }}</div>
        <div v-if="record.detail" class="mt-1 whitespace-pre-wrap text-sm">
          {{ record.detail }}
        </div>
      </ElTimelineItem>
    </ElTimeline>
  </div>
</template>
