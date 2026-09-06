<p align="center">
  <img src="./web/public/favicon.svg" alt="Denova icon" width="76" height="76">
</p>

<p align="center">
  <strong>Denova is an integrated AI creative platform for novel writing and AI role-playing games, with built-in AI Agents, Skills, SubAgent collaboration, automation, image generation, and project version management</strong>
</p>

<p align="center">
  English | <a href="README.md">中文</a>
</p>

<p align="center">
  <a href="https://discord.gg/QuHu2aPya"><img src="https://img.shields.io/badge/Discord-5865F2?logo=discord&logoColor=white" alt="Join the Denova Discord" /></a>
  <a href="https://github.com/alfredxw/denova/releases"><img alt="Release" src="https://img.shields.io/github/v/release/alfredxw/denova?style=flat-square"></a>
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/github/license/alfredxw/denova?style=flat-square"></a>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26.6%2B-00ADD8?style=flat-square&logo=go&logoColor=white">
  <img alt="Node.js" src="https://img.shields.io/badge/Node.js-22.13%2B-5FA04E?style=flat-square&logo=nodedotjs&logoColor=white">
</p>

<p align="center">
  Current version: <strong>v0.4.2</strong> (2026-09-06) · Beta · <a href="https://github.com/alfredxw/denova/releases">Download the latest release</a>
</p>

![Denova Writing](./img/ide.png)

<details>
<summary>View more screenshots</summary>

### Game

![Denova Game](./img/interactive.png)

### Lore Library

![Denova Lore Library](./img/setting.png)

### Presets

![Denova Presets](./img/story-teller.png)

### Change review

![Denova change review](./img/review.png)

### Workspace

![Denova workspace](./img/workspace.png)

</details>

## Why Denova

Denova brings novel writing, interactive stories, structured lore, AI Agents, image generation, automation, and local version management into one workspace. It is designed for creative projects whose settings and content need to evolve over time.

Start a new book from an idea, import an existing novel to continue writing, or use character cards and lore to build a branching text adventure. Agents can read project content, use tools, and edit files while important changes remain reviewable, undoable, and recoverable.

## Core Features

- **Novel writing**: Markdown document and source editing, multiple tabs, find and replace, outlines and chapter plans, progress tracking, inline comments, change review, and novel import.
- **Creative Agents**: work with the current selection, project files, and lore; extend workflows with Skills; and support multiple conversations plus opt-in SubAgent collaboration.
- **Interactive games**: advance stories through player input, with branches, storyline switching, action suggestions, character and world state, rule checks, and adjustable story planning.
- **Lore and presets**: manage characters, locations, factions, world rules, and narrative styles in one place so durable creative assets can serve both Writing and Game.
- **Image creation**: generate chapter illustrations, interactive images, and book covers, then preview and manage the results in the app.
- **Versions and recovery**: save local versions, inspect changes, restore historical files, and review or undo Agent edits to the workspace.
- **Automation**: run scheduled review, continuation, and custom creative tasks.
- **Cross-platform experience**: Chinese and English UI, light and dark themes, Windows / macOS / Linux support, remote access, and installation to a phone's home screen.

## Writing and Game

Writing and Game are peer top-level destinations in the workbench. Writing focuses on ideas, settings, outlines, chapters, and progress. Game focuses on player actions, story branches, character state, and storyline progression.

Lore, presets, Skills, and version management are shared between the two workflows. Chapter progress and game state remain separate so temporary state from one destination does not affect the other.

## Quick Start

### Install a Release

macOS / Linux users can run the installer:

```bash
curl -fsSL https://raw.githubusercontent.com/alfredxw/denova/master/scripts/install.sh | sh
```

Run `denova` after installation. Windows users and anyone who prefers manual installation can download the archive for their platform from [GitHub Releases](https://github.com/alfredxw/denova/releases); on Windows, run `denova.exe`.

For stable use, choose a Release. The `master` branch may contain unreleased changes.

Before upgrading from v0.3.3, read the [v0.4.0 release and data migration notes](./CHANGELOG.md): recreate legacy global Automations within Projects and reselect affected model and context options.

### First Run

1. Follow the startup guide to add a language-model API key and model name.
2. Create or import a book, or open an existing project directory.
3. Start creating from Writing, or create an interactive storyline from Game. Configure an image model only when you need image generation.

### Run from Source

You need Go 1.26.6+, Node.js 22.13+, pnpm, ripgrep, and Bash. On Windows, run these commands from Git Bash or WSL.

```bash
git clone https://github.com/alfredxw/denova.git
cd denova
corepack enable
./scripts/bootstrap.sh
```

Default addresses:

- Frontend: `http://localhost:5173`
- Backend: `http://localhost:8080`

## Models and Configuration

The recommended setup is through Settings: add a provider connection, select or enter a model, and test the connection. One connection can be reused by multiple models. Language models support built-in providers and custom API-compatible endpoints. Image models support OpenAI, xAI/Grok, Volcengine Seedream, Google Gemini Image, ComfyUI Workflow, and custom endpoints.

## Remote Access and Phone Usage

Enable LAN access under **Settings → Remote Access**, then set a username and password. Other devices can open the address shown on that page, and phone browsers can add Denova to the home screen.

For public or domain-based deployments, put Denova behind an HTTPS reverse proxy such as Caddy or Nginx so login credentials are not transmitted in cleartext.

## Development

Read [CONTRIBUTING.md](./CONTRIBUTING.md) before contributing. Common commands:

```bash
./scripts/bootstrap.sh fe
./scripts/bootstrap.sh be
./scripts/build.sh
```

## Community

Denova is evolving quickly. Bug reports, workflow ideas, usage notes, and creative discussions are welcome.

[Discord community](https://discord.gg/QuHu2aPya)

<p align="center">
  <img src="./img/wechat.png" alt="WeChat community" width="240">
</p>

## Support Denova

> Help Denova keep improving and remain open source. Thank you for your support!

<p align="center">
  <img src="./img/donate.png" alt="Donate" width="240">
</p>

## License

[Apache-2.0](./LICENSE)
