<script setup lang="ts">
import type { User, UserCreate } from '#/api/console/user';

import { computed, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';
import { useAccessStore, useUserStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCheckbox,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessageBox,
  ElOption,
  ElSelect,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { errorMessage, problemsOf } from '#/api/console/problem';
import {
  createUser,
  disableUser,
  listUsers,
  updateUser,
} from '#/api/console/user';
import ProblemList from '#/components/console/ProblemList.vue';

const access = useAccessStore();
const current = useUserStore();
const allowed = computed(() => access.accessCodes.includes('user:manage'));
const failure = ref<unknown>();
const cursor = ref('');
const next = ref('');
const history = ref<string[]>([]);
let generation = 0;
let latestItems: User[] = [];
const [Grid, grid] = useVbenVxeGrid<User>({
  gridOptions: {
    columns: [
      { field: 'username', title: '用户名', minWidth: 160 },
      { field: 'displayName', title: '显示名', minWidth: 160 },
      { field: 'role', title: '角色', width: 150 },
      { field: 'disabled', title: '已禁用', width: 90 },
      { field: 'rev', title: '修订', width: 80 },
      { title: '操作', width: 180, slots: { default: 'action' } },
    ],
    pagerConfig: { enabled: false },
    rowConfig: { keyField: 'username' },
    proxyConfig: {
      ajax: {
        query: async () => {
          const run = ++generation;
          try {
            const page = await listUsers({ cursor: cursor.value, limit: 50 });
            if (run === generation) {
              next.value = page.next;
              latestItems = page.items ?? [];
            }
            return { items: latestItems };
          } catch (error) {
            if (run === generation) failure.value = error;
            return { items: latestItems };
          }
        },
      },
    },
  },
});
function refresh() {
  cursor.value = '';
  history.value = [];
  failure.value = undefined;
  void grid.reload();
}
function forward() {
  history.value.push(cursor.value);
  cursor.value = next.value;
  void grid.reload();
}
function back() {
  cursor.value = history.value.pop() ?? '';
  void grid.reload();
}
const open = ref(false);
const editing = ref<User>();
const busy = ref(false);
const form = reactive<UserCreate>({
  username: '',
  displayName: '',
  role: 'viewer',
  password: '',
  disabled: false,
});
const roles = [
  'viewer',
  'operator',
  'hook-author',
  'hook-approver',
  'admin',
] as const;
function edit(user?: User) {
  editing.value = user;
  Object.assign(form, {
    username: user?.username ?? '',
    displayName: user?.displayName ?? '',
    role: user?.role ?? 'viewer',
    password: '',
    disabled: user?.disabled ?? false,
  });
  failure.value = undefined;
  open.value = true;
}
async function save() {
  if (
    !allowed.value ||
    busy.value ||
    !form.username ||
    (!editing.value && !form.password)
  )
    return;
  busy.value = true;
  failure.value = undefined;
  try {
    if (editing.value)
      await updateUser(editing.value.username, {
        ...form,
        rev: editing.value.rev,
      });
    else await createUser({ ...form });
    open.value = false;
    refresh();
  } catch (error) {
    failure.value = error;
  } finally {
    busy.value = false;
  }
}
async function disable(user: User) {
  try {
    await ElMessageBox.confirm(
      `确认禁用用户 ${user.username} 并使其会话失效？`,
      '禁用用户',
    );
  } catch {
    return;
  }
  try {
    await disableUser(user.username);
    refresh();
  } catch (error) {
    failure.value = error;
  }
}
</script>
<template>
  <Page
    title="用户管理"
    description="用户角色、密码与启用状态；更新按服务端修订号保存。"
  >
    <ElAlert
      v-if="failure"
      :title="errorMessage(failure)"
      type="error"
      :closable="false"
      class="mb-3"
    />
    <ProblemList :problems="problemsOf(failure)" />
    <div class="mb-3 flex gap-2">
      <ElButton v-if="allowed" type="primary" @click="edit()">新建用户</ElButton><ElButton @click="refresh">刷新</ElButton><ElButton :disabled="!history.length" @click="back">上一页</ElButton><ElButton :disabled="!next" @click="forward">下一页</ElButton>
    </div>
    <Grid>
<template #action="{ row }">
<ElButton v-if="allowed" link type="primary" @click="edit(row)">
编辑
</ElButton><ElButton
          v-if="allowed"
          link
          type="danger"
          :disabled="
            row.disabled || row.username === current.userInfo?.username
          "
          @click="disable(row)"
          >
禁用
</ElButton>
</template>
</Grid>
    <ElDialog
      v-model="open"
      :title="editing ? '编辑用户' : '新建用户'"
      width="520px"
    >
      <ElForm label-width="100px" @submit.prevent="save">
<ElFormItem label="用户名">
<ElInput v-model="form.username" :disabled="!!editing" />
</ElFormItem><ElFormItem label="显示名">
<ElInput v-model="form.displayName" />
</ElFormItem><ElFormItem label="角色">
<ElSelect v-model="form.role">
<ElOption
              v-for="role in roles"
              :key="role"
              :value="role"
              :label="role"
/>
</ElSelect>
</ElFormItem><ElFormItem :label="editing ? '重置密码' : '密码'">
<ElInput
            v-model="form.password"
            type="password"
            show-password
            :placeholder="
              editing ? '留空保留密码' : '请输入初始密码'
            "
/>
</ElFormItem><ElFormItem label="状态">
<ElCheckbox
            v-model="form.disabled"
            :disabled="form.username === current.userInfo?.username"
            >
禁用
</ElCheckbox>
</ElFormItem>
</ElForm>
      <template #footer>
<ElButton @click="open = false">取消</ElButton><ElButton type="primary" :loading="busy" @click="save">
保存
</ElButton>
</template>
    </ElDialog>
  </Page>
</template>
