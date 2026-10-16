import { expect,it,vi } from 'vitest';
import { sessionResponseInterceptor } from './session-interceptor';
it('does not expire a newer session when an old request returns 401',async()=>{
 const expired=vi.fn(async()=>{});const guard=sessionResponseInterceptor(()=> 'new',expired);
 const error={config:{url:'/user/info',headers:{Authorization:'Bearer old'}},response:{status:401}};
 await expect(guard.rejected!(error)).rejects.toBeDefined();expect(expired).not.toHaveBeenCalled();
});
it('expires the matching token once without making a logout request',async()=>{
 const expired=vi.fn(async()=>{});const guard=sessionResponseInterceptor(()=> 'same',expired);
 const error={config:{url:'/user/info',headers:{Authorization:'Bearer same'}},response:{status:401}};
 await expect(guard.rejected!(error)).rejects.toBe(error);expect(expired).toHaveBeenCalledTimes(1);
});
it('rejects successful old-session data and leaves a failed login separate from expiration',async()=>{
 const expired=vi.fn(async()=>{});const guard=sessionResponseInterceptor(()=> 'new',expired);
 expect(()=>guard.fulfilled!({config:{url:'/hosts',headers:{Authorization:'Bearer old'}}} as any)).toThrow();
 await expect(guard.rejected!({config:{url:'/auth/login',headers:{Authorization:'Bearer new'}},response:{status:401}})).rejects.toBeDefined();expect(expired).not.toHaveBeenCalled();
});
