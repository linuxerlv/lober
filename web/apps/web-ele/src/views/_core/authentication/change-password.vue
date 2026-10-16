<script setup lang="ts">
import { reactive, ref } from 'vue';
import { ElAlert, ElButton, ElCard, ElInput, ElMessage } from 'element-plus';
import { changePasswordApi } from '#/api';
import { useAuthStore } from '#/store';
const auth=useAuthStore();
const form=reactive({currentPassword:'',newPassword:'',confirmPassword:''});
const busy=ref(false),error=ref('');
async function submit(){
 if(busy.value)return;
 error.value='';
 if(!form.currentPassword||!form.newPassword){error.value='请填写当前密码和新密码';return;}
 if(form.newPassword!==form.confirmPassword){error.value='两次输入的新密码不一致';return;}
 if(form.currentPassword===form.newPassword){error.value='新密码不能与当前密码相同';return;}
 busy.value=true;
 try{
  await changePasswordApi({currentPassword:form.currentPassword,newPassword:form.newPassword});
  form.currentPassword='';form.newPassword='';form.confirmPassword='';
  ElMessage.success('密码已修改，请使用新密码登录');
  await auth.clearSession(false);
 }catch(e:any){error.value=e?.response?.data?.message||e?.message||'修改密码失败，请重试';}
 finally{busy.value=false;}
}
async function logout(){try{await auth.logout(false);}catch{error.value='退出失败，请检查网络后重试';}}
</script>
<template>
 <main class="mx-auto w-full max-w-lg p-6">
  <ElCard>
   <h1 class="mb-4 text-xl font-semibold">修改密码</h1>
   <p class="mb-4 text-sm text-gray-500">修改成功后所有登录会话都会失效，请使用新密码重新登录。</p>
   <ElAlert v-if="auth.sessionUser?.passwordChangeRequired" class="mb-4" type="warning" :closable="false" title="首次登录需要修改初始密码" />
   <ElAlert v-if="error" class="mb-4" type="error" :closable="false" :title="error" role="alert" />
   <form @submit.prevent="submit">
    <label class="mb-4 block">当前密码<ElInput v-model="form.currentPassword" name="currentPassword" type="password" show-password autocomplete="current-password" :disabled="busy" /></label>
    <label class="mb-4 block">新密码<ElInput v-model="form.newPassword" name="newPassword" type="password" show-password autocomplete="new-password" :disabled="busy" /></label>
    <label class="mb-4 block">确认新密码<ElInput v-model="form.confirmPassword" name="confirmPassword" type="password" show-password autocomplete="new-password" :disabled="busy" /></label>
    <ElButton native-type="submit" type="primary" :loading="busy">修改并重新登录</ElButton>
    <ElButton :disabled="busy" @click="logout">退出登录</ElButton>
   </form>
  </ElCard>
 </main>
</template>
