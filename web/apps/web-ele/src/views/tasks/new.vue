<script lang="ts" setup>
/**
 * 建任务页 —— 唯一的授权动作，按危险动作做（SPEC §3.2、§12.5、§12.6）。
 *
 * 四步流程：选主机 → 选模板 → 动态表单 → 二次确认。
 *
 * 三条硬要求（任务文本、契约约定 3、§4.3、§4.7）：
 * 1. SN 字段用 host-select 组件从 GET /hosts 取值，不接受自由文本输入。
 * 2. 提交前弹二次确认，确认框内必须显示 SN、机架位、上次被发现时间——
 *    这三项是运维判断“这台是不是我要装的机器”的全部依据，删掉 armed 状态
 *    之后这是唯一的人工确认点。
 * 3. 请求体传主机清单 id，不传 SN 字符串——服务端据 id 取 SN 与 MAC 构成
 *    双因子（约定 3）。服务端会拒绝请求体里的 SN 字段，传了也是错，因此
 *    本页面自始至终不组装、不提交 sn/mac 字段。
 *
 * 建任务时按主机的 arch 与 firmware 过滤可选模板（SPEC §4.7 约束 3）：
 * BIOS 机器不能被分配 ESXi 任务。这是比引导时拦截更早也更友好的一道——
 * 引导时才拦，运维已经在机房了。过滤依据模板 -> artifactSetId -> 制品集
 * 的 arch/firmware 字段（模板本身不带 arch/firmware，需要联查制品集）。
 *
 * UI 上不存在 arm / disarm 按钮（task:arm 权限码已删除）。
 */
import type { VbenFormSchema } from '#/adapter/form';
import type { JsonSchemaForm } from '#/adapter/form-schema';
import type { ArtifactSetItem } from '#/api/console/artifact';
import type { HostListItem } from '#/api/console/host';
import type { TemplateItem } from '#/api/console/template';

import { computed, ref, shallowRef, watch } from 'vue';
import { useRouter } from 'vue-router';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElDescriptions,
  ElDescriptionsItem,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElStep,
  ElSteps,
} from 'element-plus';

import { useVbenForm } from '#/adapter/form';
import { convertJsonSchemaForm } from '#/adapter/form-schema';
import { getArtifactSetListApi } from '#/api/console/artifact';
import { createTaskApi } from '#/api/console/task';
import {
  getTemplateFormSchemaApi,
  getTemplateListApi,
} from '#/api/console/template';
import HostSelect from '#/components/host-select.vue';

defineOptions({ name: 'TaskNew' });

const router = useRouter();

// ---------------------------------------------------------------------------
// 步骤状态
// ---------------------------------------------------------------------------

const currentStep = ref(0);

// ---------------------------------------------------------------------------
// 步骤 1：选主机
// ---------------------------------------------------------------------------

/** 选中的主机清单 id——提交到服务端的字段，绝不是 SN 字符串（约定 3）。 */
const selectedHostId = ref<string>();
/** host-select 组件透出的完整主机记录，用于过滤模板与二次确认展示。 */
const selectedHost = shallowRef<HostListItem>();

function onHostChange(host: HostListItem | undefined) {
  selectedHost.value = host;
  // 换主机后模板与表单选择一律失效，避免残留一个对新主机不适配的模板。
  selectedTemplateId.value = undefined;
  formSchemaList.value = [];
}

// ---------------------------------------------------------------------------
// 步骤 2：选模板（按主机 arch / firmware 过滤，SPEC §4.7 约束 3）
// ---------------------------------------------------------------------------

/**
 * 契约缺口提示（供 contractGaps 汇报，非本文件独立发现——
 * src/api/console/template.ts 的 getTemplateListApi() 顶部注释已详细记录）：
 * docs/contracts/http-console.md 冻结的 28 个端点没有 `GET /templates` 列表
 * 端点，getTemplateListApi() 是该文件作者按 hosts/artifact-sets 同构的列表
 * 端点形态补齐的，运行时若后端未实现会报错——本页面据此显示
 * templateListLoadError，不静默展示空列表。
 */
const templateListLoading = ref(false);
const templateListLoadError = ref<string>();
const allTemplates = ref<TemplateItem[]>([]);
const allArtifactSets = ref<ArtifactSetItem[]>([]);
const selectedTemplateId = ref<string>();

/**
 * 判断一个制品集是否兼容目标主机的固件模式。
 *
 * `firmware = "any"` 表示该制品集不区分固件模式，两侧都视为兼容；否则要求
 * 两边精确相等——BIOS 机器不能被分配 UEFI-only 或 ESXi（通常固定 UEFI）的
 * 模板，这正是本约束要拦的组合。
 */
function isFirmwareCompatible(
  artifactFirmware: string,
  hostFirmware: string,
): boolean {
  return artifactFirmware === 'any' || artifactFirmware === hostFirmware;
}

/** 按选中主机的 arch/firmware 过滤出可用模板（仅保留 state=active 的模板，非 active 建任务会被服务端拒绝）。 */
const availableTemplates = computed(() => {
  const host = selectedHost.value;
  if (!host) {
    return [];
  }
  const artifactSetById = new Map(
    allArtifactSets.value.map((item) => [item.id, item]),
  );
  return allTemplates.value.filter((tpl) => {
    if (tpl.state !== 'active') {
      return false;
    }
    const artifactSet = artifactSetById.get(tpl.artifactSetId);
    if (!artifactSet) {
      // 制品集查不到（可能已被删除或数据不一致）时保守隐藏，不猜测兼容性。
      return false;
    }
    return (
      artifactSet.arch === host.arch &&
      isFirmwareCompatible(artifactSet.firmware, host.firmware)
    );
  });
});

async function loadTemplatesAndArtifactSets() {
  templateListLoading.value = true;
  templateListLoadError.value = undefined;
  try {
    const [templates, artifactSets] = await Promise.all([
      getTemplateListApi(),
      getArtifactSetListApi(),
    ]);
    allTemplates.value = templates;
    allArtifactSets.value = artifactSets;
  } catch (error) {
    templateListLoadError.value =
      error instanceof Error ? error.message : String(error);
  } finally {
    templateListLoading.value = false;
  }
}

// ---------------------------------------------------------------------------
// 步骤 3：动态表单（JSON Schema -> VbenFormSchema，任务 10 的转换层）
// ---------------------------------------------------------------------------

const formSchemaLoading = ref(false);
const formSchemaList = ref<VbenFormSchema[]>([]);
const formSchemaError = ref<string>();

const [DynamicForm, dynamicFormApi] = useVbenForm({
  layout: 'horizontal',
  showDefaultActions: false,
  schema: [],
});

// 整体替换动态表单的 schema：换模板后整份表单的字段集合都会变，因此用
// setState（与 updateSchema/removeSchemaByFields 内部使用的同一原语，见
// form-api.ts 源码）而不是逐字段 updateSchema——updateSchema 只按 fieldName
// 合并到已有字段上，无法增删字段（见 updateFormSchemaList 实现：遍历"当前"
// schema 逐条查找匹配的 fieldName 合并，不在列表里的新字段不会被加入）。
watch(formSchemaList, (schema) => {
  // @ts-expect-error TS2589：本文件与 useVbenForm/TanStack Form + Zod 4 的
  // 类型组合累计起来超出 vue-tsc 的实例化深度上限（已用最小复现单独验证过：
  // dynamicFormApi.setState({ schema }) 单独抽出到一个更小的 .vue 文件里
  // 类型检查通过，说明不是这一行本身的类型错误，是整份文件的类型复杂度
  // 累积触发的编译器限制）。setState({ schema }) 是 updateSchema/
  // removeSchemaByFields 内部使用的同一原语，运行时行为不受影响。若升级
  // TypeScript/TanStack Form 后这行不再报错，本行会变成"多余的 ts-expect-
  // error"而被编译器报错，从而强制到时候删掉这条抑制，不会长期隐藏真实的
  // 类型退化。
  dynamicFormApi.setState({ schema });
});

async function loadFormSchema(templateId: string) {
  formSchemaLoading.value = true;
  formSchemaError.value = undefined;
  try {
    const jsonSchema = await getTemplateFormSchemaApi(templateId);
    formSchemaList.value = convertJsonSchemaForm(
      jsonSchema as unknown as JsonSchemaForm,
    );
  } catch (error) {
    formSchemaError.value =
      error instanceof Error ? error.message : String(error);
  } finally {
    formSchemaLoading.value = false;
  }
}

watch(selectedTemplateId, async (templateId) => {
  formSchemaList.value = [];
  if (templateId) {
    await loadFormSchema(templateId);
  }
});

// ---------------------------------------------------------------------------
// 步骤导航
// ---------------------------------------------------------------------------

const canProceedFromStep1 = computed(() => Boolean(selectedHostId.value));
const canProceedFromStep2 = computed(() => Boolean(selectedTemplateId.value));

function goToStep2() {
  if (!canProceedFromStep1.value) {
    ElMessage.warning('请先选择目标主机');
    return;
  }
  currentStep.value = 1;
  if (allTemplates.value.length === 0) {
    void loadTemplatesAndArtifactSets();
  }
}

function goToStep3() {
  if (!canProceedFromStep2.value) {
    ElMessage.warning('请先选择安装模板');
    return;
  }
  currentStep.value = 2;
}

function backToStep(step: number) {
  currentStep.value = step;
}

// ---------------------------------------------------------------------------
// 步骤 4：二次确认与提交
// ---------------------------------------------------------------------------

const submitting = ref(false);

/** 格式化“上次被发现时间”，lastSeenAt 是 Unix 秒。 */
function formatLastSeenAt(lastSeenAt: number | undefined): string {
  if (!lastSeenAt) {
    return '从未';
  }
  return new Date(lastSeenAt * 1000).toLocaleString();
}

async function confirmAndSubmit() {
  const host = selectedHost.value;
  const templateId = selectedTemplateId.value;
  if (!host || !templateId) {
    return;
  }

  const { valid } = await dynamicFormApi.validate();
  if (!valid) {
    ElMessage.error('请检查表单填写内容');
    return;
  }
  // POST /tasks 的 formData 契约类型是 Record<string, string>，但动态表单里
  // switch/number 控件在 TanStack Form 里实际持有 boolean/number 值——这里
  // 显式转成字符串，不是简单 as 断言掩盖类型不一致（服务端按 SPEC §5.6 对
  // form_data 做类型白名单强校验，那些值会进 kickstart 脚本，传一个"看起来
  // 是 string 但实际是 boolean"的值会在服务端反序列化时出问题，不能留到
  // 那时候才发现）。
  const rawValues = await dynamicFormApi.getValues();
  const formData: Record<string, string> = Object.fromEntries(
    Object.entries(rawValues).map(([key, value]) => [key, String(value)]),
  );

  // 二次确认框必须显示 SN、机架位、上次被发现时间三项（任务文本硬要求、
  // SPEC §3.2）——这是删掉 armed 状态后唯一的人工确认点，且确认文案要写明
  // 后果：建任务后机器一旦网络启动就会开始装机，本系统没有远程关机手段
  // 可纠正（SPEC §3.2 残留风险）。
  try {
    await ElMessageBox.confirm(
      `序列号：${host.sn}\n机架位：${host.rack || '未记录'}\n上次被发现时间：${formatLastSeenAt(host.lastSeenAt)}\n\n确认后该主机一旦网络启动即会立即开始装机，本系统没有远程关机手段可以中途取消或纠正，请核对以上信息确实是目标机器。`,
      '建任务二次确认',
      {
        confirmButtonText: '确认建任务并授权装机',
        cancelButtonText: '取消',
        type: 'warning',
        dangerouslyUseHTMLString: false,
      },
    );
  } catch {
    // 用户取消，不提交。
    return;
  }

  submitting.value = true;
  try {
    // 请求体只传 hostId，绝不拼装/回传 sn 或 mac——服务端会拒绝请求体里的
    // SN 字段（契约约定 3），传了也是错。
    const task = await createTaskApi({
      hostId: host.id,
      templateId,
      formData,
    });
    ElMessage.success('任务已创建，机器上电后将自动开始装机');
    await router.push(`/tasks/${task.id}`);
  } catch (error) {
    // 服务端的 message 是给人看的中文错误说明（apierr 约定），直接展示，
    // 不做二次翻译——例如“该主机已有进行中的装机任务”这类，翻译一遍反而
    // 可能失真。errorMessageResponseInterceptor 已经会弹出提示，这里仅
    // 兜底避免 submitting 状态卡住。
    ElMessage.error(
      error instanceof Error ? error.message : '建任务失败，请重试',
    );
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="p-4">
    <ElCard>
      <ElSteps :active="currentStep" finish-status="success" align-center>
        <ElStep title="选主机" />
        <ElStep title="选模板" />
        <ElStep title="填写参数并确认" />
      </ElSteps>

      <!-- 步骤 1：选主机 -->
      <div v-if="currentStep === 0" class="mt-6">
        <ElAlert
          type="info"
          :closable="false"
          title="SN 字段必须从已发现主机清单选取，不接受自由文本输入"
          class="mb-4"
        />
        <HostSelect v-model="selectedHostId" @host-change="onHostChange" />
        <div class="mt-6 flex justify-end">
          <ElButton
            type="primary"
            :disabled="!canProceedFromStep1"
            @click="goToStep2"
          >
            下一步
          </ElButton>
        </div>
      </div>

      <!-- 步骤 2：选模板（已按主机 arch/firmware 过滤） -->
      <div v-else-if="currentStep === 1" class="mt-6">
        <ElAlert
          v-if="selectedHost"
          type="info"
          :closable="false"
          :title="`目标主机：${selectedHost.sn}（${selectedHost.arch} / ${selectedHost.firmware}），下拉仅显示固件与架构兼容的已启用模板`"
          class="mb-4"
        />
        <ElAlert
          v-if="templateListLoadError"
          type="error"
          :closable="false"
          title="加载模板列表失败（疑似契约缺口：契约未定义 GET /templates 列表端点，详见本文件顶部注释）"
          :description="templateListLoadError"
          class="mb-4"
        />
        <ElSelect
          v-model="selectedTemplateId"
          placeholder="请选择安装模板"
          filterable
          class="w-full"
          :loading="templateListLoading"
        >
          <ElOption
            v-for="tpl in availableTemplates"
            :key="tpl.id"
            :label="tpl.name"
            :value="tpl.id"
          />
        </ElSelect>
        <ElAlert
          v-if="
            !templateListLoading &&
            !templateListLoadError &&
            availableTemplates.length === 0
          "
          type="warning"
          :closable="false"
          title="该主机的架构/固件模式没有兼容的已启用模板，请先补充制品集或模板"
          class="mt-4"
        />
        <div class="mt-6 flex justify-between">
          <ElButton @click="backToStep(0)">上一步</ElButton>
          <ElButton
            type="primary"
            :disabled="!canProceedFromStep2"
            @click="goToStep3"
          >
            下一步
          </ElButton>
        </div>
      </div>

      <!-- 步骤 3：动态表单 + 二次确认提交 -->
      <div v-else class="mt-6">
        <ElDescriptions title="目标主机" :column="2" border class="mb-4">
          <ElDescriptionsItem label="序列号">
            {{ selectedHost?.sn }}
          </ElDescriptionsItem>
          <ElDescriptionsItem label="机架位">
            {{ selectedHost?.rack || '未记录' }}
          </ElDescriptionsItem>
          <ElDescriptionsItem label="上次被发现时间">
            {{ formatLastSeenAt(selectedHost?.lastSeenAt) }}
          </ElDescriptionsItem>
          <ElDescriptionsItem label="架构 / 固件">
            {{ selectedHost?.arch }} / {{ selectedHost?.firmware }}
          </ElDescriptionsItem>
        </ElDescriptions>

        <ElAlert
          v-if="formSchemaError"
          type="error"
          :closable="false"
          title="加载表单结构失败"
          :description="formSchemaError"
          class="mb-4"
        />
        <DynamicForm v-if="formSchemaList.length > 0" />
        <ElAlert
          v-else-if="!formSchemaLoading && !formSchemaError"
          type="info"
          :closable="false"
          title="该模板没有额外的表单字段"
        />

        <div class="mt-6 flex justify-between">
          <ElButton @click="backToStep(1)">上一步</ElButton>
          <ElButton
            type="danger"
            :loading="submitting"
            @click="confirmAndSubmit"
          >
            确认建任务并授权装机
          </ElButton>
        </div>
      </div>
    </ElCard>
  </div>
</template>
