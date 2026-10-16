<script setup lang="ts">
import { ref } from 'vue';
import { ElButton,ElEmpty,ElAlert } from 'element-plus';
import { useRouter } from 'vue-router';
import { useAuthStore } from '#/store';
import { refreshAccess } from '#/access/refresh';
const auth=useAuthStore(),router=useRouter(),error=ref('');
async function logout(){try{await auth.logout(false);}catch{error.value='退出失败，请检查网络后重试';}}
</script>
<template><main class="p-8"><ElEmpty description="暂无可访问菜单，请联系管理员配置角色菜单" /><ElAlert v-if="error" :title="error" type="error" /><div class="flex justify-center gap-3"><ElButton @click="refreshAccess">重新读取权限</ElButton><ElButton @click="router.push('/account/password')">修改密码</ElButton><ElButton @click="logout">退出登录</ElButton></div></main></template>
