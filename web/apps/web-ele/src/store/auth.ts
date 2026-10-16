import type { Recordable } from '@vben/types';
import type { ConsoleSessionUser } from '#/api/console/session';
import { ref, shallowRef } from 'vue';
import { useRouter } from 'vue-router';
import { LOGIN_PATH } from '@vben/constants';
import { useAccessStore,useUserStore,useTabbarStore } from '@vben/stores';
import { defineStore } from 'pinia';
import { getUserInfoApi,loginApi,logoutApi } from '#/api';
import { toFrameworkUser } from '#/api/console/session';
import { sessionHome } from '#/access/policy';
export const useAuthStore=defineStore('auth',()=>{
 const access=useAccessStore(),users=useUserStore(),tabs=useTabbarStore(),router=useRouter();
 const loginLoading=ref(false),sessionUser=shallowRef<ConsoleSessionUser|null>(null);
 let generation=0,pending:{token:string;request:Promise<ConsoleSessionUser>}|null=null;
 function applySessionUser(user:ConsoleSessionUser){sessionUser.value=user;users.setUserInfo(toFrameworkUser(user));access.setAccessCodes([...user.permissions]);}
 async function refreshSessionUser():Promise<ConsoleSessionUser>{
  const token=access.accessToken;if(!token)throw new Error('当前没有有效会话');
  if(pending?.token===token)return pending.request;
  const version=generation;
  const request=getUserInfoApi().then(user=>{if(generation===version&&access.accessToken===token)applySessionUser(user);return user;});
  pending={token,request};try{return await request;}finally{if(pending?.request===request)pending=null;}
 }
 async function clearSession(redirect=true){
  generation++;pending=null;const previous=router.currentRoute.value.fullPath;
  sessionUser.value=null;access.$reset();users.$reset();tabs.$reset();
  if(router.currentRoute.value.path!==LOGIN_PATH)await router.replace({path:LOGIN_PATH,query:redirect?{redirect:encodeURIComponent(previous)}:{}});
 }
 async function authLogin(params:Recordable<any>,onSuccess?:()=>Promise<void>|void){
  const version=++generation;loginLoading.value=true;
  try{const result=await loginApi(params);if(version!==generation)return{userInfo:null};
   if(!result.token||!result.user)throw new Error('登录响应缺少会话信息');
   access.setAccessToken(result.token);access.setIsAccessChecked(false);access.setLoginExpired(false);applySessionUser(result.user);
   if(result.user.passwordChangeRequired)await router.push('/account/password');
   else if(onSuccess)await onSuccess();else await router.push(sessionHome(result.user));
   return{userInfo:toFrameworkUser(result.user)};
  }finally{loginLoading.value=false;}
 }
 async function logout(redirect=true){try{await logoutApi();}catch(error:any){if(error?.response?.status!==401)throw error;}await clearSession(redirect);}
 async function fetchUserInfo(){return toFrameworkUser(await refreshSessionUser());}
 function $reset(){generation++;pending=null;sessionUser.value=null;loginLoading.value=false;}
 return{$reset,loginLoading,sessionUser,applySessionUser,refreshSessionUser,clearSession,authLogin,logout,fetchUserInfo};
});
