import type { ResponseInterceptorConfig } from '@vben/request';
import { CanceledError } from '@vben/request';

export function sessionResponseInterceptor(currentToken:()=>string|null,onExpired:()=>Promise<void>):ResponseInterceptorConfig{
 function stale(config:any){
  const used=config?.headers?.Authorization||null;
  const token=currentToken();
  return used!==(token?`Bearer ${token}`:null);
 }
 return{
  fulfilled(response){if(stale(response.config))throw new CanceledError('会话已变更');return response;},
  async rejected(error){
   if(error?.config&&stale(error.config))throw new CanceledError('会话已变更');
   if(error?.response?.status===401&&error?.config?.url!=='/auth/login'&&currentToken())await onExpired();
   throw error;
  },
 };
}
