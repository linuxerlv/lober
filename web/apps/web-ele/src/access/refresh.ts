let refresher: () => Promise<void> = async () => {};
export function registerAccessRefresh(callback: () => Promise<void>) {
  refresher = callback;
}
export function refreshAccess() {
  return refresher();
}
