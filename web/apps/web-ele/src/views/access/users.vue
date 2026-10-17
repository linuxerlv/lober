<script setup lang="ts">
import type { ConsoleSessionUser, ConsoleRole } from '#/api/console/session';
import { computed, onMounted, reactive, ref } from 'vue';
import { ElAlert, ElButton, ElCard } from 'element-plus';
import { accountsApi, roleLabels } from '#/api/console/accounts';
import { useConsoleAccess } from '#/composables/use-console-access';
import { useAuthStore } from '#/store';
const auth = useAuthStore(),
  { can, refresh } = useConsoleAccess();
const allowed = computed(
  () => auth.sessionUser?.role === 'admin' && can('user:manage'),
);
const items = ref<ConsoleSessionUser[]>([]),
  cursor = ref(''),
  loading = ref(false),
  saving = ref(false),
  editing = ref(false),
  error = ref(''),
  notice = ref('');
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
async function load(more = false) {
  if (!allowed.value) return;
  loading.value = true;
  error.value = '';
  try {
    const page = await accountsApi.list(more ? cursor.value : '');
    items.value = more ? [...items.value, ...page.items] : page.items;
    cursor.value = page.next;
  } catch (e: any) {
    error.value = e?.response?.data?.message || e.message || '读取账号失败';
  } finally {
    loading.value = false;
  }
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
    <ElCard v-else
      ><h1>账号管理</h1>
      <p class="muted">为每个账号分配角色；角色菜单在“角色与菜单”中设置。</p>
      <ElAlert
        v-if="error"
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
        <ElButton type="primary" @click="edit()" :disabled="saving"
          >创建账号</ElButton
        ><ElButton @click="load()" :disabled="loading || saving"
          >刷新列表</ElButton
        >
      </div>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>用户名</th>
              <th>显示名称</th>
              <th>角色</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="user in items" :key="user.username">
              <td>{{ user.username }}</td>
              <td>{{ user.displayName || '—' }}</td>
              <td>{{ roleLabels[user.role] }}</td>
              <td>{{ user.disabled ? '已禁用' : '正常' }}</td>
              <td>
                <ElButton @click="edit(user)" :disabled="saving">编辑</ElButton>
              </td>
            </tr>
            <tr v-if="!items.length">
              <td colspan="5">{{ loading ? '正在加载…' : '暂无账号' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <ElButton v-if="cursor" @click="load(true)" :loading="loading"
        >加载更多</ElButton
      >
      <form v-if="editing" @submit.prevent="save" class="account-form">
        <h2>{{ form.rev ? '编辑账号' : '创建账号' }}</h2>
        <label
          >用户名<input
            name="username"
            v-model="form.username"
            :readonly="!!form.rev"
            :disabled="saving"
            required
            pattern="[A-Za-z0-9_]+"
            maxlength="64"
        /></label>
        <label
          >显示名称<input
            name="displayName"
            v-model="form.displayName"
            maxlength="100"
            :disabled="saving"
        /></label>
        <label
          >角色<select name="role" v-model="form.role" :disabled="saving">
            <option v-for="(label, key) in roleLabels" :key="key" :value="key">
              {{ label }}
            </option>
          </select></label
        >
        <p class="muted">{{ descriptions[form.role] }}</p>
        <label
          >{{ form.rev ? '重置密码（留空保留原密码）' : '初始密码'
          }}<input
            name="password"
            v-model="form.password"
            type="password"
            autocomplete="new-password"
            :required="!form.rev"
            :disabled="saving"
        /></label>
        <label v-if="form.rev" class="check"
          ><input
            name="disabled"
            type="checkbox"
            v-model="form.disabled"
            :disabled="saving || form.username === auth.sessionUser?.username"
          />禁用账号（使已有登录失效）</label
        >
        <p class="muted">
          不能禁用本人，也不能停用或降级最后一个管理员。重置密码后该账号的已有登录全部失效。
        </p>
        <div class="actions">
          <ElButton native-type="submit" type="primary" :loading="saving"
            >保存账号</ElButton
          ><ElButton
            @click="
              editing = false;
              form.password = '';
            "
            :disabled="saving"
            >取消</ElButton
          >
        </div>
      </form>
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
h2 {
  font-size: 18px;
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
.table-wrap {
  overflow-x: auto;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  text-align: left;
  border-bottom: 1px solid var(--el-border-color-lighter);
  padding: 14px 12px;
}
.account-form {
  display: grid;
  gap: 14px;
  max-width: 600px;
  padding-top: 28px;
}
label {
  display: grid;
  gap: 8px;
}
input:not([type='checkbox']),
select {
  padding: 10px;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  background: var(--el-fill-color-blank);
  color: var(--el-text-color-primary);
}
input[readonly] {
  background: var(--el-fill-color-light);
}
.check {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
