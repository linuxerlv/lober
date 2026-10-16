import type { NavigationGuard,Router,RouteRecordRaw } from 'vue-router';
import type { ConsoleSessionUser } from '#/api/console/session';
import { canOperate,consoleMenus,sessionHome } from '#/access/policy';

export function consoleAccessKey(user:ConsoleSessionUser):string{
 return JSON.stringify([user.username,user.role,[...user.permissions].sort(),[...user.menuIds].sort(),user.menuRev,user.passwordChangeRequired]);
}
export function filterConsoleRoutes(routes:RouteRecordRaw[],user:ConsoleSessionUser,inheritedMenu?:string):RouteRecordRaw[]{
 const out:RouteRecordRaw[]=[];
 for(const original of routes){
  const menuId=(original.meta?.menuId as string|undefined)||inheritedMenu;
  const menu=menuId?consoleMenus[menuId]:undefined;
  const permission=original.meta?.permission as string|undefined;
  if(!menuId||!menu||!user.menuIds.includes(menuId)||!canOperate(user,menu.permission)||(permission&&!canOperate(user,permission)))continue;
  const meta={...original.meta,menuId};delete meta.authority;
  const node={...original,meta} as RouteRecordRaw;
  if(original.children){
   node.children=filterConsoleRoutes(original.children,user,menuId);if(!node.children.length)continue;
   const first=node.children[0]!;
   if(!node.redirect&&first.path&&!first.path.includes(':'))node.redirect=first.path.startsWith('/')?first.path:`${node.path}/${first.path}`;
  }
  out.push(node);
 }
 return out;
}
export function firstConsolePath(routes:RouteRecordRaw[]):string{
 const first=routes[0];if(!first)return '/no-access';
 const child=first.children?.find(route=>!route.meta?.hideInMenu&&!route.path.includes(':'));
 if(child?.path)return child.path.startsWith('/')?child.path:`${first.path}/${child.path}`;
 return first.path;
}
function namesOf(routes:RouteRecordRaw[]):Set<string|symbol>{
 const names=new Set<string|symbol>();for(const route of routes){if(route.name)names.add(route.name);for(const n of namesOf(route.children||[]))names.add(n);}return names;
}
export function replaceConsoleRoutes(router:Router,baseRoutes:RouteRecordRaw[],routes:RouteRecordRaw[],user:ConsoleSessionUser|null):RouteRecordRaw[]{
 const keep=namesOf(baseRoutes);
 for(const route of router.getRoutes())if(route.name&&!keep.has(route.name)&&router.hasRoute(route.name))router.removeRoute(route.name);
 const allowed=user?filterConsoleRoutes(routes,user):[];
 for(const route of allowed){if(router.hasRoute('Root'))router.addRoute('Root',route);else router.addRoute(route);}
 return allowed;
}
interface ConsoleGuardOptions{
 router:Router;baseRoutes:RouteRecordRaw[];routes:RouteRecordRaw[];
 getToken:()=>string|null;getUser:()=>ConsoleSessionUser|null;refreshUser:()=>Promise<unknown>;
 clearCaches:()=>void;onError:(error:unknown)=>void;onRoutes?:(routes:RouteRecordRaw[])=>void;
}
export function createConsoleGuard(options:ConsoleGuardOptions):NavigationGuard&{refresh:()=>Promise<void>}{
 let installed='';
 function reset(){if(installed){replaceConsoleRoutes(options.router,options.baseRoutes,options.routes,null);options.clearCaches();options.onRoutes?.([]);installed='';}}
 async function synchronize(){
  const token=options.getToken();if(!token){reset();return null;}
  await options.refreshUser();if(token!==options.getToken())return null;
  const user=options.getUser();if(!user)return null;
  const key=JSON.stringify([token,consoleAccessKey(user)]);const changed=key!==installed;
  if(changed){options.clearCaches();const routes=replaceConsoleRoutes(options.router,options.baseRoutes,options.routes,user);options.onRoutes?.(routes);installed=key;}
  return{user,changed};
 }
 const guard:NavigationGuard=async(to)=>{
  if(!options.getToken()){reset();return to.path==='/auth/login'?true:{path:'/auth/login',query:{redirect:encodeURIComponent(to.fullPath)},replace:true};}
  let state;try{state=await synchronize();}catch(error){options.onError(error);return false;}
  if(!state)return false;
  const{user,changed}=state;
  if(user.passwordChangeRequired)return to.path==='/account/password'?true:{path:'/account/password',replace:true};
  if(to.path==='/account/password'||to.path==='/no-access')return true;
  if(to.path==='/auth/login'||to.path==='/')return{path:sessionHome(user),replace:true};
  const resolved=options.router.resolve(to.fullPath);
  if(!resolved.matched.some(route=>route.meta.menuId))return{path:sessionHome(user),replace:true};
  return changed?{path:to.fullPath,replace:true}:true;
 };
 return Object.assign(guard,{async refresh(){
  try{
   const state=await synchronize();if(!state?.changed)return;
   const current=options.router.currentRoute.value;
   const allowed=options.router.resolve(current.fullPath).matched.some(route=>route.meta.menuId);
   const path=state.user.passwordChangeRequired?'/account/password':allowed?current.fullPath:sessionHome(state.user);
   await options.router.replace({path,force:true});
  }catch(error){options.onError(error);}
 }});
}
