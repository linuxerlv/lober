<script setup lang="ts">
import type { Authoring, Preview } from '#/api/console/operations';
import { computed, reactive, ref } from 'vue';
import {
  ElAlert,
  ElButton,
  ElCard,
  ElDialog,
  ElInput,
  ElMessageBox,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';
import { operations } from '#/api/console/operations';
import AccessButton from './access-button.vue';
import { useConsoleAccess } from '#/composables/use-console-access';
import { errorMessage, useConsoleList } from '#/composables/use-console-list';
import { useAuthStore } from '#/store';
const props = defineProps<{ kind: 'hooks' | 'templates' }>();
const singular = props.kind === 'hooks' ? 'hook' : 'template',
  title = props.kind === 'hooks' ? 'hook' : '模板';
const write = `${singular}:write`,
  approve = `${singular}:approve`;
const { can, run } = useConsoleAccess();
const auth = useAuthStore();
const query = ref('');
const { items, next, loading, error, load } = useConsoleList((cursor) =>
  operations.listAuthoring(props.kind, {
    prefix: query.value,
    cursor,
    limit: 100,
  }),
);
const selected = ref<Authoring | null>(null),
  versions = ref<Authoring[]>([]),
  preview = ref<Preview | null>(null),
  diagnostics = ref('');
const editor = ref(false),
  editing = ref<Authoring | null>(null),
  saving = ref(false);
const form = reactive({
  name: '',
  description: '',
  family: 'linux',
  series: 'rocky',
  kind: 'fragment',
  interpreter: 'sh',
  timings: 'post',
  body: '',
  params: '[]',
  bindings: '{}',
  assembly: '{}',
  timeoutSec: 300,
  required: true,
});
const selfApproval = computed(
  () => selected.value?.author === auth.sessionUser?.username,
);
function openEditor(item?: Authoring) {
  if (!can(write)) return;
  editing.value = item || null;
  const impl = item?.impls?.[0];
  Object.assign(form, {
    name: item?.name || '',
    description: item?.description || '',
    family: impl?.family || item?.family || 'linux',
    series: (impl?.series || item?.series || ['rocky']).join(','),
    kind: impl?.kind || 'fragment',
    interpreter: impl?.interpreter || 'sh',
    timings: (impl?.timings || ['post']).join(','),
    body: impl?.body || item?.body || '',
    params: JSON.stringify(item?.params || [], null, 2),
    bindings: JSON.stringify(item?.bindings || {}, null, 2),
    assembly: JSON.stringify(item?.assembly || {}, null, 2),
    timeoutSec: item?.timeoutSec || 300,
    required: item?.required ?? true,
  });
  editor.value = true;
}
async function save() {
  if (saving.value || !can(write)) return;
  saving.value = true;
  try {
    const series = form.series
        .split(',')
        .map((x) => x.trim())
        .filter(Boolean),
      params = JSON.parse(form.params);
    if (!Array.isArray(params)) throw new Error('参数声明必须为数组');
    const data: Record<string, unknown> =
      props.kind === 'hooks'
        ? {
            name: form.name,
            description: form.description,
            timeoutSec: form.timeoutSec,
            required: form.required,
            params,
            impls: [
              {
                family: form.family,
                series,
                kind: form.kind,
                body: form.body,
                interpreter: form.kind === 'script' ? form.interpreter : '',
                timings:
                  form.kind === 'script'
                    ? form.timings
                        .split(',')
                        .map((x) => x.trim())
                        .filter(Boolean)
                    : [],
              },
              ...(editing.value?.impls?.slice(1) || []),
            ],
          }
        : {
            name: form.name,
            family: form.family,
            series,
            body: form.body,
            params,
            bindings: JSON.parse(form.bindings),
            assembly: JSON.parse(form.assembly),
          };
    const item = await run(write, () =>
      editing.value
        ? operations.updateAuthoring(props.kind, editing.value, data)
        : operations.createAuthoring(props.kind, data),
    );
    if (item) {
      selected.value = item;
      editor.value = false;
      await load();
    }
  } catch (e) {
    error.value = errorMessage(e);
  } finally {
    saving.value = false;
  }
}
async function inspect(item: Authoring) {
  selected.value = item;
  preview.value = null;
  try {
    versions.value = (await operations.versions(props.kind, item)).items;
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function mutate(permission: string, action: () => Promise<unknown>) {
  try {
    const result = await run(permission, action);
    if (result !== undefined) {
      await load();
      diagnostics.value = '操作已完成';
    }
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function lifecycle(
  item: Authoring,
  action: 'submit' | 'approve' | 'reject' | 'deprecate',
) {
  const permission = action === 'submit' ? write : approve;
  if (!can(permission)) return;
  if (action === 'approve' && item.author === auth.sessionUser?.username) {
    error.value = '作者不能审批自己提交的版本';
    return;
  }
  let body: unknown;
  try {
    if (action === 'reject') {
      const prompt = await ElMessageBox.prompt('请填写驳回原因', '驳回版本', {
        inputValidator: (value) => !!value?.trim() || '请填写原因',
      });
      body = { reason: prompt.value };
    } else
      await ElMessageBox.confirm(
        `确认${({ submit: '提交审批', approve: '批准', deprecate: '废弃' } as const)[action]}「${item.name}」v${item.version}？`,
        '确认操作',
        { type: 'warning' },
      );
  } catch {
    return;
  }
  await mutate(permission, () =>
    operations.lifecycle(props.kind, item, action, body),
  );
}
async function validate(item: Authoring) {
  try {
    const result = await run(write, () =>
      operations.validate(props.kind, item),
    );
    if (result)
      diagnostics.value = result.ok
        ? '校验通过'
        : result.problems.map((p) => p.message).join('\n');
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function showPreview() {
  if (!selected.value) return;
  try {
    preview.value = await operations.preview(selected.value, {
      series: selected.value.series?.[0],
      firmware: 'uefi',
    });
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function removeDraft(item: Authoring) {
  try {
    await ElMessageBox.confirm(
      `确认删除草稿「${item.name}」v${item.version}？`,
      '删除草稿',
    );
  } catch {
    return;
  }
  await mutate(write, () => operations.deleteDraft(props.kind, item));
}
</script>
<template>
  <main class="p-6">
    <header class="mb-5 flex flex-wrap items-center gap-3">
      <h1 class="mr-auto text-2xl font-semibold">{{ title }}管理</h1>
      <ElInput
        v-model="query"
        class="!w-56"
        placeholder="按名称前缀搜索"
        @keyup.enter="load()"
      /><ElButton :loading="loading" @click="load()">查询</ElButton
      ><AccessButton :permission="write" type="primary" @click="openEditor()"
        >新建{{ title }}</AccessButton
      >
    </header>
    <ElAlert
      v-if="error"
      class="mb-4"
      :title="error"
      type="error"
      :closable="false"
    />
    <div class="grid gap-5 xl:grid-cols-[3fr_2fr]">
      <ElCard
        ><ElTable :data="items" row-key="id"
          ><ElTableColumn
            prop="name"
            label="名称"
            min-width="150"
          /><ElTableColumn
            prop="version"
            label="版本"
            width="70"
          /><ElTableColumn label="状态" width="110"
            ><template #default="{ row }"
              ><ElTag>{{ row.state }}</ElTag></template
            ></ElTableColumn
          ><ElTableColumn
            prop="author"
            label="作者"
            width="120"
          /><ElTableColumn label="操作" min-width="220"
            ><template #default="{ row }">
              <ElButton link @click="inspect(row as Authoring)"
                >详情与版本</ElButton
              >
              <AccessButton
                v-if="row.state === 'draft' && !row.builtin"
                :permission="write"
                link
                @click="openEditor(row as Authoring)"
                >编辑草稿</AccessButton
              >
              <AccessButton
                v-if="!row.builtin"
                :permission="write"
                link
                @click="
                  mutate(write, () =>
                    operations.newVersion(kind, row as Authoring),
                  )
                "
                >新建版本</AccessButton
              >
              <AccessButton
                v-if="row.state === 'draft' && !row.builtin"
                :permission="write"
                link
                @click="validate(row as Authoring)"
                >校验</AccessButton
              >
              <AccessButton
                v-if="row.state === 'draft' && !row.builtin"
                :permission="write"
                link
                @click="lifecycle(row as Authoring, 'submit')"
                >提交审批</AccessButton
              >
              <AccessButton
                v-if="row.state === 'review' && !row.builtin"
                :permission="approve"
                link
                :disabled="row.author === auth.sessionUser?.username"
                @click="lifecycle(row as Authoring, 'approve')"
                >批准</AccessButton
              >
              <AccessButton
                v-if="row.state === 'review' && !row.builtin"
                :permission="approve"
                link
                @click="lifecycle(row as Authoring, 'reject')"
                >驳回</AccessButton
              >
              <AccessButton
                v-if="row.state === 'approved' && !row.builtin"
                :permission="approve"
                link
                @click="lifecycle(row as Authoring, 'deprecate')"
                >废弃</AccessButton
              >
              <AccessButton
                v-if="row.state === 'draft' && !row.builtin"
                :permission="write"
                link
                type="danger"
                @click="removeDraft(row as Authoring)"
                >删除草稿</AccessButton
              >
            </template></ElTableColumn
          ></ElTable
        ><ElButton
          v-if="next"
          class="mt-4"
          :loading="loading"
          @click="load(true)"
          >加载更多</ElButton
        ></ElCard
      >
      <ElCard
        ><h2 class="mb-3 text-lg font-medium">
          详情{{ kind === 'templates' ? '与预览' : '' }}
        </h2>
        <template v-if="selected"
          ><p>
            {{ selected.name }} · v{{ selected.version }} · {{ selected.state }}
          </p>
          <p class="my-2 text-sm text-gray-500">
            作者：{{ selected.author }}　审批人：{{
              selected.approver || '尚未审批'
            }}
          </p>
          <p v-if="selfApproval" class="my-2 text-sm text-gray-500">
            当前用户是此版本作者，不能审批自己提交的版本。
          </p>
          <div class="my-3 flex flex-wrap gap-2">
            <ElButton
              v-for="version in versions"
              :key="version.version"
              size="small"
              @click="
                selected = version;
                preview = null;
              "
              >v{{ version.version }} · {{ version.state }}</ElButton
            >
          </div>
          <pre
            class="max-h-72 overflow-auto whitespace-pre-wrap rounded bg-gray-100 p-3 text-xs dark:bg-gray-900"
            >{{
              selected.body ||
              selected.impls?.map((impl) => impl.body).join('\n\n')
            }}</pre>
          <ElAlert
            v-for="problem in selected.problems"
            :key="problem.code + problem.message"
            class="mt-2"
            :title="problem.message"
            type="warning"
            :closable="false"
          /><ElButton
            v-if="kind === 'templates'"
            class="mt-3"
            @click="showPreview"
            >生成预览</ElButton
          ><template v-if="preview"
            ><ElAlert
              v-for="problem in preview.problems"
              :key="problem.code"
              class="mt-2"
              :title="problem.message"
              type="warning"
              :closable="false"
            />
            <section
              v-for="file in preview.files"
              :key="file.name"
              class="mt-4"
            >
              <h3>{{ file.name }}{{ file.sample ? '（示例）' : '' }}</h3>
              <pre
                class="max-h-96 overflow-auto whitespace-pre-wrap rounded bg-gray-100 p-3 text-xs dark:bg-gray-900"
                >{{ file.body }}</pre>
            </section></template
          ></template
        >
        <p v-else class="text-gray-500">选择一个版本查看正文和诊断。</p>
        <pre v-if="diagnostics" class="mt-4 whitespace-pre-wrap text-sm">{{
          diagnostics
        }}</pre>
      </ElCard>
    </div>
    <ElDialog
      v-model="editor"
      :title="editing ? '编辑草稿' : '新建' + title"
      width="min(780px,95vw)"
    >
      <form @submit.prevent="save" class="grid gap-4">
        <label>名称<ElInput v-model="form.name" /></label
        ><label v-if="kind === 'hooks'"
          >说明<ElInput v-model="form.description"
        /></label>
        <div class="flex gap-4">
          <label
            >系统族<select
              v-model="form.family"
              class="ml-2 rounded border bg-transparent p-2"
            >
              <option>linux</option>
              <option>windows</option>
              <option>esxi</option>
            </select></label
          ><label class="flex-1"
            >系统系列（逗号分隔）<ElInput v-model="form.series"
          /></label>
        </div>
        <div v-if="kind === 'hooks'" class="flex flex-wrap gap-4">
          <label
            >类型<select
              v-model="form.kind"
              class="ml-2 rounded border bg-transparent p-2"
            >
              <option value="fragment">配置片段</option>
              <option value="script">脚本</option>
            </select></label
          ><label v-if="form.kind === 'script'"
            >解释器<ElInput v-model="form.interpreter" /></label
          ><label v-if="form.kind === 'script'"
            >执行时机<ElInput
              v-model="form.timings"
              placeholder="post,firstboot"
          /></label>
        </div>
        <label
          >正文<ElInput v-model="form.body" type="textarea" :rows="12"
        /></label>
        <details>
          <summary class="cursor-pointer">参数和装配配置</summary>
          <label class="mt-3 block"
            >参数声明（JSON 数组）<ElInput
              v-model="form.params"
              type="textarea"
              :rows="5" /></label
          ><template v-if="kind === 'templates'"
            ><label class="mt-3 block"
              >参数绑定（JSON 对象）<ElInput
                v-model="form.bindings"
                type="textarea"
                :rows="4" /></label
            ><label class="mt-3 block"
              >hook 装配（JSON 对象）<ElInput
                v-model="form.assembly"
                type="textarea"
                :rows="5" /></label
          ></template>
        </details>
        <div>
          <AccessButton
            :permission="write"
            native-type="submit"
            type="primary"
            :loading="saving"
            >保存草稿</AccessButton
          ><ElButton @click="editor = false">取消</ElButton>
        </div>
      </form>
    </ElDialog>
  </main>
</template>
