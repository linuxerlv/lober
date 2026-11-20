<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';

import { ElAlert, ElButton, ElCard } from 'element-plus';

import { sessionHome } from '#/access/policy';
import { accountsApi, roleLabels } from '#/api/console/accounts';
import { useAuthStore } from '#/store';
const auth = useAuthStore();
  const router = useRouter();
const form = reactive({
  displayName: '',
  email: '',
  avatar: '',
  bio: '',
  rev: 0,
});
const error = ref('');
  const loading = ref(false);
  const notice = ref('');
  const saving = ref(false);
async function load() {
  loading.value = true;
  error.value = '';
  notice.value = '';
  try {
    const u = await auth.refreshSessionUser();
    Object.assign(form, {
      displayName: u.displayName,
      email: u.email || '',
      avatar: u.avatar || '',
      bio: u.bio || '',
      rev: u.rev,
    });
  } catch (e: any) {
    error.value = e?.response?.data?.message || e.message || '读取个人资料失败';
  } finally {
    loading.value = false;
  }
}
async function avatar(event: Event) {
  const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
  if (!file) return;
  error.value = '';
  if (
    !['image/jpeg', 'image/png'].includes(file.type) ||
    file.size > 256 * 1024
  ) {
    error.value = '请选择不超过 256 KiB 的 PNG/JPEG 图片';
    input.value = '';
    return;
  }
  const reader = new FileReader();
  reader.onerror = () => {
    error.value = '图片读取失败';
  };
  reader.onload = () => {
    form.avatar = String(reader.result);
  };
  reader.readAsDataURL(file);
}
async function save() {
  if (saving.value || loading.value || !form.rev) return;
  saving.value = true;
  error.value = '';
  notice.value = '';
  try {
    await accountsApi.profile({ ...form });
    await auth.refreshSessionUser();
    form.rev = auth.sessionUser!.rev;
    notice.value = '个人资料已保存';
  } catch (e: any) {
    error.value =
      e?.response?.status === 409
        ? '资料已被修改，请重新读取后再保存；当前编辑已保留'
        : e?.response?.data?.message || e.message || '保存失败';
  } finally {
    saving.value = false;
  }
}
onMounted(load);
</script>
<template>
  <main class="profile-page">
    <ElCard>
<h1>个人设置</h1>
      <p class="muted">更新个人资料和登录密码</p>
      <ElAlert
        v-if="error"
        :title="error"
        type="error"
        :closable="false"
        role="alert"
      />
      <ElAlert
        v-if="notice"
        :title="notice"
        type="success"
        :closable="false"
        role="status"
      />
      <form @submit.prevent="save" class="profile-form">
        <label>用户名<input :value="auth.sessionUser?.username" readonly /></label>
        <label>账号角色<input
            :value="auth.sessionUser ? roleLabels[auth.sessionUser.role] : ''"
            readonly
        /></label>
        <p class="muted">角色和权限由管理员分配。</p>
        <label>显示名称<input
            name="displayName"
            v-model="form.displayName"
            maxlength="100"
            :disabled="loading || saving"
        /></label>
        <label>邮箱<input
            name="email"
            v-model="form.email"
            type="email"
            maxlength="254"
            :disabled="loading || saving"
            placeholder="用于展示的联系邮箱"
        /></label>
        <label>头像<img
            v-if="form.avatar"
            :src="form.avatar"
            alt="个人头像预览"
            class="avatar"
/><input
            name="avatar"
            type="file"
            accept="image/png,image/jpeg"
            @change="avatar"
            :disabled="loading || saving"
        /></label>
        <p class="muted">PNG/JPEG，最大 256 KiB，宽高不超过 1024 像素。</p>
        <ElButton
          v-if="form.avatar"
          @click="form.avatar = ''"
          :disabled="saving"
          >
移除头像
</ElButton>
        <label>个人简介<textarea
            name="bio"
            v-model="form.bio"
            maxlength="500"
            rows="4"
            :disabled="loading || saving"
          ></textarea>
        </label>
        <div class="actions">
          <ElButton
            native-type="submit"
            type="primary"
            :loading="saving"
            :disabled="loading || !form.rev"
            >
保存资料
</ElButton><ElButton @click="load" :disabled="saving || loading">
重新读取
</ElButton><ElButton
            @click="router.push('/account/password')"
            :disabled="saving"
            >
修改密码
</ElButton>
        </div>
      </form>
      <ElButton
        class="back"
        @click="
          router.push(
            auth.sessionUser ? sessionHome(auth.sessionUser) : '/auth/login',
          )
        "
        >
返回控制台
</ElButton>
    </ElCard>
  </main>
</template>
<style scoped>
.profile-page {
  max-width: 760px;
  margin: 36px auto;
  padding: 20px;
}
h1 {
  font-size: 24px;
  font-weight: 600;
}
.muted {
  color: var(--el-text-color-secondary);
  margin: 8px 0 16px;
}
.profile-form {
  display: grid;
  gap: 16px;
  margin-top: 24px;
}
label {
  display: grid;
  gap: 8px;
  font-weight: 500;
}
input:not([type='file']),
textarea {
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  padding: 10px;
  background: var(--el-fill-color-blank);
  color: var(--el-text-color-primary);
}
input[readonly] {
  background: var(--el-fill-color-light);
}
.actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.avatar {
  width: 80px;
  height: 80px;
  border-radius: 50%;
  object-fit: cover;
}
.back {
  margin-top: 24px;
}
</style>
