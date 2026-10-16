import { createApp, defineComponent, nextTick } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';
import { useAccessStore } from '@vben/stores';
import { afterEach, describe, expect, it, vi } from 'vitest';

const io = vi.hoisted(() => ({loginApi: vi.fn(), logoutApi: vi.fn(), getUserInfoApi: vi.fn()}));
vi.mock('#/api', () => io);
import { useAuthStore } from './auth';

const user = {username:'alice', displayName:'Alice', role:'viewer' as const, permissions:['host:read'], disabled:false,createdAt:1,rev:1,passwordChangeRequired:false,menuIds:['hosts'],menuRev:0};
const cleanups: Array<() => void> = [];
afterEach(() => { cleanups.splice(0).forEach(fn => fn()); vi.resetAllMocks(); });
async function setup() {
  const router = createRouter({history:createMemoryHistory(),routes:['/auth/login','/hosts','/no-access','/account/password'].map(path=>({path, component:{template:'<div />'}}))});
  await router.push('/auth/login');
  const pinia=createPinia(); setActivePinia(pinia);
  let store!: ReturnType<typeof useAuthStore>;
  const el=document.createElement('div');document.body.append(el);
  const app=createApp(defineComponent({setup(){store=useAuthStore();return()=>null;}}));
  app.use(pinia);app.use(router);app.mount(el);
  cleanups.push(()=>{app.unmount();el.remove();});
  return {store,router,access:useAccessStore(pinia)};
}
describe('authentication state',()=>{
  it('establishes the backend token, permissions and role without auth/codes',async()=>{
    const {store,router,access}=await setup();
    io.loginApi.mockResolvedValue({token:'first',expiresAt:10,user});
    await store.authLogin({username:'alice',password:'old'});
    expect(access.accessToken).toBe('first');
    expect(access.accessCodes).toEqual(['host:read']);
    expect(store.sessionUser).toEqual(user);
    expect(router.currentRoute.value.path).toBe('/hosts');
    expect(io.getUserInfoApi).not.toHaveBeenCalled();
  });
  it('keeps the session when logout cannot reach the backend',async()=>{
    const {store,access}=await setup();access.setAccessToken('first');store.applySessionUser(user);
    io.logoutApi.mockRejectedValue(new Error('offline'));
    await expect(store.logout()).rejects.toThrow('offline');
    expect(access.accessToken).toBe('first');expect(store.sessionUser).toEqual(user);
  });
  it('clears an already expired session without another logout request',async()=>{
    const {store,router,access}=await setup();access.setAccessToken('first');store.applySessionUser(user);
    io.logoutApi.mockRejectedValue({response:{status:401}});
    await store.logout();expect(access.accessToken).toBeNull();expect(store.sessionUser).toBeNull();expect(router.currentRoute.value.path).toBe('/auth/login');expect(io.logoutApi).toHaveBeenCalledTimes(1);
  });
  it('ignores an old account response arriving after the token changes',async()=>{
    const {store,access}=await setup();access.setAccessToken('first');store.applySessionUser(user);
    let finish!: (value: typeof user)=>void;
    io.getUserInfoApi.mockReturnValue(new Promise(resolve=>{finish=resolve;}));
    const old=store.refreshSessionUser();
    access.setAccessToken('second');store.applySessionUser({...user,username:'bob'});
    finish(user);await old;await nextTick();
    expect(store.sessionUser?.username).toBe('bob');
  });
});
