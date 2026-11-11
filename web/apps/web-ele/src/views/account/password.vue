<script setup lang="ts">
import { computed, ref } from 'vue';

import { Page } from '@vben/common-ui';
import { useUserStore } from '@vben/stores';

import { ElAlert, ElButton, ElForm, ElFormItem, ElInput } from 'element-plus';

import { errorMessage, problemsOf } from '#/api/console/problem';
import { changePasswordApi } from '#/api/core/auth';
import ProblemList from '#/components/console/ProblemList.vue';
import { useAuthStore } from '#/store';

const userStore = useUserStore();
const auth = useAuthStore();
const currentPassword = ref('');
const password = ref('');
const confirmation = ref('');
const busy = ref(false);
const failure = ref<unknown>();
const matches = computed(
  () => !!currentPassword.value && !!password.value && password.value === confirmation.value,
);
async function save() {
  if (!matches.value || busy.value) return;
  busy.value = true;
  failure.value = undefined;
  try {
    await changePasswordApi({
      currentPassword: currentPassword.value,
      newPassword: password.value,
    });
    await auth.clearSession(false);
  } catch (error) {
    failure.value = error;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <Page title="修改密码" description="修改成功后请使用新密码重新登录。">
    <ElAlert
      v-if="userStore.userInfo?.passwordChangeRequired"
      title="首次登录必须先修改密码。"
      type="warning"
      :closable="false"
      class="mb-4"
    />
    <ElAlert
      v-if="failure"
      :title="errorMessage(failure)"
      type="error"
      :closable="false"
    /><ProblemList :problems="problemsOf(failure)" />
    <ElForm class="max-w-lg" label-width="100px" @submit.prevent="save">
      <ElFormItem label="当前密码">
        <ElInput
          v-model="currentPassword"
          type="password"
          autocomplete="current-password"
          show-password
        />
      </ElFormItem>
<ElFormItem label="新密码">
<ElInput
          v-model="password"
          type="password"
          autocomplete="new-password"
          show-password
/>
</ElFormItem><ElFormItem label="确认密码">
<ElInput
          v-model="confirmation"
          type="password"
          autocomplete="new-password"
          show-password
/>
</ElFormItem><ElButton
        type="primary"
        :disabled="!matches"
        :loading="busy"
        @click="save"
        >
保存并重新登录
</ElButton>
</ElForm>
  </Page>
</template>
