import type { ConsoleRole } from './session';

import { requestClient } from '#/api/request';
export interface MenuDefinition {
  id: string;
  title: string;
  parentId: string;
  permission: string;
  required: boolean;
}
export interface RoleMenuConfig {
  role: ConsoleRole;
  availableMenuIds: string[];
  menuIds: string[];
  rev: number;
}
export interface RoleMenuList {
  menus: MenuDefinition[];
  roles: RoleMenuConfig[];
}
export interface RoleMenuInput {
  menuIds: string[];
  rev: number;
}
interface MenuHttp {
  get<T>(path: string): Promise<T>;
  put<T>(path: string, data: unknown): Promise<T>;
}
export function createRoleMenusApi(client: MenuHttp) {
  return {
    list: () => client.get<RoleMenuList>('/roles/menus'),
    save: (role: ConsoleRole, data: RoleMenuInput) =>
      client.put<RoleMenuConfig>(`/roles/${role}/menus`, data),
  };
}
export function getRoleMenusApi() {
  return createRoleMenusApi(requestClient).list();
}
export function saveRoleMenusApi(role: ConsoleRole, data: RoleMenuInput) {
  return createRoleMenusApi(requestClient).save(role, data);
}
