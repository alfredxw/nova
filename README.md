<p align="center">
  <img src="./web/public/favicon.svg" alt="Denova 图标" width="76" height="76">
</p>

<p align="center">
  <strong>Denova 是一个面向小说写作与 AI 角色扮演游戏的一体化 AI 创作平台，内置 AI Agents、Skills、SubAgent 协作、自动化、图像生成与项目版本管理</strong>
</p>

<p align="center">
  <a href="README.en.md">English</a> | 中文
</p>

<p align="center">
  <a href="https://discord.gg/QuHu2aPya"><img src="https://img.shields.io/badge/Discord-5865F2?logo=discord&logoColor=white" alt="加入 Denova Discord" /></a>
  <a href="https://github.com/alfredxw/denova/releases"><img alt="Release" src="https://img.shields.io/github/v/release/alfredxw/denova?style=flat-square"></a>
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/github/license/alfredxw/denova?style=flat-square"></a>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26.6%2B-00ADD8?style=flat-square&logo=go&logoColor=white">
  <img alt="Node.js" src="https://img.shields.io/badge/Node.js-22.13%2B-5FA04E?style=flat-square&logo=nodedotjs&logoColor=white">
</p>

<p align="center">
  当前版本：<strong>v0.4.2</strong>（2026-09-06） · Beta · <a href="https://github.com/alfredxw/denova/releases">下载最新版本</a>
</p>

![Denova 写作](./img/ide.png)

<details>
<summary>查看更多界面截图</summary>

### 游戏

![Denova 游戏](./img/interactive.png)

### 资料库

![Denova 资料库](./img/setting.png)

### 方案预设

![Denova 方案预设](./img/story-teller.png)

### 修改审阅

![Denova 修改审阅](./img/review.png)

### 工作台

![Denova 工作台](./img/workspace.png)

</details>

## 为什么选择 Denova

Denova 把小说写作、互动故事、结构化资料库、AI Agent、图像生成、自动化和本地版本管理放进同一个工作区，适合需要长期维护设定、反复修改并持续积累内容的创作项目。

你可以从一个灵感开始新书，导入已有小说继续创作，也可以使用角色卡和资料库搭建可分支的文字冒险。Agent 能读取项目内容、调用工具并修改文件，所有重要变更仍可检查、撤销和恢复。

## 核心能力

- **小说写作**：Markdown 文档与源码编辑、多文档 Tab、查找替换、大纲与章节细纲、进度追踪、正文评论、修改审阅和现有小说导入。
- **创作 Agent**：结合当前选区、项目文件和资料库进行创作，可通过 Skills 扩展工作流，并支持多个会话和按需的 SubAgent 协作。
- **互动游戏**：通过玩家输入推进故事，支持剧情分支、故事线切换、行动建议、角色与世界状态、规则检定以及可调整的剧情规划。
- **资料库与方案预设**：统一管理角色、地点、势力、世界规则和叙事风格，让稳定设定同时服务写作与游戏。
- **图像创作**：生成章节插画、互动图像和书籍封面，并在界面中预览和管理结果。
- **版本与恢复**：保存本地版本、查看差异、恢复历史文件，并审阅或撤销 Agent 对工作区的修改。
- **自动化**：按计划运行审阅、续写和自定义创作任务。
- **跨平台体验**：支持中文与英文、浅色与深色主题、Windows / macOS / Linux、远程访问和添加到手机主屏幕。

## 写作与游戏

写作和游戏是工作台中并列的一级入口。写作侧重构思、设定、大纲、章节和进度；游戏侧重玩家行动、剧情分支、角色状态和故事线推进。

资料库、方案预设、Skills 和版本管理由两类流程共享；章节进度和游戏状态则各自独立，避免一个入口的临时状态干扰另一个入口。

## 快速开始

### 安装发布版

macOS / Linux 可以使用一键安装脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/alfredxw/denova/master/scripts/install.sh | sh
```

安装完成后运行 `denova`。Windows 用户以及希望手动安装的用户，可以从 [GitHub Releases](https://github.com/alfredxw/denova/releases) 下载对应平台的压缩包；Windows 运行 `denova.exe`。

稳定使用建议选择 Release；`master` 分支可能包含尚未发布的改动。

从 v0.3.3 升级前，请阅读 [v0.4.0 更新与数据迁移说明](./CHANGELOG.md)：旧全局自动化需在项目内重新创建，部分模型和上下文选项需重新设置。

### 第一次使用

1. 按启动引导添加语言模型的 API Key 和模型名。
2. 创建或导入一本书，也可以直接打开已有项目目录。
3. 从「写作」开始创作，或从「游戏」创建一条互动故事线；需要生成图片时再配置图像模型。

### 从源码运行

需要 Go 1.26.6+、Node.js 22.13+、pnpm、ripgrep 和 Bash。Windows 请在 Git Bash 或 WSL 中运行以下命令。

```bash
git clone https://github.com/alfredxw/denova.git
cd denova
corepack enable
./scripts/bootstrap.sh
```

默认地址：

- 前端：`http://localhost:5173`
- 后端：`http://localhost:8080`

## 模型与配置

推荐在设置页完成模型配置：先添加服务商连接，再选择或填写模型并测试连接。同一连接可以复用于多个模型。语言模型支持内置服务商和自定义兼容端点；图像模型支持 OpenAI、xAI/Grok、火山引擎 Seedream、Google Gemini Image、ComfyUI Workflow 和自定义端点。

## 远程访问与手机使用

在 **设置 → 远程访问** 中开启局域网访问并设置用户名和密码后，其他设备可以使用页面显示的地址访问 Denova。手机浏览器可以将页面添加到主屏幕。

通过公网或域名部署时，请使用 Caddy、Nginx 等反向代理提供 HTTPS，避免明文传输登录凭据。

## 开发

贡献代码前请先阅读 [CONTRIBUTING.md](./CONTRIBUTING.md)。常用命令：

```bash
./scripts/bootstrap.sh fe
./scripts/bootstrap.sh be
./scripts/build.sh
```

## 欢迎交流

Denova 仍在快速迭代中，欢迎反馈问题、分享用法或讨论创作工作流。

[Discord 社区](https://discord.gg/QuHu2aPya)

<p align="center">
  <img src="./img/wechat.png" alt="微信交流" width="240">
</p>

## 赞助项目

> 给项目冲点 token，帮助 Denova 持续迭代和开源。感谢你的支持！

<p align="center">
  <img src="./img/donate.png" alt="捐赠" width="240">
</p>

## License

[Apache-2.0](./LICENSE)
