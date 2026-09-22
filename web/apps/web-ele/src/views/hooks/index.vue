<script lang="ts" setup>
/**
 * hook 管理与审批页（P2-05 任务 1/2/3）。
 *
 * 关键约束（§9.4）：hook 以 root 身份在分区之前运行（`pre` 阶段），
 * 能力等同「擦掉这台机器所有盘」——本页的核心不是让接口可用（Phase 1 已实现），
 * 而是让审批这个动作在界面上真正起到审批的作用：审批人看得见完整正文、
 * 知道自己授权了什么能力、且不能自审自批；驳回要能填理由。
 *
 * hook 正文不可变（§2.7）：编辑 = 建新版本，不是就地修改——本页不提供
 * 「编辑正文」的直接入口，只有「基于此版本创建新版本」。
 */
import type { VbenFormSchema } from '@vben/common-ui';

import { computed, h, ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { useUserStore } from '@vben/stores';

import { useClipboard } from '@vueuse/core';
import {
  ElButton,
  ElDescriptions,
  ElDescriptionsItem,
  ElInput,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { requestClient } from '#/api/request';

defineOptions({ name: 'HooksIndex' });

/** 对应 model.HookStage（docs/contracts/http-console.md §4） */
type HookStage = 'firstboot' | 'post' | 'post-nochroot' | 'pre';
/** 对应 model.HookState */
type HookState = 'active' | 'deprecated' | 'draft' | 'review';

interface HookRequires {
  contractVersion: string;
  facts: string[];
  capabilities: string[];
}

interface HookItem {
  id: string;
  name: string;
  description: string;
  stage: HookStage;
  osFamilies: string[];
  interpreter: string;
  body: string;
  bodySha256: string;
  paramsSchema: string;
  requires: HookRequires;
  required: boolean;
  timeoutSec: number;
  version: number;
  state: HookState;
  author: string;
  approver: string;
  approvedAt: number;
}

interface HookRejectResult extends HookItem {
  reason: string;
}

interface HookValidateIssue {
  level: 'error' | 'warning';
  message: string;
}

interface HookValidateResult {
  passed: boolean;
  issues: HookValidateIssue[];
}

const STAGE_LABEL: Record<HookStage, string> = {
  pre: 'pre（分区前，root）',
  'post-nochroot': 'post-nochroot（分区后未 chroot）',
  post: 'post（chroot 内）',
  firstboot: 'firstboot（首次开机）',
};

/** §9.4：pre 阶段能碰分区，能力边界必须写清楚给审批人看 */
const STAGE_CAPABILITY: Record<HookStage, string> = {
  pre: '此 hook 在分区之前以 root 身份运行，可以擦除、重新分区这台机器的任何磁盘。一旦激活，凡是引用了这个 hook 的模板发起装机时都会以最高权限执行它的正文。',
  'post-nochroot':
    '此 hook 在分区完成、但尚未 chroot 进新系统之前以 root 运行，能读写目标磁盘上已落地的文件，但通常不会重新分区。',
  post: '此 hook 在 chroot 进新系统之后运行，作用范围是新系统内部，不直接操作分区表。',
  firstboot: '此 hook 在新系统首次开机后运行，作用范围是已经装好的系统本身。',
};

const STATE_TAG_TYPE: Record<
  HookState,
  'danger' | 'info' | 'success' | 'warning'
> = {
  draft: 'info',
  review: 'warning',
  active: 'success',
  deprecated: 'danger',
};

const STATE_LABEL: Record<HookState, string> = {
  draft: '草稿',
  review: '待审批',
  active: '已激活',
  deprecated: '已停用',
};

const OS_FAMILY_OPTIONS = ['rhel', 'debian', 'ubuntu', 'suse'];
const INTERPRETER_OPTIONS = ['bash', 'ash', 'powershell'];

const userStore = useUserStore();
const currentUsername = computed(() => userStore.userInfo?.username ?? '');

function shortDigest(sha256: string) {
  return sha256 ? `${sha256.slice(0, 12)}…` : '（无）';
}

const { copy } = useClipboard();
async function copyDigest(sha256: string) {
  await copy(sha256);
  ElMessage.success('digest 已复制到剪贴板');
}

/** 列表过滤表单：stage / os_family 都是精确匹配（契约 §4 GET /hooks） */
const filterFormSchema: VbenFormSchema[] = [
  {
    component: 'Select',
    componentProps: {
      clearable: true,
      options: (Object.keys(STAGE_LABEL) as HookStage[]).map((value) => ({
        label: STAGE_LABEL[value],
        value,
      })),
      placeholder: '按阶段过滤',
    },
    fieldName: 'stage',
    label: '阶段',
  },
  {
    component: 'Select',
    componentProps: {
      clearable: true,
      options: OS_FAMILY_OPTIONS.map((value) => ({ label: value, value })),
      placeholder: '按 OS 家族过滤',
    },
    fieldName: 'os_family',
    label: 'OS 家族',
  },
];

async function queryHooks(formValues: Record<string, any>) {
  const params: Record<string, string> = {};
  if (formValues.stage) params.stage = formValues.stage;
  if (formValues.os_family) params.os_family = formValues.os_family;
  const items = await requestClient.get<HookItem[]>('/hooks', { params });
  return { items };
}

const [Grid, gridApi] = useVbenVxeGrid<HookItem>({
  formOptions: {
    schema: filterFormSchema,
  },
  gridOptions: {
    columns: [
      { field: 'name', title: '名字', minWidth: 160 },
      {
        field: 'stage',
        title: '阶段',
        width: 160,
        slots: { default: 'stage' },
      },
      {
        field: 'state',
        title: '状态',
        width: 110,
        slots: { default: 'state' },
      },
      {
        field: 'osFamilies',
        title: '支持的家族',
        minWidth: 140,
        formatter: ({ cellValue }: { cellValue: string[] }) =>
          cellValue && cellValue.length > 0 ? cellValue.join(', ') : '不限',
      },
      { field: 'author', title: '作者', width: 120 },
      { field: 'version', title: '当前版本', width: 90 },
      {
        field: 'action',
        title: '操作',
        width: 300,
        fixed: 'right',
        slots: { default: 'action' },
      },
    ],
    height: 'auto',
    keepSource: true,
    proxyConfig: {
      ajax: {
        query: async (_params: any, formValues: Record<string, any>) =>
          queryHooks(formValues),
      },
    },
    rowConfig: { keyField: 'id' },
    toolbarConfig: { refresh: true, search: true },
  },
});

/** 任务 1：hook 列表与版本视图 —— 版本抽屉，digest 可完整复制 */
const versionRows = ref<HookItem[]>([]);
const [VersionModal, versionModalApi] = useVbenModal({
  onCancel() {
    versionModalApi.close();
  },
  title: '历史版本（按版本号升序）',
});

async function openVersions(row: HookItem) {
  versionModalApi.open();
  versionModalApi.setState({ loading: true });
  try {
    versionRows.value = await requestClient.get<HookItem[]>(
      `/hooks/${row.id}/versions`,
    );
  } finally {
    versionModalApi.setState({ loading: false });
  }
}

/** 任务 2：新建版本（=新建 hook 时 version=1，或对既有 hook 建新版本）与三级校验反馈 */
const isEditingExisting = ref(false);
const editingHookId = ref('');
const validateResult = ref<HookValidateResult | null>(null);
const hasValidatedCurrentBody = ref(false);

const editForm = ref({
  name: '',
  description: '',
  stage: 'post' as HookStage,
  osFamilies: [] as string[],
  interpreter: 'bash',
  body: '',
  paramsSchema: '{}',
  requiresFacts: '',
  requiresCapabilities: '',
  contractVersion: '>=1',
  required: true,
  timeoutSec: 300,
});

const [EditModal, editModalApi] = useVbenModal({
  async onConfirm() {
    if (!hasValidatedCurrentBody.value) {
      ElMessage.warning(
        '保存前必须先点击「校验」，且校验未通过错误级问题时不能保存',
      );
      return;
    }
    if (validateResult.value?.issues.some((i) => i.level === 'error')) {
      ElMessage.error('存在错误级校验问题，不能保存');
      return;
    }
    await submitHookVersion();
  },
  onCancel() {
    editModalApi.close();
  },
});

function resetEditForm() {
  editForm.value = {
    name: '',
    description: '',
    stage: 'post',
    osFamilies: [],
    interpreter: 'bash',
    body: '',
    paramsSchema: '{}',
    requiresFacts: '',
    requiresCapabilities: '',
    contractVersion: '>=1',
    required: true,
    timeoutSec: 300,
  };
  validateResult.value = null;
  hasValidatedCurrentBody.value = false;
}

function openCreateHook() {
  resetEditForm();
  isEditingExisting.value = false;
  editingHookId.value = '';
  editModalApi.setState({ title: '新建 hook（草稿）' });
  editModalApi.open();
}

/** 「基于此版本创建新版本」——不是编辑旧版本，是带入内容后另起一条 PUT 记录 */
function openNewVersion(row: HookItem) {
  editForm.value = {
    name: row.name,
    description: row.description,
    stage: row.stage,
    osFamilies: [...row.osFamilies],
    interpreter: row.interpreter,
    body: row.body,
    paramsSchema: row.paramsSchema || '{}',
    requiresFacts: row.requires?.facts?.join(',') ?? '',
    requiresCapabilities: row.requires?.capabilities?.join(',') ?? '',
    contractVersion: row.requires?.contractVersion ?? '>=1',
    required: row.required,
    timeoutSec: row.timeoutSec,
  };
  validateResult.value = null;
  hasValidatedCurrentBody.value = false;
  isEditingExisting.value = true;
  editingHookId.value = row.id;
  editModalApi.setState({
    title: `基于 ${row.name}（当前版本 v${row.version}）创建新版本——正文不可就地修改，保存会生成新版本与新 digest`,
  });
  editModalApi.open();
}

function buildHookPayload() {
  return {
    name: editForm.value.name,
    description: editForm.value.description,
    stage: editForm.value.stage,
    osFamilies: editForm.value.osFamilies,
    interpreter: editForm.value.interpreter,
    body: editForm.value.body,
    paramsSchema: editForm.value.paramsSchema,
    requires: {
      contractVersion: editForm.value.contractVersion,
      facts: editForm.value.requiresFacts
        ? editForm.value.requiresFacts.split(',').map((s) => s.trim())
        : [],
      capabilities: editForm.value.requiresCapabilities
        ? editForm.value.requiresCapabilities.split(',').map((s) => s.trim())
        : [],
    },
    required: editForm.value.required,
    timeoutSec: editForm.value.timeoutSec,
  };
}

async function runValidateOnDraft() {
  // 校验端点挂在已存在的 hook id 下（POST /hooks/{id}/validate）。
  // 新建场景先以草稿落地一次才能校验；建新版本场景先 PUT 出新记录再校验。
  // 两种场景都先落地、再校验、再视校验结果决定是否已经算「通过保存前校验」。
  const payload = buildHookPayload();
  const target: HookItem =
    isEditingExisting.value && editingHookId.value
      ? await requestClient.put<HookItem>(
          `/hooks/${editingHookId.value}`,
          payload,
        )
      : await requestClient.post<HookItem>('/hooks', payload);
  // 落地后的 id 成为后续校验与再次保存（若还要改）的目标。
  editingHookId.value = target.id;
  isEditingExisting.value = true;
  validateResult.value = await requestClient.post<HookValidateResult>(
    `/hooks/${target.id}/validate`,
  );
  hasValidatedCurrentBody.value = true;
  gridApi.reload();
  return target;
}

async function submitHookVersion() {
  // 校验通过（无 error 级问题）后，落地的记录已经是最终结果，这里只需关闭并刷新。
  editModalApi.close();
  gridApi.reload();
  ElMessage.success('已保存为新版本（草稿状态，需重新走审批）');
}

/** interpreter 会跳过语法检查的情形（M6 对 PowerShell / ash 的已知限制）——
 * 任务 2 要求这条提示必须显示，否则作者以为「检查通过」而实际没检查。 */
const skipsyntaxCheckNote = computed(() => {
  if (editForm.value.interpreter === 'powershell') {
    return 'PowerShell 解释器跳过 bash -n / shellcheck 语法检查（M6 未接入 PowerShell 静态检查工具），下方结果仅覆盖 requires/paramsSchema 校验。';
  }
  if (editForm.value.interpreter === 'ash') {
    return 'ash 解释器跳过 shellcheck（shellcheck 不支持 ash 方言），仅做 bash -n 兼容的基础语法检查与 requires/paramsSchema 校验。';
  }
  return '';
});

/** 任务 3：审批流界面 —— 完整正文 + 能力边界 + digest + 作者/时间 + 二次确认 + 驳回 */
const approvalTarget = ref<HookItem | null>(null);
const [ApprovalModal, approvalModalApi] = useVbenModal({
  onCancel() {
    approvalModalApi.close();
  },
  onConfirm() {
    confirmApprove();
  },
});

function openApproval(row: HookItem) {
  approvalTarget.value = row;
  approvalModalApi.setState({
    title: `审批 hook：${row.name} v${row.version}`,
  });
  approvalModalApi.open();
}

const isSelfApproval = computed(
  () =>
    !!approvalTarget.value &&
    approvalTarget.value.author === currentUsername.value,
);

async function confirmApprove() {
  const target = approvalTarget.value;
  if (!target) return;
  if (isSelfApproval.value) {
    ElMessage.error('审批人不能是作者本人，服务端会拒绝此操作');
    return;
  }
  try {
    await ElMessageBox.confirm(
      h('div', [
        h('p', { class: 'mb-2 font-bold' }, STAGE_LABEL[target.stage]),
        h('p', STAGE_CAPABILITY[target.stage]),
        h('p', { class: 'mt-2' }, `digest：${target.bodySha256}`),
      ]),
      '再次确认：批准即授权上述能力',
      {
        confirmButtonText: '确认批准',
        cancelButtonText: '取消',
        type: 'warning',
      },
    );
  } catch {
    return;
  }
  await requestClient.post<HookItem>(`/hooks/${target.id}/approve`);
  ElMessage.success('已推进审批状态');
  approvalModalApi.close();
  gridApi.reload();
}

async function advanceToReview(row: HookItem) {
  // draft -> review 允许作者自己发起，不需要二次确认弹窗那套能力披露
  // （能力披露是审批动作的要求，不是提交审批的要求）。
  await requestClient.post<HookItem>(`/hooks/${row.id}/approve`);
  ElMessage.success('已提交审批（draft → review）');
  gridApi.reload();
}

const rejectReason = ref('');
const [RejectModal, rejectModalApi] = useVbenModal({
  async onConfirm() {
    const target = approvalTarget.value;
    if (!target) return;
    if (!rejectReason.value.trim()) {
      ElMessage.error('驳回理由不能为空');
      return;
    }
    if (isSelfApproval.value) {
      ElMessage.error('审批人不能是作者本人，服务端会拒绝此操作');
      return;
    }
    const result = await requestClient.post<HookRejectResult>(
      `/hooks/${target.id}/reject`,
      { reason: rejectReason.value.trim() },
    );
    rejectModalApi.close();
    gridApi.reload();
    // 已知缺口如实告知操作者：理由目前只在这次响应里可见，没有查询 API，
    // 不能暗示「理由会留在某处给作者看」——服务端的 AuditLogger 只写不读。
    await ElMessageBox.alert(
      h('div', [
        h('p', { class: 'mb-2' }, `已驳回，状态回退为草稿。理由：`),
        h('p', { class: 'font-bold' }, result.reason),
        h(
          'p',
          { class: 'mt-2 text-sm text-gray-500' },
          '注意：此理由目前只在这次驳回操作的响应里可见，系统没有为作者提供事后查询驳回理由的入口。请通过其它渠道（如线下沟通）把这条理由告知作者。',
        ),
      ]),
      '驳回已提交',
      { confirmButtonText: '知道了' },
    );
  },
  onCancel() {
    rejectModalApi.close();
  },
});

function openReject(row: HookItem) {
  approvalTarget.value = row;
  rejectReason.value = '';
  rejectModalApi.setState({ title: `驳回 hook：${row.name} v${row.version}` });
  rejectModalApi.open();
}
</script>

<template>
  <Page
    description="hook 以 root 身份在分区之前运行，pre 阶段能力等同「擦掉这台机器所有盘」——审批前请完整阅读正文与能力说明。"
    title="hook 管理与审批"
  >
    <template #extra>
      <ElButton type="primary" @click="openCreateHook">新建 hook</ElButton>
    </template>

    <Grid>
      <template #stage="{ row }">
        <ElTag :type="row.stage === 'pre' ? 'danger' : 'info'" effect="dark">
          {{ STAGE_LABEL[row.stage as HookStage] }}
        </ElTag>
      </template>
      <template #state="{ row }">
        <ElTag
          :effect="row.state === 'review' ? 'dark' : 'light'"
          :type="STATE_TAG_TYPE[row.state as HookState]"
        >
          {{ STATE_LABEL[row.state as HookState] }}
        </ElTag>
      </template>
      <template #action="{ row }">
        <ElButton link size="small" @click="openVersions(row)">版本</ElButton>
        <ElButton link size="small" @click="openNewVersion(row)">
          新建版本
        </ElButton>
        <ElButton
          v-if="row.state === 'draft'"
          link
          size="small"
          type="primary"
          @click="advanceToReview(row)"
        >
          提交审批
        </ElButton>
        <ElButton
          v-if="row.state === 'review'"
          link
          size="small"
          type="success"
          @click="openApproval(row)"
        >
          审批
        </ElButton>
        <ElButton
          v-if="row.state === 'review'"
          link
          size="small"
          type="danger"
          @click="openReject(row)"
        >
          驳回
        </ElButton>
      </template>
    </Grid>

    <!-- 任务 1：版本视图，digest 可完整复制 -->
    <VersionModal class="w-[720px]">
      <div
        v-for="v in versionRows"
        :key="v.version"
        class="mb-3 border-b border-gray-200 pb-2"
      >
        <div class="flex items-center gap-2">
          <ElTag size="small">v{{ v.version }}</ElTag>
          <ElTag :type="STATE_TAG_TYPE[v.state]" size="small">
            {{ STATE_LABEL[v.state] }}
          </ElTag>
          <span class="font-mono text-sm">{{ shortDigest(v.bodySha256) }}</span>
          <ElButton link size="small" @click="copyDigest(v.bodySha256)">
            复制完整 digest
          </ElButton>
        </div>
        <div class="mt-1 flex flex-wrap gap-4 text-sm text-gray-500">
          <span>作者：{{ v.author || '（无）' }}</span>
          <span>审批人：{{ v.approver || '（无）' }}</span>
          <span>
            审批时间：{{
              v.approvedAt
                ? new Date(v.approvedAt * 1000).toLocaleString()
                : '（未审批）'
            }}
          </span>
        </div>
      </div>
    </VersionModal>

    <!-- 任务 2：新建版本表单 + 三级校验反馈 -->
    <EditModal class="w-[820px]" confirm-text="保存为新版本">
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="mb-1 block text-sm">名字</label>
          <ElInput v-model="editForm.name" />
        </div>
        <div>
          <label class="mb-1 block text-sm">阶段</label>
          <ElSelect v-model="editForm.stage" class="w-full">
            <ElOption
              v-for="(label, value) in STAGE_LABEL"
              :key="value"
              :label="label"
              :value="value"
            />
          </ElSelect>
        </div>
        <div>
          <label class="mb-1 block text-sm">解释器</label>
          <ElSelect v-model="editForm.interpreter" class="w-full">
            <ElOption
              v-for="opt in INTERPRETER_OPTIONS"
              :key="opt"
              :label="opt"
              :value="opt"
            />
          </ElSelect>
        </div>
        <div>
          <label class="mb-1 block text-sm">支持的 OS 家族（空=不限）</label>
          <ElSelect v-model="editForm.osFamilies" class="w-full" multiple>
            <ElOption
              v-for="opt in OS_FAMILY_OPTIONS"
              :key="opt"
              :label="opt"
              :value="opt"
            />
          </ElSelect>
        </div>
        <div class="col-span-2">
          <label class="mb-1 block text-sm">描述</label>
          <ElInput v-model="editForm.description" />
        </div>
        <div class="col-span-2">
          <label class="mb-1 block text-sm">正文</label>
          <ElInput v-model="editForm.body" :rows="8" type="textarea" />
        </div>
        <div class="col-span-2">
          <!-- §12.7 子集约束：提前告知，而不是保存时才失败 -->
          <label class="mb-1 block text-sm">
            参数 schema（JSON，须落在 §12.7 定义的子集内：仅支持
            string/number/boolean/enum 等基础类型与 required 列表，不支持嵌套
            object/自定义 format）
          </label>
          <ElInput v-model="editForm.paramsSchema" :rows="3" type="textarea" />
        </div>
        <div>
          <label class="mb-1 block text-sm">requires.facts（逗号分隔）</label>
          <ElInput v-model="editForm.requiresFacts" />
        </div>
        <div>
          <label class="mb-1 block text-sm">
            requires.capabilities（逗号分隔）
          </label>
          <ElInput v-model="editForm.requiresCapabilities" />
        </div>
      </div>

      <div
        v-if="skipsyntaxCheckNote"
        class="mt-3 rounded bg-blue-50 p-2 text-sm text-blue-700"
      >
        提示：{{ skipsyntaxCheckNote }}
      </div>

      <div class="mt-4 flex items-center gap-2">
        <ElButton type="primary" @click="runValidateOnDraft">校验</ElButton>
        <span v-if="!hasValidatedCurrentBody" class="text-sm text-gray-500">
          未校验，保存按钮不会生效
        </span>
      </div>

      <div v-if="validateResult" class="mt-3">
        <div
          v-for="(issue, idx) in validateResult.issues"
          :key="idx"
          :class="
            issue.level === 'error'
              ? 'border-red-300 bg-red-50 text-red-700'
              : 'border-yellow-300 bg-yellow-50 text-yellow-700'
          "
          class="mb-1 rounded border p-2 text-sm"
        >
          [{{
            issue.level === 'error'
              ? '错误（阻止保存）'
              : '警告（可保存，需注意）'
          }}]
          {{ issue.message }}
        </div>
        <div
          v-if="validateResult.issues.length === 0"
          class="rounded border border-green-300 bg-green-50 p-2 text-sm text-green-700"
        >
          校验通过，无错误或警告。
        </div>
      </div>
    </EditModal>

    <!-- 任务 3：审批弹窗——完整正文、能力边界、digest、作者/时间、二次确认 -->
    <ApprovalModal
      class="w-[820px]"
      :confirm-disabled="isSelfApproval"
      confirm-text="确认批准"
    >
      <template v-if="approvalTarget">
        <div
          class="mb-3 rounded border border-red-300 bg-red-50 p-3 text-red-700"
        >
          <div class="font-bold">
            {{ STAGE_LABEL[approvalTarget.stage] }}
          </div>
          <div class="mt-1">{{ STAGE_CAPABILITY[approvalTarget.stage] }}</div>
        </div>
        <div
          v-if="isSelfApproval"
          class="mb-3 rounded border border-red-400 bg-red-100 p-2 font-bold text-red-800"
        >
          你是这个 hook 的作者，不能审批自己提交的 hook（服务端会拒绝）。
        </div>
        <ElDescriptions :column="2" border>
          <ElDescriptionsItem label="作者">
            {{ approvalTarget.author }}
          </ElDescriptionsItem>
          <ElDescriptionsItem label="版本">
            v{{ approvalTarget.version }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :span="2" label="digest">
            <span class="font-mono">{{ approvalTarget.bodySha256 }}</span>
            <ElButton
              link
              size="small"
              @click="copyDigest(approvalTarget.bodySha256)"
            >
              复制
            </ElButton>
          </ElDescriptionsItem>
        </ElDescriptions>
        <div class="mt-3">
          <div class="mb-1 text-sm font-bold">
            完整正文（审批人必读，不折叠）
          </div>
          <pre
            class="max-h-80 overflow-auto rounded bg-gray-900 p-3 text-sm text-gray-100"
            >{{ approvalTarget.body }}</pre>
        </div>
      </template>
    </ApprovalModal>

    <!-- 驳回弹窗：理由必填，提交后原样回显并说明可查性缺口 -->
    <RejectModal class="w-[520px]" :confirm-disabled="isSelfApproval">
      <label class="mb-1 block text-sm">驳回理由（必填）</label>
      <ElInput
        v-model="rejectReason"
        :rows="4"
        type="textarea"
        placeholder="作者需要靠这段文字知道问题所在"
      />
    </RejectModal>
  </Page>
</template>
