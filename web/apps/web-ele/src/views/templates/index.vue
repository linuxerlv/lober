<script lang="ts" setup>
/**
 * 模板列表页（SPEC §12.6、契约「5. 模板」端点 17-20）。
 *
 * 布局：左侧列表（编辑、建新版本），右侧常驻 preview 面板——SPEC §12.6 末段
 * 明确要求 preview 要给显眼入口，不能埋进二级菜单，UI 不给入口等于白做
 * （§8.2：把「建任务 → 上电 → 等数分钟 → 失败 → 翻日志」的反馈周期压到
 * 秒级）。因此这里不做"点开才弹出"的 modal，选中一行后 preview 面板直接
 * 在右侧渲染结果。
 *
 * 契约缺口（如实记录，非本文件发明）：docs/contracts/http-console.md 冻结
 * 的 28 个端点里没有 GET /templates 列表端点（只有 POST 建版本 / PUT 建
 * 新版本 / GET form-schema / POST preview 四个按 id 操作的端点）。
 * src/views/tasks/new.vue 与 src/api/console/template.ts 已经各自记录过
 * 这个缺口并假定存在一个同构的列表端点；本页面沿用同一假定直接调用
 * `GET /templates`，一旦后端未实现会在 Integrate 阶段的集成验证中因请求
 * 报错而显性暴露，不静默假装数据存在。
 *
 * 模板正文（answerTemplate/hookRefs 等）不可就地修改（SPEC §2.7 与 hook
 * 同源的不可变原则）：编辑 = PUT 建新版本，不是覆盖旧版本，因此本页的
 * "编辑"按钮语义是"基于当前版本创建新版本"，与 src/views/hooks/index.vue
 * 的 openNewVersion 同构。
 *
 * 列表页统一用 useVbenVxeGrid（§12.6 末句），本地类型 + requestClient 直连
 * + proxyConfig.ajax.query 把裸数组包成 { items }（契约约定 2）。
 */
import { ref, shallowRef } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElInput,
  ElMessage,
  ElOption,
  ElSelect,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { requestClient } from '#/api/request';

defineOptions({ name: 'TemplatesIndex' });

/** 对应 model.DiskPolicy（authoring.go），五个取值 */
type DiskPolicy = 'by-hook' | 'by-model' | 'first' | 'largest' | 'raid1-ssd';
/** 对应 model.OnFailurePolicy（authoring.go），四个取值 */
type OnFailurePolicy = 'halt' | 'keep-network' | 'reboot' | 'shell';
/** 对应 model.TemplateState */
type TemplateState = 'active' | 'deprecated' | 'draft';

interface TemplateHookRef {
  hookDigest: string;
  hookId: string;
  order: number;
  paramsDefaults: Record<string, unknown>;
  required: boolean;
  stage: string;
}

/** 模板条目，字段名与 model.Template 对应（契约「5. 模板」节）。 */
interface TemplateItem {
  answerTemplate: string;
  artifactSetId: string;
  diskPolicy: DiskPolicy;
  hookRefs: TemplateHookRef[];
  id: string;
  kernelArgs: string;
  name: string;
  onFailure: OnFailurePolicy;
  requiredFormFields: string[];
  state: TemplateState;
  version: number;
}

interface TemplatePreviewResult {
  answerFile: string;
  bootMenu: string;
  warnings: string[];
}

const DISK_POLICY_OPTIONS: DiskPolicy[] = [
  'first',
  'largest',
  'raid1-ssd',
  'by-model',
  'by-hook',
];
const ON_FAILURE_OPTIONS: OnFailurePolicy[] = [
  'reboot',
  'halt',
  'shell',
  'keep-network',
];

const STATE_TAG_TYPE: Record<TemplateState, 'danger' | 'info' | 'success'> = {
  draft: 'info',
  active: 'success',
  deprecated: 'danger',
};

const STATE_LABEL: Record<TemplateState, string> = {
  draft: '草稿',
  active: '已激活',
  deprecated: '已停用',
};

async function queryTemplates() {
  const items = await requestClient.get<TemplateItem[]>('/templates');
  return { items };
}

const [Grid, gridApi] = useVbenVxeGrid<TemplateItem>({
  gridOptions: {
    columns: [
      { field: 'name', title: '名字', minWidth: 160 },
      { field: 'artifactSetId', title: '制品集 id', minWidth: 160 },
      { field: 'diskPolicy', title: '磁盘策略', width: 110 },
      { field: 'onFailure', title: '失败处理', width: 120 },
      { field: 'version', title: '当前版本', width: 90 },
      {
        field: 'state',
        title: '状态',
        width: 100,
        slots: { default: 'state' },
      },
      {
        field: 'action',
        title: '操作',
        width: 200,
        fixed: 'right',
        slots: { default: 'action' },
      },
    ],
    height: 'auto',
    keepSource: true,
    proxyConfig: {
      ajax: { query: async () => queryTemplates() },
    },
    rowConfig: { keyField: 'id' },
    toolbarConfig: { refresh: true },
  },
});

// ---------------------------------------------------------------------------
// 新建 / 编辑（PUT 建新版本，正文不可就地修改，与 hook 同源）
// ---------------------------------------------------------------------------

const isEditingExisting = ref(false);
const editingTemplateId = ref('');

const editForm = ref({
  name: '',
  artifactSetId: '',
  answerTemplate: '',
  kernelArgs: '',
  diskPolicy: 'first' as DiskPolicy,
  onFailure: 'reboot' as OnFailurePolicy,
  requiredFormFields: '',
  hookRefsJson: '[]',
});

function resetEditForm() {
  editForm.value = {
    name: '',
    artifactSetId: '',
    answerTemplate: '',
    kernelArgs: '',
    diskPolicy: 'first',
    onFailure: 'reboot',
    requiredFormFields: '',
    hookRefsJson: '[]',
  };
}

function buildTemplatePayload() {
  let hookRefs: Array<
    Omit<TemplateHookRef, 'hookDigest'> & { hookDigest?: string }
  >;
  try {
    hookRefs = JSON.parse(editForm.value.hookRefsJson || '[]');
  } catch {
    throw new Error('hookRefs 不是合法 JSON');
  }
  return {
    name: editForm.value.name,
    artifactSetId: editForm.value.artifactSetId,
    answerTemplate: editForm.value.answerTemplate,
    kernelArgs: editForm.value.kernelArgs,
    diskPolicy: editForm.value.diskPolicy,
    onFailure: editForm.value.onFailure,
    requiredFormFields: editForm.value.requiredFormFields
      ? editForm.value.requiredFormFields.split(',').map((s) => s.trim())
      : [],
    hookRefs,
  };
}

const [EditModal, editModalApi] = useVbenModal({
  async onConfirm() {
    if (!editForm.value.name.trim() || !editForm.value.artifactSetId.trim()) {
      ElMessage.error('名字与制品集 id 均为必填');
      return;
    }
    let payload;
    try {
      payload = buildTemplatePayload();
    } catch (error) {
      ElMessage.error(
        error instanceof Error ? error.message : 'hookRefs 不是合法 JSON',
      );
      return;
    }
    try {
      if (isEditingExisting.value && editingTemplateId.value) {
        await requestClient.put<TemplateItem>(
          `/templates/${editingTemplateId.value}`,
          payload,
        );
        ElMessage.success('已保存为新版本（草稿状态）');
      } else {
        await requestClient.post<TemplateItem>('/templates', payload);
        ElMessage.success('已创建（草稿状态）');
      }
      editModalApi.close();
      gridApi.reload();
    } catch (error: any) {
      const message =
        error?.response?.data?.message || error?.message || '保存失败';
      ElMessage.error(message);
    }
  },
  onCancel() {
    editModalApi.close();
  },
});

function openCreate() {
  resetEditForm();
  isEditingExisting.value = false;
  editingTemplateId.value = '';
  editModalApi.setState({ title: '新建模板（草稿）' });
  editModalApi.open();
}

/** 编辑 = 基于当前版本创建新版本，不是就地修改（SPEC §2.7）。 */
function openNewVersion(row: TemplateItem) {
  editForm.value = {
    name: row.name,
    artifactSetId: row.artifactSetId,
    answerTemplate: row.answerTemplate,
    kernelArgs: row.kernelArgs,
    diskPolicy: row.diskPolicy,
    onFailure: row.onFailure,
    requiredFormFields: row.requiredFormFields?.join(',') ?? '',
    hookRefsJson: JSON.stringify(row.hookRefs ?? [], null, 2),
  };
  isEditingExisting.value = true;
  editingTemplateId.value = row.id;
  editModalApi.setState({
    title: `基于 ${row.name}（当前版本 v${row.version}）创建新版本——正文不可就地修改`,
  });
  editModalApi.open();
}

// ---------------------------------------------------------------------------
// 右侧常驻 preview 面板（§12.6 末段：显眼入口，不埋进二级菜单）
// ---------------------------------------------------------------------------

const selectedTemplate = shallowRef<TemplateItem>();
const previewHostId = ref('');
const previewFormDataJson = ref('{}');
const previewLoading = ref(false);
const previewResult = shallowRef<TemplatePreviewResult>();
const previewError = ref('');

function selectForPreview(row: TemplateItem) {
  selectedTemplate.value = row;
  previewResult.value = undefined;
  previewError.value = '';
}

async function runPreview() {
  const template = selectedTemplate.value;
  if (!template) {
    ElMessage.warning('请先在左侧列表选择一个模板');
    return;
  }
  let formData: Record<string, unknown>;
  try {
    formData = JSON.parse(previewFormDataJson.value || '{}');
  } catch {
    ElMessage.error('formData 不是合法 JSON');
    return;
  }
  previewLoading.value = true;
  previewError.value = '';
  try {
    previewResult.value = await requestClient.post<TemplatePreviewResult>(
      `/templates/${template.id}/preview`,
      {
        hostId: previewHostId.value.trim() || undefined,
        formData,
      },
    );
  } catch (error: any) {
    // 渲染失败返回 CodeInternal，message 带具体渲染错误，直接展示
    // （§8.2：这个端点存在的意义就是把反馈周期压到秒级，错误信息必须直接可读）。
    previewResult.value = undefined;
    previewError.value =
      error?.response?.data?.message || error?.message || '预览渲染失败';
  } finally {
    previewLoading.value = false;
  }
}
</script>

<template>
  <Page
    description="模板列表、编辑（=建新版本）；右侧常驻 preview 面板，选中任意模板即可秒级预览渲染结果（§8.2）。"
    title="模板管理"
  >
    <template #extra>
      <ElButton type="primary" @click="openCreate">新建模板</ElButton>
    </template>

    <div class="flex gap-4">
      <div class="min-w-0 flex-1">
        <Grid>
          <template #state="{ row }">
            <ElTag :type="STATE_TAG_TYPE[row.state as TemplateState]">
              {{ STATE_LABEL[row.state as TemplateState] }}
            </ElTag>
          </template>
          <template #action="{ row }">
            <ElButton link size="small" @click="selectForPreview(row)">
              预览
            </ElButton>
            <ElButton link size="small" @click="openNewVersion(row)">
              新建版本
            </ElButton>
          </template>
        </Grid>
      </div>

      <!-- 常驻 preview 面板：不做成 modal，选中即渲染，避免"入口不显眼"（§12.6）。 -->
      <ElCard class="w-[420px] shrink-0" shadow="never">
        <template #header>
          <span class="font-bold">Preview 面板</span>
        </template>

        <template v-if="selectedTemplate">
          <div class="mb-3 text-sm text-gray-500">
            模板：{{ selectedTemplate.name }}（v{{ selectedTemplate.version }}）
          </div>

          <label class="mb-1 block text-sm">
            主机 id（可选，不传则用服务端内置的假 Host）
          </label>
          <ElInput
            v-model="previewHostId"
            class="mb-3"
            placeholder="留空使用假数据"
          />

          <label class="mb-1 block text-sm">formData（JSON）</label>
          <ElInput
            v-model="previewFormDataJson"
            :rows="4"
            class="mb-3"
            type="textarea"
          />

          <ElButton
            :loading="previewLoading"
            type="primary"
            @click="runPreview"
          >
            渲染预览
          </ElButton>

          <ElAlert
            v-if="previewError"
            :closable="false"
            :title="previewError"
            class="mt-3"
            type="error"
          />

          <template v-if="previewResult">
            <div class="mt-4">
              <div class="mb-1 text-sm font-bold">应答文件</div>
              <pre
                class="max-h-60 overflow-auto rounded bg-gray-900 p-2 text-xs text-gray-100"
                >{{ previewResult.answerFile }}</pre>
            </div>
            <div class="mt-3">
              <div class="mb-1 text-sm font-bold">iPXE 引导菜单</div>
              <pre
                class="max-h-40 overflow-auto rounded bg-gray-900 p-2 text-xs text-gray-100"
                >{{ previewResult.bootMenu }}</pre>
            </div>
            <div v-if="previewResult.warnings.length > 0" class="mt-3">
              <div class="mb-1 text-sm font-bold text-yellow-600">提示</div>
              <div
                v-for="(warning, idx) in previewResult.warnings"
                :key="idx"
                class="text-sm text-yellow-700"
              >
                {{ warning }}
              </div>
            </div>
          </template>
        </template>
        <ElAlert
          v-else
          :closable="false"
          title="在左侧列表点击任意模板的「预览」即可在此渲染"
          type="info"
        />
      </ElCard>
    </div>

    <EditModal class="w-[820px]" confirm-text="保存为新版本">
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="mb-1 block text-sm">名字</label>
          <ElInput v-model="editForm.name" />
        </div>
        <div>
          <label class="mb-1 block text-sm">制品集 id</label>
          <ElInput v-model="editForm.artifactSetId" />
        </div>
        <div>
          <label class="mb-1 block text-sm">磁盘策略</label>
          <ElSelect v-model="editForm.diskPolicy" class="w-full">
            <ElOption
              v-for="opt in DISK_POLICY_OPTIONS"
              :key="opt"
              :label="opt"
              :value="opt"
            />
          </ElSelect>
        </div>
        <div>
          <label class="mb-1 block text-sm">失败处理</label>
          <ElSelect v-model="editForm.onFailure" class="w-full">
            <ElOption
              v-for="opt in ON_FAILURE_OPTIONS"
              :key="opt"
              :label="opt"
              :value="opt"
            />
          </ElSelect>
        </div>
        <div class="col-span-2">
          <label class="mb-1 block text-sm">kernelArgs</label>
          <ElInput v-model="editForm.kernelArgs" />
        </div>
        <div class="col-span-2">
          <label class="mb-1 block text-sm">requiredFormFields</label>
          <ElInput
            v-model="editForm.requiredFormFields"
            placeholder="逗号分隔"
          />
        </div>
        <div class="col-span-2">
          <label class="mb-1 block text-sm">answerTemplate</label>
          <ElInput
            v-model="editForm.answerTemplate"
            :rows="6"
            type="textarea"
          />
        </div>
        <div class="col-span-2">
          <label class="mb-1 block text-sm">
            hookRefs（JSON 数组，hookDigest 可留空由服务端按 hookId 查出当前
            active 版本自动填入）
          </label>
          <ElInput v-model="editForm.hookRefsJson" :rows="6" type="textarea" />
        </div>
      </div>
    </EditModal>
  </Page>
</template>
