import type { ConsoleSessionUser } from '#/api/console/session';
export const consoleMenus: Record<string,{path:string;permission:string}>={
 tasks:{path:'/tasks',permission:'task:read'},hosts:{path:'/hosts',permission:'host:read'},
 templates:{path:'/templates',permission:'template:read'},hooks:{path:'/hooks',permission:'hook:read'},
 isos:{path:'/artifact-sets',permission:'iso:read'},tools:{path:'/tools/disk-dryrun',permission:'template:read'},
 reports:{path:'/reports',permission:'audit:read'},'role-menus':{path:'/access/role-menus',permission:'user:manage'},
};
export function canOperate(user:ConsoleSessionUser|null,permission:string):boolean{
 return !!user&&!user.passwordChangeRequired&&user.permissions.includes(permission);
}
export function sessionHome(user:ConsoleSessionUser):string{
 if(user.passwordChangeRequired)return '/account/password';
 for(const id of user.menuIds){const menu=consoleMenus[id];if(menu&&canOperate(user,menu.permission))return menu.path;}
 return '/no-access';
}
