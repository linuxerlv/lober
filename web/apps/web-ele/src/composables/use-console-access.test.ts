import { createApp } from 'vue';
import { createPinia } from 'pinia';
import { useAccessStore } from '@vben/stores';
import { afterEach,expect,it,vi } from 'vitest';
vi.mock('#/api',()=>({loginApi:vi.fn(),logoutApi:vi.fn(),getUserInfoApi:vi.fn()}));
import { registerAccessRefresh } from '#/access/refresh';
import { useConsoleSessionRefresh } from './use-console-access';
afterEach(()=>{vi.useRealTimers();registerAccessRefresh(async()=>{});});
it('refreshes on focus and every 30 seconds without overlapping, then removes listeners',async()=>{
 vi.useFakeTimers();const pinia=createPinia();let finish!:()=>void;let calls=0;
 registerAccessRefresh(async()=>{calls++;await new Promise<void>(resolve=>{finish=resolve;});});
 const el=document.createElement('div');const app=createApp({setup(){useConsoleSessionRefresh();return()=>null;}});app.use(pinia);app.mount(el);
 const access=useAccessStore(pinia);access.setAccessToken('token');
 window.dispatchEvent(new Event('focus'));window.dispatchEvent(new Event('focus'));await vi.advanceTimersByTimeAsync(30_000);expect(calls).toBe(1);
 finish();await Promise.resolve();await vi.advanceTimersByTimeAsync(30_000);expect(calls).toBe(2);
 finish();await Promise.resolve();app.unmount();await vi.advanceTimersByTimeAsync(60_000);window.dispatchEvent(new Event('focus'));expect(calls).toBe(2);
});
