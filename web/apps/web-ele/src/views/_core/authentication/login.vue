<script lang="ts" setup>
/**
 * 登录页（SPEC §12.5、§12.6）。对接 docs/contracts/http-console.md 的四个
 * 认证端点（POST /auth/login、GET /user/info、GET /auth/codes、
 * POST /auth/logout），实际调用发生在 src/store/auth.ts 的 authLogin，本文件
 * 只负责表单结构与展示。
 *
 * 相对上游模板改动两处：
 * 1. 删掉 selectAccount 模拟账号选择器与滑块验证码——两者都是上游演示脚手架，
 *    契约的 POST /auth/login 入参只有 username/password 两个字段，没有验证
 *    环节，留着假验证码只会让运维误以为这里有机器人防护而实际没有。
 * 2. 关闭注册 / 忘记密码 / 扫码登录三个入口——Foundation 已按 SPEC §12.2 删除
 *    对应页面与路由，留着链接点进去会 404。手机验证码登录（showCodeLogin）
 *    同样关闭，本系统不支持手机号登录。
 */
import type { VbenFormSchema } from '@vben/common-ui';

import { computed } from 'vue';

import { AuthenticationLogin, z } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { useAuthStore } from '#/store';

defineOptions({ name: 'Login' });

const authStore = useAuthStore();

const formSchema = computed((): VbenFormSchema[] => {
  return [
    {
      component: 'VbenInput',
      componentProps: {
        placeholder: $t('authentication.usernameTip'),
      },
      fieldName: 'username',
      label: $t('authentication.username'),
      rules: z.string().min(1, { message: $t('authentication.usernameTip') }),
    },
    {
      component: 'VbenInputPassword',
      componentProps: {
        placeholder: $t('authentication.password'),
      },
      fieldName: 'password',
      label: $t('authentication.password'),
      rules: z.string().min(1, { message: $t('authentication.passwordTip') }),
    },
  ];
});
</script>

<template>
  <AuthenticationLogin
    :form-schema="formSchema"
    :loading="authStore.loginLoading"
    :show-code-login="false"
    :show-forget-password="false"
    :show-qrcode-login="false"
    :show-register="false"
    :show-third-party-login="false"
    @submit="authStore.authLogin"
  />
</template>
