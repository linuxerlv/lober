import {
  appCopyrightPreferences,
  defineOverridesPreferences,
} from '@vben/preferences';

/**
 * @description 项目配置文件
 * 只需要覆盖项目中的一部分配置，不需要的配置不用覆盖，会自动使用默认配置
 * !!! 更改配置后请清空缓存，否则可能不生效
 */
export const overridesPreferences = defineOverridesPreferences({
  // overrides
  app: {
    name: import.meta.env.VITE_APP_TITLE,
    // 菜单写死在前端路由，省掉一个后端菜单接口（SPEC §12.5）
    accessMode: 'frontend',
    // 任务列表是主战场
    defaultHomePath: '/tasks',
    // 只发中文
    locale: 'zh-CN',
    // 离线控制台没必要按分钟轮询检查前端更新
    enableCheckUpdates: false,
    // 默认值指向 unpkg.com，气隙下必坏，换成本地资源
    defaultAvatar: '/avatar.svg',
    // 水印带操作人，误装追溯用
    watermark: true,
  },
  copyright: {
    ...appCopyrightPreferences,
    // 默认带 vben 的公司名与站点链接，不需要
    enable: false,
  },
  logo: {
    // 默认值指向 unpkg.com，气隙下必坏，换成本地资源
    source: '/logo.svg',
  },
  widget: {
    // 只发中文，不需要语言切换按钮
    languageToggle: false,
  },
});
