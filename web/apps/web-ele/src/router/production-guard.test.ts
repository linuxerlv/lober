import { createApp } from 'vue';
import { createMemoryHistory,createRouter } from 'vue-router';

import { createPinia } from 'pinia';
import { expect,it,vi } from 'vitest';
vi.mock('#/api',()=>({loginApi:vi.fn(),logoutApi:vi.fn(),getUserInfoApi:vi.fn()}));
import { createRouterGuard } from './guard';
it('allows an unauthenticated browser to finish the real login navigation',async()=>{
 const view={template:'<div />'};
 const router=createRouter({history:createMemoryHistory(),routes:[{name:'Root',path:'/',component:view},{name:'Login',path:'/auth/login',component:view}]});
 createRouterGuard(router);const app=createApp(view);app.use(createPinia());app.use(router);
 await expect(router.push('/auth/login')).resolves.toBeUndefined();expect(router.currentRoute.value.path).toBe('/auth/login');
});
