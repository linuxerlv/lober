<script setup lang="ts">
import type { ConsoleRole, ConsoleSessionUser } from '#/api/console/session';

import { computed, onMounted, reactive, ref } from 'vue';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElOption,
  ElSelect,
  ElSwitch,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import { accountsApi, roleLabels } from '#/api/console/accounts';
import ListPagination from '#/components/console/ListPagination.vue';
import { NumberedPage } from '#/composables/numbered-page';
import { useConsoleAccess } from '#/composables/use-console-access';
import { useAuthStore } from '#/store';
const auth = useAuthStore();
const { can, refresh } = useConsoleAccess();
const allowed = computed(
  () => auth.sessionUser?.role === 'admin' && can('user:manage'),
);
const page = reactive(new NumberedPage<ConsoleSessionUser>());
const editing = ref(false);
const error = ref('');
const items = computed(() => page.items);
const loading = computed(() => page.loading);
const notice = ref('');
const saving = ref(false);
const form = reactive({
  username: '',
  displayName: '',
  role: 'viewer' as ConsoleRole,
  password: '',
  disabled: false,
  rev: 0,
});
const descriptions: Record<ConsoleRole, string> = {
  viewer: '读取业务数据，不能执行写入操作',
  operator: '主机维护、任务创建与控制',
  'hook-author': '编写并提交脚本和模板',
  'hook-approver': '审核脚本和模板，不能编写',
  admin: '管理账号、角色分配、菜单及全部业务操作',
};
async function load() {
  if (!allowed.value) return;
  error.value = '';
  try {
    await page.load((pagination) => accountsApi.list(pagination));
  } catch (e: any) {
    error.value = e?.response?.data?.message || e.message || '读取账号失败';
  }
}
function changePage(value: number) {
  page.setPage(value);
  void load();
}
function changePageSize(value: number) {
  page.setPageSize(value);
  void load();
}
function edit(user?: ConsoleSessionUser) {
  Object.assign(
    form,
    user
      ? {
          username: user.username,
          displayName: user.displayName,
          role: user.role,
          password: '',
          disabled: user.disabled,
          rev: user.rev,
        }
      : {
          username: '',
          displayName: '',
          role: 'viewer',
          password: '',
          disabled: false,
          rev: 0,
        },
  );
  editing.value = true;
  error.value = '';
  notice.value = '';
}
function closeEditor() {
  if (saving.value) return;
  editing.value = false;
  form.password = '';
  error.value = '';
}
function beforeCloseEditor(done: () => void) {
  if (saving.value) return;
  closeEditor();
  done();
}
async function save() {
  if (!allowed.value || saving.value) return;
  saving.value = true;
  error.value = '';
  notice.value = '';
  try {
    const isNew = !form.rev;
    if (isNew)
      await accountsApi.create({
        username: form.username,
        displayName: form.displayName,
        role: form.role,
        password: form.password,
      });
    else
      await accountsApi.update(form.username, {
        displayName: form.displayName,
        role: form.role,
        disabled: form.disabled,
        rev: form.rev,
        ...(form.password ? { password: form.password } : {}),
      });
    const self = form.username === auth.sessionUser?.username;
    form.password = '';
    editing.value = false;
    notice.value = isNew ? '账号已创建' : '账号已更新';
    if (self) await refresh();
    await load();
  } catch (e: any) {
    error.value =
      e?.response?.status === 409
        ? '账号已变更或操作不允许，请重新读取核对；当前编辑已保留'
        : e?.response?.data?.message || e.message || '保存失败';
  } finally {
    saving.value = false;
  }
}
onMounted(() => load());
</script>
<template>
  <div class="accounts-page">
    <ElCard v-if="!allowed"><p>仅管理员可以管理账号及分配角色。</p></ElCard>
    <ElCard v-else>
      <h1>账号管理</h1>
      <p class="muted">为每个账号分配角色；角色菜单在“角色与菜单”中设置。</p>
      <ElAlert
        v-if="error && !editing"
        :title="error"
        type="error"
        :closable="false"
        role="alert"
      /><ElAlert
        v-if="notice"
        :title="notice"
        type="success"
        :closable="false"
        role="status"
      />
      <div class="actions">
        <ElButton type="primary" @click="edit()" :disabled="saving">
          创建账号
        </ElButton>
        <ElButton @click="load()" :disabled="loading || saving">
          刷新列表
        </ElButton>
      </div>
      <ElTable
        :data="items"
        row-key="username"
        :empty-text="loading ? '正在加载…' : '暂无账号'"
      >
        <ElTableColumn prop="username" label="用户名" min-width="160" />
        <ElTableColumn prop="displayName" label="显示名称" min-width="160">
          <template #default="{ row }">{{ row.displayName || '—' }}</template>
        </ElTableColumn>
        <ElTableColumn label="角色" min-width="180">
          <template #default="{ row }">
            <ElTag>{{ roleLabels[row.role as ConsoleRole] }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="110">
          <template #default="{ row }">
            <ElTag :type="row.disabled ? 'danger' : 'success'">
              {{ row.disabled ? '已禁用' : '正常' }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="操作" width="100">
          <template #default="{ row }">
            <ElButton
              @click="edit(row as ConsoleSessionUser)"
              :disabled="saving"
            >
              编辑
            </ElButton>
          </template>
        </ElTableColumn>
      </ElTable>
      <ListPagination
        :current-page="page.page"
        :page-size="page.pageSize"
        :total="page.total"
        :disabled="loading || saving"
        @page-change="changePage"
        @size-change="changePageSize"
      />
      <ElDialog
        v-model="editing"
        :title="form.rev ? '编辑账号' : '创建账号'"
        width="min(600px, calc(100vw - 32px))"
        top="8vh"
        append-to-body
        destroy-on-close
        :close-on-click-modal="false"
        :close-on-press-escape="!saving"
        :show-close="!saving"
        :before-close="beforeCloseEditor"
      >
        <ElAlert
          v-if="error"
          :title="error"
          type="error"
          :closable="false"
          role="alert"
          class="mb-4"
        />
        <ElForm
          id="account-editor-form"
          :model="form"
          :disabled="saving"
          label-position="top"
          @submit.prevent="save"
          class="account-form"
        >
          <ElFormItem label="用户名">
            <ElInput
              name="username"
              v-model="form.username"
              :readonly="!!form.rev"
              :disabled="saving"
              required
              pattern="[A-Za-z0-9_]+"
              maxlength="64"
            />
          </ElFormItem>
          <ElFormItem label="显示名称">
            <ElInput
              name="displayName"
              v-model="form.displayName"
              maxlength="100"
              :disabled="saving"
            />
          </ElFormItem>
          <ElFormItem label="角色">
            <ElSelect name="role" aria-label="角色" v-model="form.role">
              <ElOption
                v-for="(label, key) in roleLabels"
                :key="key"
                :value="key"
                :label="label"
              />
            </ElSelect>
          </ElFormItem>
          <p class="muted">{{ descriptions[form.role] }}</p>
          <ElFormItem
            :label="form.rev ? '重置密码（留空保留原密码）' : '初始密码'"
          >
            <ElInput
              name="password"
              v-model="form.password"
              type="password"
              autocomplete="new-password"
              :required="!form.rev"
              :disabled="saving"
            />
          </ElFormItem>
          <ElFormItem v-if="form.rev" label="账号状态">
            <ElSwitch
              name="disabled"
              v-model="form.disabled"
              :disabled="saving || form.username === auth.sessionUser?.username"
              active-text="禁用账号（使已有登录失效）"
            />
          </ElFormItem>
          <p class="muted">
            不能禁用本人，也不能停用或降级最后一个管理员。重置密码后该账号的已有登录全部失效。
          </p>
        </ElForm>
        <template #footer>
          <ElButton @click="closeEditor" :disabled="saving">取消</ElButton>
          <ElButton
            native-type="submit"
            form="account-editor-form"
            type="primary"
            :loading="saving"
            :disabled="saving"
          >
            保存账号
          </ElButton>
        </template>
      </ElDialog>
    </ElCard>
  </div>
</template>
<style scoped>
.accounts-page {
  padding: 24px;
}
h1 {
  font-size: 24px;
  font-weight: 600;
}
.muted {
  color: var(--el-text-color-secondary);
  margin: 10px 0;
}
.actions {
  display: flex;
  gap: 8px;
  margin: 16px 0;
}
.account-form {
  width: 100%;
}
</style>
