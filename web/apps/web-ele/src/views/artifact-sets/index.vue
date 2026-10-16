<script setup lang="ts">
import type { ISO } from '#/api/console/operations';
import { reactive, ref } from 'vue';
import {
  ElAlert,
  ElButton,
  ElCard,
  ElDialog,
  ElInput,
  ElMessageBox,
  ElTable,
  ElTableColumn,
} from 'element-plus';
import { operations } from '#/api/console/operations';
import { requestClient } from '#/api/request';
import AccessButton from '#/components/access-button.vue';
import { useConsoleAccess } from '#/composables/use-console-access';
import { errorMessage, useConsoleList } from '#/composables/use-console-list';
const { can, run } = useConsoleAccess();
const { items, next, loading, error, load } = useConsoleList((cursor) =>
  operations.isos({ cursor, limit: 100 }),
);
const editor = ref(false),
  saving = ref(false),
  progress = ref('');
const form = reactive({
  name: '',
  family: 'linux',
  series: 'rocky',
  kind: 'path',
  addr: '',
  sha256: '',
});
const file = ref<File | null>(null);
async function save() {
  if (saving.value || !can('iso:write')) return;
  saving.value = true;
  try {
    if (form.kind === 'upload' && !file.value)
      throw new Error('请选择 ISO 文件');
    const result = await run('iso:write', () =>
      operations.createISO({
        name: form.name,
        family: form.family,
        series: form.series,
        sources: [
          {
            kind: form.kind,
            addr: form.kind === 'upload' ? undefined : form.addr,
            size: file.value?.size,
            sha256: form.sha256 || undefined,
          },
        ],
      }),
    );
    if (result) {
      if (form.kind === 'upload' && file.value) {
        const selected = file.value;
        const size = 4 * 1024 * 1024;
        for (let start = 0; start < selected.size; start += size) {
          if (!can('iso:write')) throw new Error('当前已没有上传权限');
          const end = Math.min(start + size, selected.size);
          await run('iso:write', () =>
            requestClient.put(
              `/isos/${result.id}/content`,
              selected.slice(start, end),
              {
                headers: {
                  'Content-Type': 'application/octet-stream',
                  'Content-Range': `bytes ${start}-${end - 1}/${selected.size}`,
                },
              },
            ),
          );
          progress.value = `上传 ${Math.round((end / selected.size) * 100)}%`;
        }
      }
      editor.value = false;
      await load();
    }
  } catch (e) {
    error.value = errorMessage(e);
  } finally {
    saving.value = false;
  }
}
async function action(
  item: ISO,
  op: 'cancel' | 'retry' | 'reimport' | 'deprecate' | 'delete',
) {
  try {
    await ElMessageBox.confirm(
      `确认${({ cancel: '取消获取', retry: '重试获取', reimport: '重新导入', deprecate: '废弃', delete: '删除' } as const)[op]}「${item.name}」？`,
      '确认操作',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  try {
    if (
      (await run('iso:write', () =>
        op === 'delete'
          ? operations.deleteISO(item)
          : operations.isoAction(item, op),
      )) !== undefined
    )
      await load();
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function showProgress(item: ISO) {
  try {
    const result = await operations.isoProgress(item.id);
    progress.value = JSON.stringify(result, null, 2);
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function addSource(item: ISO) {
  try {
    const role = await ElMessageBox.prompt(
      '附加来源角色，例如 netboot；主介质留空',
      '补充来源',
    );
    const addr = await ElMessageBox.prompt(
      '服务器可读取的本地路径',
      '补充本地来源',
    );
    if (
      (await run('iso:write', () =>
        operations.registerSource(item.id, {
          kind: 'path',
          role: role.value,
          addr: addr.value,
        }),
      )) !== undefined
    )
      await load();
  } catch (e: any) {
    if (e !== 'cancel' && e !== 'close') error.value = errorMessage(e);
  }
}
</script>
<template>
  <main class="p-6">
    <header class="mb-5 flex items-center gap-3">
      <h1 class="mr-auto text-2xl font-semibold">制品集</h1>
      <ElButton :loading="loading" @click="load()">刷新状态</ElButton
      ><AccessButton
        permission="iso:write"
        type="primary"
        @click="editor = true"
        >登记制品集</AccessButton
      >
    </header>
    <ElAlert
      v-if="error"
      class="mb-4"
      :title="error"
      type="error"
      :closable="false"
    /><ElCard
      ><ElTable :data="items" row-key="id"
        ><ElTableColumn
          prop="name"
          label="名称"
          min-width="160"
        /><ElTableColumn prop="series" label="系列" /><ElTableColumn
          prop="version"
          label="版本"
        /><ElTableColumn prop="arch" label="架构" /><ElTableColumn
          prop="state"
          label="状态"
        /><ElTableColumn label="操作" min-width="300"
          ><template #default="{ row }"
            ><ElButton link @click="showProgress(row as ISO)"
              >来源与进度</ElButton
            ><AccessButton
              v-if="row.state === 'acquiring'"
              permission="iso:write"
              link
              @click="action(row as ISO, 'cancel')"
              >取消获取</AccessButton
            ><AccessButton
              v-if="row.state === 'failed'"
              permission="iso:write"
              link
              @click="action(row as ISO, 'retry')"
              >重试获取</AccessButton
            ><AccessButton
              v-if="row.state === 'failed'"
              permission="iso:write"
              link
              @click="addSource(row as ISO)"
              >补充来源</AccessButton
            ><AccessButton
              v-if="row.state === 'failed'"
              permission="iso:write"
              link
              @click="action(row as ISO, 'reimport')"
              >重新导入</AccessButton
            ><AccessButton
              v-if="row.state === 'ready'"
              permission="iso:write"
              link
              @click="action(row as ISO, 'deprecate')"
              >废弃</AccessButton
            ><AccessButton
              v-if="row.state === 'deprecated' || row.state === 'failed'"
              permission="iso:write"
              link
              type="danger"
              @click="action(row as ISO, 'delete')"
              >删除</AccessButton
            ></template
          ></ElTableColumn
        ></ElTable
      ><ElButton v-if="next" class="mt-4" @click="load(true)"
        >加载更多</ElButton
      ></ElCard
    ><ElCard v-if="progress" class="mt-5">
      <pre class="whitespace-pre-wrap text-xs">{{ progress }}</pre></ElCard
    ><ElDialog
      v-model="editor"
      title="登记制品集"
      width="min(600px,95vw)"
      :close-on-click-modal="!saving"
      ><form @submit.prevent="save" class="grid gap-4">
        <label>名称<ElInput v-model="form.name" /></label
        ><label
          >系统族<select
            v-model="form.family"
            class="ml-3 rounded border bg-transparent p-2"
          >
            <option>linux</option>
            <option>windows</option>
            <option>esxi</option>
          </select></label
        ><label
          >系统系列<ElInput
            v-model="form.series"
            placeholder="例如 rocky、debian、ubuntu" /></label
        ><label
          >来源<select
            v-model="form.kind"
            class="ml-3 rounded border bg-transparent p-2"
          >
            <option value="path">服务器本地文件</option>
            <option value="url">下载地址</option>
            <option value="upload">浏览器上传</option>
          </select></label
        ><label v-if="form.kind !== 'upload'"
          >{{ form.kind === 'path' ? '服务器本地路径' : '下载地址'
          }}<ElInput v-model="form.addr" /></label
        ><label v-else
          >ISO 文件<input
            type="file"
            accept=".iso"
            @change="
              file = ($event.target as HTMLInputElement).files?.[0] || null
            " /></label
        ><label>SHA256（可选）<ElInput v-model="form.sha256" /></label>
        <div>
          <AccessButton
            permission="iso:write"
            native-type="submit"
            type="primary"
            :loading="saving"
            >登记并导入</AccessButton
          ><ElButton :disabled="saving" @click="editor = false">取消</ElButton>
        </div>
      </form></ElDialog
    >
  </main>
</template>
