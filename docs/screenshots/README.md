# Console screenshots / 后台截图

These screenshots were refreshed from the current embedded Connara administration console on 2026-10-04. The application still displays its APIHub compatibility name. System and API screenshots are filtered to built-in GitHub records; Authentication Center shows built-in templates. User-created systems, internal addresses, credentials, and run history are excluded.

以下截图于 2026-10-04 从当前内嵌 Connara 管理后台重新采集，界面仍显示 APIHub 兼容名称。系统和 API 截图只展示筛选后的内置 GitHub 数据，认证中心只展示内置模板；自建系统、内网地址、凭据和运行记录均已排除。

| View / 页面 | English | 简体中文 |
| --- | --- | --- |
| Visual workflow editor / 可视化工作流编辑器 | [overview.en.png](overview.en.png) | [overview.zh-CN.png](overview.zh-CN.png) |
| System catalog / 系统目录 | [systems.en.png](systems.en.png) | [systems.zh-CN.png](systems.zh-CN.png) |
| Authentication / 认证中心 | [authentication.en.png](authentication.en.png) | [authentication.zh-CN.png](authentication.zh-CN.png) |
| API actions / API 操作 | [actions.en.png](actions.en.png) | [actions.zh-CN.png](actions.zh-CN.png) |
| Mobile navigation / 手机导航 | [mobile-navigation.en.png](mobile-navigation.en.png) | [mobile-navigation.zh-CN.png](mobile-navigation.zh-CN.png) |

Desktop viewport: **1440 × 1000**. Mobile viewport: **390 × 844**. Theme: **light**. Capture tool: **agent-browser**. Each language uses the real UI language setting. Current English screenshots retain untranslated workflow and authentication text from the application; the images were not edited to simulate a complete translation.

桌面视口为 **1440 × 1000**，手机视口为 **390 × 844**，使用浅色主题和 **agent-browser** 截图。两种语言分别切换实际 UI 语言后获取；英文工作流和认证界面中的未翻译文本按实际保留，没有编辑图片伪造完整翻译。

The current visual workflow editor screenshots and their capture boundaries are documented in [the workflow screenshot index](../assets/workflows/README.md).

当前可视化工作流编辑器截图及采集边界见[工作流截图说明](../assets/workflows/README.md)。

To refresh / 更新截图：

1. Start the current build and sign into an authorized local instance. / 启动当前构建并登录授权的本地实例。
2. Select English or Chinese and the light theme. / 选择对应语言和浅色主题。
3. Capture `/workflows/new`; filter `/providers` and `/actions` to the built-in GitHub records; open `/auth?section=templates`. Wait for backend and capability loading before every capture. / 截取 `/workflows/new`；将 `/providers` 和 `/actions` 筛选为内置 GitHub 数据；打开 `/auth?section=templates`。每次截图前等待后台和能力加载完成。
4. At the mobile viewport, open navigation on `/workflows/new` and wait for the drawer animation to finish. / 手机尺寸下在 `/workflows/new` 打开导航抽屉，待动画结束后截图。
5. Check the final pixels for user-created systems, internal URLs, credentials, and run history, then update both language variants. / 检查最终图片没有自建系统、内网地址、凭据或运行记录，再更新中英文两套图片。

Return to [English README](../../README.md) / [中文 README](../../README.zh-CN.md).
