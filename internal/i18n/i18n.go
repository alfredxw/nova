package i18n

import (
	"fmt"
	"strings"
)

const (
	LocaleAuto = "auto"
	LocaleZH   = "zh-CN"
	LocaleEN   = "en-US"
)

type Localizer struct {
	locale string
}

func New(locale string) Localizer {
	return Localizer{locale: Resolve(locale)}
}

func FromHeader(value string) Localizer {
	return New(value)
}

func Resolve(value string) string {
	normalized := strings.TrimSpace(value)
	if normalized == "" || strings.EqualFold(normalized, LocaleAuto) {
		return LocaleZH
	}
	lower := strings.ToLower(normalized)
	if strings.HasPrefix(lower, "zh") {
		return LocaleZH
	}
	if strings.HasPrefix(lower, "en") {
		return LocaleEN
	}
	return LocaleZH
}

func (l Localizer) Locale() string {
	return l.locale
}

func (l Localizer) T(key string, args ...any) string {
	catalog := catalogZH
	if l.locale == LocaleEN {
		catalog = catalogEN
	}
	template, ok := catalog[key]
	if !ok {
		template = catalogZH[key]
	}
	if template == "" {
		return key
	}
	return format(template, args...)
}

func format(template string, args ...any) string {
	out := template
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := args[i].(string)
		if !ok || name == "" {
			continue
		}
		out = strings.ReplaceAll(out, "{{"+name+"}}", stringify(args[i+1]))
	}
	return out
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

var catalogZH = map[string]string{
	"api.update.checkFailed":                  "检查更新失败，请稍后重试。详细原因请查看服务端日志。",
	"api.update.installFailed":                "安装更新失败，请重试或从 GitHub Releases 手动下载安装包。详细原因请查看服务端日志。",
	"api.update.applyFailed":                  "应用更新失败，请查看服务端日志，并尝试手动重启或安装。",
	"api.common.invalidRequest":               "请求参数无效",
	"api.common.invalidRequestWithDetail":     "请求参数无效: {{detail}}",
	"api.common.invalidBody":                  "无效请求体",
	"api.common.messageRequired":              "消息不能为空",
	"api.common.pathRequired":                 "请提供 path 参数",
	"api.access.authRequired":                 "请先输入 Denova 远程访问用户名和密码",
	"api.access.lanDisabled":                  "当前未开启局域网访问",
	"api.access.localHostEffect":              "此系统操作只能从运行 Denova 的本机触发",
	"api.hostDialog.projectDirectoryTitle":    "选择项目目录",
	"api.hostDialog.unavailable":              "当前系统无法打开目录选择器",
	"api.hostDialog.failed":                   "打开目录选择器失败: {{detail}}",
	"api.hostReveal.unavailable":              "当前系统无法打开文件管理器",
	"api.hostReveal.failed":                   "在文件管理器中显示失败: {{detail}}",
	"api.books.titleRequired":                 "title 不能为空",
	"api.books.novaDirMissing":                "Denova 数据目录未配置",
	"api.books.removed":                       "已移除书籍记录",
	"api.books.reordered":                     "已保存书籍排序",
	"api.books.pathQueryRequired":             "path 参数不能为空",
	"api.books.pathRequired":                  "path 不能为空",
	"api.books.coverUploadRequired":           "请上传 PNG 或 JPEG 格式的书籍封面",
	"api.books.coverTooLarge":                 "书籍封面不能超过 16MB",
	"api.books.coverReadFailed":               "读取封面文件失败: {{detail}}",
	"api.lore.imageUploadRequired":            "请上传 PNG 或 JPEG 格式的资料图片",
	"api.lore.imageTooLarge":                  "资料图片不能超过 16MB",
	"api.lore.imageReadFailed":                "读取资料图片失败: {{detail}}",
	"api.lore.imageInvalid":                   "仅支持有效的 PNG 或 JPEG 资料图片",
	"api.books.exportFormatRequired":          "请提供导出格式",
	"api.books.exportFormatUnsupported":       "暂不支持导出格式: {{format}}",
	"api.books.exportNoChapters":              "没有可导出的非空章节",
	"api.chat.noActiveTask":                   "没有活跃任务",
	"api.command.empty":                       "命令不能为空",
	"api.command.clearFailed":                 "清空失败: {{detail}}",
	"api.command.cleared":                     "上下文已清理，历史消息已保留",
	"api.command.compactFailed":               "上下文压缩失败: {{detail}}",
	"api.command.compacted":                   "上下文压缩完成，epoch {{epoch}}，估算 {{before}} → {{after}} tokens",
	"api.command.noStatus":                    "当前无作品状态数据，请先创建大纲",
	"api.command.unknown":                     "未知命令: {{command}}",
	"api.command.help":                        "可用命令:\n\n  plan    — 先规划再执行（/plan <需求描述>）\n  clear   — 清理当前 Agent 上下文并保留历史消息\n  compact — 主动压缩当前 Agent 上下文\n  status  — 显示当前作品状态\n  help    — 显示此帮助信息\n  /<skill-name> — 在支持 Skills 的 Agent 中加载指定 Skill，例如 /web-research\n\n在聊天中直接输入创作想法即可开始与 Denova 对话。",
	"api.skills.scopeNameRequired":            "请提供 scope 和 name 参数",
	"api.skills.scopeNamePathRequired":        "请提供 scope、name 和 path 参数",
	"api.skills.uploadRequired":               "请上传 Skill ZIP 文件",
	"api.skills.tooLarge":                     "Skill ZIP 文件不能超过 32MB",
	"api.skills.readFailed":                   "读取 Skill ZIP 文件失败: {{detail}}",
	"api.characterCard.parseFailed":           "解析酒馆角色卡失败: {{detail}}",
	"api.characterCard.uploadRequired":        "请上传 PNG 或 JSON 格式的酒馆角色卡文件",
	"api.characterCard.tooLarge":              "角色卡文件不能超过 32MB",
	"api.characterCard.readFailed":            "读取上传文件失败: {{detail}}",
	"api.characterCard.invalidTarget":         "导入目标无效",
	"api.characterCard.importFailed":          "导入酒馆角色卡失败: {{detail}}",
	"api.characterCard.imported":              "已导入酒馆角色卡「{{name}}」",
	"api.novelImport.parseFailed":             "解析小说文件失败: {{detail}}",
	"api.novelImport.uploadRequired":          "请上传 txt 或 md 格式的小说文件",
	"api.novelImport.tooLarge":                "小说文件不能超过 64MB",
	"api.novelImport.readFailed":              "读取上传文件失败: {{detail}}",
	"api.novelImport.importFailed":            "导入小说失败: {{detail}}",
	"api.novelImport.imported":                "小说导入完成",
	"api.novelImport.singleChapterWarning":    "未识别到明确章节标题，已作为单章导入",
	"api.novelImport.agentFallbackWarning":    "智能识别章节正则失败，已回退内置规则",
	"api.novelImport.regexFewChaptersWarning": "智能识别出的章节正则少于 2 章，已回退内置规则",
	"api.novelImport.regexFallbackWarning":    "智能识别出的章节正则不可用，已回退内置规则: {{detail}}",
	"api.interactive.storyIDRequired":         "故事 ID 不能为空",
	"api.interactive.storyModeOnly":           "当前仅支持 story 子模式",
	"api.interactive.storyStructureBusy":      "故事正在生成，请在本轮结束后再修改主角或状态结构",
	"api.interactive.tellerInstructionEmpty":  "叙事风格编辑指令不能为空",
	"api.lore.instructionEmpty":               "资料库编辑指令不能为空",
	"api.settings.revisionConflict":           "配置已被 Agent 或其他操作更新，请重新加载后再保存",
	"api.conversationConfig.revisionConflict": "会话配置已在其他窗口中更新，请重新加载后再修改",
	"api.conversationConfig.rememberModelFailed": "未能记住模型选择用于新会话，请重新选择并重试。",
	"api.goal.revisionConflict":               "目标已在其他窗口或 Agent 运行中更新，请重新加载后再操作",
	"api.goal.stateChanged":                   "目标状态已变化，请重新加载后再操作",
	"api.resource.revisionConflict":           "内容已被 Agent 或其他操作更新，请重新加载后再保存",
	"api.automation.baseRevisionRequired":     "自动化更新缺少版本基准，请重新加载后再保存",
	"api.versions.invalidCreateRequest":       "版本保存请求格式不正确",
	"api.versions.invalidDiffComparison":      "版本对比方式不正确",
	"api.versions.invalidRestoreRequest":      "版本恢复请求格式不正确",
	"api.versions.idRequired":                 "请提供版本 ID",
	"api.workspace.scanFailed":                "扫描目录失败: {{detail}}",
	"api.workspace.summaryFailed":             "统计作品进度失败: {{detail}}",
	"api.workspace.chapterStatusPathRequired": "请提供章节 path",
	"api.workspace.chapterStatusFailed":       "更新章节状态失败: {{detail}}",
	"api.workspace.chapterStatusSaved":        "章节状态已更新",
	"api.workspace.pathMissing":               "缺少 path 参数",
	"api.workspace.limitInvalid":              "limit 必须是非负整数",
	"api.workspace.searchFailed":              "搜索失败: {{detail}}",
	"api.workspace.queryRequired":             "请提供搜索内容",
	"api.workspace.invalidRegex":              "正则表达式无效: {{detail}}",
	"api.workspace.regexMatchesEmpty":         "正则表达式不能匹配空字符串",
	"api.workspace.replaceFailed":             "全局替换失败: {{detail}}",
	"api.workspace.pathContentRequired":       "请提供 path 和 content 参数",
	"api.workspace.writeFailed":               "写入文件失败: {{detail}}",
	"api.workspace.fileRevisionConflict":      "文件已被 Agent 或其他操作更新，请重新加载后再保存",
	"api.workspace.fileSaved":                 "文件已保存",
	"api.workspace.targetExists":              "目标已存在",
	"api.workspace.switched":                  "已切换到: {{workspace}}",
	"api.workspace.noWorkspace":               "尚未选择书籍工作区，请先在书籍管理页选择或创建书籍",
	"api.workspace.changedDuringRequest":      "工作区已切换，本次过期保存已取消，请在当前工作区重新确认修改",
	"api.projectFiles.projectIDRequired":      "请提供项目 ID",
	"api.project.idRequired":                  "请提供项目 ID",
	"api.project.idInvalid":                   "项目 ID 格式无效",
	"api.project.scopeMissing":                "项目请求上下文缺失",
	"api.project.notFound":                    "项目不存在",
	"api.project.archived":                    "项目已归档",
	"api.project.unavailable":                 "项目目录当前不可用",
	"api.project.transitioning":               "项目正在切换目录或归档，请稍后重试",
	"api.project.resolveFailed":               "无法打开项目",
	"api.attachments.scopeRequired":           "请提供有效的附件会话范围",
	"api.attachments.previewUnavailable":      "附件图片不存在或无法预览",
	"api.projectFiles.resolveTargetsRequired": "至少需要一个待解析的项目目录",
	"api.projectFiles.resolveFailed":          "读取项目目录失败: {{detail}}",
	"api.projectFiles.notDirectory":           "指定的项目路径不是目录",
	"api.projectFiles.cursorStale":            "目录内容已变化，请重新加载",
	"api.projectFiles.invalidCursor":          "目录续页信息无效，请重新加载",
	"api.projectFiles.invalidPath":            "项目目录路径无效",
	"api.projectFiles.budgetExhausted":        "项目目录请求超出本次加载预算，请分批重试",
	"api.projectFiles.readFailed":             "读取项目文件失败: {{detail}}",
	"api.projectFiles.saveFailed":             "保存项目文件失败: {{detail}}",
	"api.projectFiles.saved":                  "项目文件已保存",
	"api.projectFiles.operationsRequired":     "请提供至少一项文件操作",
	"api.projectFiles.operationFailed":        "文件操作失败: {{detail}}",
	"api.projectFiles.revealFailed":           "无法定位项目文件: {{detail}}",
	"api.projectFiles.notFound":               "项目文件或目录不存在",
	"api.projectFiles.symlinkPath":            "为保护项目外的数据，文件操作不能经过符号链接",
	"api.projectBook.bookRequired":            "此资源仅适用于书籍项目",
	"api.projectBook.readFailed":              "读取项目书籍失败: {{detail}}",
	"api.projectBook.loreFailed":              "资料库操作失败: {{detail}}",
	"api.projectBook.loreFieldsRequired":      "资料条目更新缺少完整字段: {{fields}}",
	"api.projectBook.loreIDMismatch":          "资料条目正文 ID 与资源路径不一致",
	"api.projectBook.reviewFailed":            "文本审阅操作失败: {{detail}}",
	"api.terminal.disabled":                   "终端功能已在设置中关闭",
	"api.terminal.notFound":                   "终端会话不存在或已结束",
	"api.terminal.tooMany":                    "并发终端会话数已达上限，请先关闭部分终端",
	"api.terminal.ownerConflict":              "这个终端标签页已属于另一个项目，请重新打开终端标签页",
	"api.terminal.tokenInvalid":               "终端附着令牌无效，请重新打开终端标签页",
	"api.terminal.invalidProfile":             "这个终端命令已不存在或已停用，请从新建标签页菜单重新选择",
	"api.terminal.invalidLaunchCommand":       "CLI 启动命令无效，请在设置的终端命令中检查配置",
	"api.settings.workspaceMissing":           "当前没有打开的工作区",
	"api.settings.lanUsernameRequired":        "开启局域网访问时必须设置用户名",
	"api.settings.lanPasswordRequired":        "开启局域网访问时必须设置密码",
	"api.trajectory.disabled":                 "Trajectory 当前不可用",
	"api.trajectory.invalidLimit":             "limit 必须是 1 到 500 的整数",
}

var catalogEN = map[string]string{
	"api.update.checkFailed":                  "Could not check for updates. Try again later; see server logs for details.",
	"api.update.installFailed":                "Could not install the update. Retry or download the archive from GitHub Releases; see server logs for details.",
	"api.update.applyFailed":                  "Could not apply the update. See server logs and try restarting or installing manually.",
	"api.common.invalidRequest":               "Invalid request.",
	"api.common.invalidRequestWithDetail":     "Invalid request: {{detail}}",
	"api.common.invalidBody":                  "Invalid request body.",
	"api.common.messageRequired":              "Message is required.",
	"api.common.pathRequired":                 "Provide the path parameter.",
	"api.access.authRequired":                 "Enter the Denova remote access username and password first.",
	"api.access.lanDisabled":                  "LAN access is not enabled.",
	"api.access.localHostEffect":              "This system action can only be triggered from the machine running Denova.",
	"api.hostDialog.projectDirectoryTitle":    "Choose a project folder",
	"api.hostDialog.unavailable":              "The system folder picker is unavailable on this machine.",
	"api.hostDialog.failed":                   "Failed to open the folder picker: {{detail}}",
	"api.hostReveal.unavailable":              "The system file manager is unavailable on this machine.",
	"api.hostReveal.failed":                   "Failed to reveal the item in the file manager: {{detail}}",
	"api.books.titleRequired":                 "Title is required.",
	"api.books.novaDirMissing":                "Denova data directory is not configured.",
	"api.books.removed":                       "Book record removed.",
	"api.books.reordered":                     "Book order saved.",
	"api.books.pathQueryRequired":             "Path query parameter is required.",
	"api.books.pathRequired":                  "Path is required.",
	"api.books.coverUploadRequired":           "Upload a PNG or JPEG book cover.",
	"api.books.coverTooLarge":                 "Book cover must be 16MB or smaller.",
	"api.books.coverReadFailed":               "Failed to read cover file: {{detail}}",
	"api.lore.imageUploadRequired":            "Upload a PNG or JPEG lore image.",
	"api.lore.imageTooLarge":                  "Lore image must be 16MB or smaller.",
	"api.lore.imageReadFailed":                "Failed to read lore image: {{detail}}",
	"api.lore.imageInvalid":                   "Upload a valid PNG or JPEG lore image.",
	"api.books.exportFormatRequired":          "Provide an export format.",
	"api.books.exportFormatUnsupported":       "Export format is not supported yet: {{format}}",
	"api.books.exportNoChapters":              "There are no non-empty chapters to export.",
	"api.chat.noActiveTask":                   "No active task.",
	"api.command.empty":                       "Command is required.",
	"api.command.clearFailed":                 "Clear failed: {{detail}}",
	"api.command.cleared":                     "Context cleared. History messages are preserved.",
	"api.command.compactFailed":               "Context compaction failed: {{detail}}",
	"api.command.compacted":                   "Context compacted. epoch {{epoch}}, estimated {{before}} -> {{after}} tokens.",
	"api.command.noStatus":                    "No story state data yet. Create an outline first.",
	"api.command.unknown":                     "Unknown command: {{command}}",
	"api.command.help":                        "Available commands:\n\n  plan    - Plan before execution (/plan <request>)\n  clear   - Clear the current Agent context while keeping history\n  compact - Manually compact the current Agent context\n  status  - Show the current story state\n  help    - Show this help\n  /<skill-name> - Load a Skill in Agents that support Skills, for example /web-research\n\nType your writing idea in chat to start working with Denova.",
	"api.skills.scopeNameRequired":            "Provide scope and name.",
	"api.skills.scopeNamePathRequired":        "Provide scope, name, and path.",
	"api.skills.uploadRequired":               "Upload a Skill ZIP file.",
	"api.skills.tooLarge":                     "Skill ZIP file must be 32MB or smaller.",
	"api.skills.readFailed":                   "Failed to read Skill ZIP file: {{detail}}",
	"api.characterCard.parseFailed":           "Failed to parse Tavern character card: {{detail}}",
	"api.characterCard.uploadRequired":        "Upload a PNG or JSON Tavern character card file.",
	"api.characterCard.tooLarge":              "Character card file must be 32MB or smaller.",
	"api.characterCard.readFailed":            "Failed to read uploaded file: {{detail}}",
	"api.characterCard.invalidTarget":         "Invalid import target.",
	"api.characterCard.importFailed":          "Failed to import Tavern character card: {{detail}}",
	"api.characterCard.imported":              "Imported Tavern character card “{{name}}”.",
	"api.novelImport.parseFailed":             "Failed to parse novel file: {{detail}}",
	"api.novelImport.uploadRequired":          "Upload a txt or md novel file.",
	"api.novelImport.tooLarge":                "Novel file must be 64MB or smaller.",
	"api.novelImport.readFailed":              "Failed to read uploaded file: {{detail}}",
	"api.novelImport.importFailed":            "Failed to import novel: {{detail}}",
	"api.novelImport.imported":                "Novel import complete.",
	"api.novelImport.singleChapterWarning":    "No clear chapter title was detected. The file will be imported as one chapter.",
	"api.novelImport.agentFallbackWarning":    "Smart chapter regex detection failed. Built-in rules were used instead.",
	"api.novelImport.regexFewChaptersWarning": "The smart chapter regex found fewer than 2 chapters. Built-in rules were used instead.",
	"api.novelImport.regexFallbackWarning":    "The smart chapter regex could not be used. Built-in rules were used instead: {{detail}}",
	"api.interactive.storyIDRequired":         "Story ID is required.",
	"api.interactive.storyModeOnly":           "Only the story submode is supported now.",
	"api.interactive.storyStructureBusy":      "The story is generating. Change the protagonist or state structure after this turn finishes.",
	"api.interactive.tellerInstructionEmpty":  "Narrative direction edit instruction is required.",
	"api.lore.instructionEmpty":               "Lore edit instruction is required.",
	"api.settings.revisionConflict":           "Settings were updated by the Agent or another operation. Reload before saving.",
	"api.conversationConfig.revisionConflict": "This conversation configuration changed in another view. Reload it before saving.",
	"api.conversationConfig.rememberModelFailed": "Could not remember the model selection for new conversations. Select it again to retry.",
	"api.goal.revisionConflict":               "This goal changed in another view or Agent run. Reload it before continuing.",
	"api.goal.stateChanged":                   "The goal state changed. Reload it before continuing.",
	"api.resource.revisionConflict":           "This content was updated by the Agent or another operation. Reload before saving.",
	"api.automation.baseRevisionRequired":     "The automation update is missing its base revision. Reload before saving.",
	"api.versions.invalidCreateRequest":       "Invalid version save request.",
	"api.versions.invalidDiffComparison":      "Invalid version diff comparison.",
	"api.versions.invalidRestoreRequest":      "Invalid version restore request.",
	"api.versions.idRequired":                 "Version ID is required.",
	"api.workspace.scanFailed":                "Failed to scan the directory: {{detail}}",
	"api.workspace.summaryFailed":             "Failed to calculate writing progress: {{detail}}",
	"api.workspace.chapterStatusPathRequired": "Provide a chapter path.",
	"api.workspace.chapterStatusFailed":       "Failed to update chapter status: {{detail}}",
	"api.workspace.chapterStatusSaved":        "Chapter status updated.",
	"api.workspace.pathMissing":               "Missing path parameter.",
	"api.workspace.limitInvalid":              "limit must be a non-negative integer.",
	"api.workspace.searchFailed":              "Search failed: {{detail}}",
	"api.workspace.queryRequired":             "Provide a search query.",
	"api.workspace.invalidRegex":              "Invalid regular expression: {{detail}}",
	"api.workspace.regexMatchesEmpty":         "The regular expression must not match an empty string.",
	"api.workspace.replaceFailed":             "Global replace failed: {{detail}}",
	"api.workspace.pathContentRequired":       "Provide path and content.",
	"api.workspace.writeFailed":               "Failed to write file: {{detail}}",
	"api.workspace.fileRevisionConflict":      "The file was updated by the Agent or another operation. Reload it before saving.",
	"api.workspace.fileSaved":                 "File saved.",
	"api.workspace.targetExists":              "Target already exists.",
	"api.workspace.switched":                  "Switched to: {{workspace}}",
	"api.workspace.noWorkspace":               "No book workspace is selected. Choose or create a book in Book Management first.",
	"api.workspace.changedDuringRequest":      "The workspace changed, so this stale save was cancelled. Review the change in the current workspace and try again.",
	"api.projectFiles.projectIDRequired":      "Project ID is required.",
	"api.project.idRequired":                  "Project ID is required.",
	"api.project.idInvalid":                   "The Project ID is invalid.",
	"api.project.scopeMissing":                "The Project request context is missing.",
	"api.project.notFound":                    "The Project does not exist.",
	"api.project.archived":                    "The Project is archived.",
	"api.project.unavailable":                 "The Project directory is currently unavailable.",
	"api.project.transitioning":               "The Project is being relinked or archived. Try again shortly.",
	"api.project.resolveFailed":               "The Project could not be opened.",
	"api.attachments.scopeRequired":           "Provide a valid attachment conversation scope.",
	"api.attachments.previewUnavailable":      "The attachment image is missing or cannot be previewed.",
	"api.projectFiles.resolveTargetsRequired": "At least one project directory target is required.",
	"api.projectFiles.resolveFailed":          "Failed to read the project directory: {{detail}}",
	"api.projectFiles.notDirectory":           "The selected project path is not a directory.",
	"api.projectFiles.cursorStale":            "The directory changed while loading. Reload it and try again.",
	"api.projectFiles.invalidCursor":          "The directory continuation is invalid. Reload it and try again.",
	"api.projectFiles.invalidPath":            "The project directory path is invalid.",
	"api.projectFiles.budgetExhausted":        "The project directory request exceeded this load budget. Retry it in smaller batches.",
	"api.projectFiles.readFailed":             "Failed to read the project file: {{detail}}",
	"api.projectFiles.saveFailed":             "Failed to save the project file: {{detail}}",
	"api.projectFiles.saved":                  "Project file saved.",
	"api.projectFiles.operationsRequired":     "Provide at least one file operation.",
	"api.projectFiles.operationFailed":        "File operation failed: {{detail}}",
	"api.projectFiles.revealFailed":           "Failed to locate the project file: {{detail}}",
	"api.projectFiles.notFound":               "The project file or directory does not exist.",
	"api.projectFiles.symlinkPath":            "File operations cannot traverse symbolic links, protecting data outside the Project.",
	"api.projectBook.bookRequired":            "This resource requires a Book project.",
	"api.projectBook.readFailed":              "Failed to read the Project Book: {{detail}}",
	"api.projectBook.loreFailed":              "Lore operation failed: {{detail}}",
	"api.projectBook.loreFieldsRequired":      "Lore update is missing required fields: {{fields}}",
	"api.projectBook.loreIDMismatch":          "The Lore body ID does not match the resource path.",
	"api.projectBook.reviewFailed":            "Document review operation failed: {{detail}}",
	"api.terminal.disabled":                   "Terminal is turned off in Settings.",
	"api.terminal.notFound":                   "Terminal session not found or already ended.",
	"api.terminal.tooMany":                    "Too many terminal sessions are open. Close one and try again.",
	"api.terminal.ownerConflict":              "This terminal tab already belongs to another project. Reopen the terminal tab.",
	"api.terminal.tokenInvalid":               "Invalid terminal attach token. Reopen the terminal tab.",
	"api.terminal.invalidProfile":             "This terminal command no longer exists or is disabled. Choose it again from the new-tab menu.",
	"api.terminal.invalidLaunchCommand":       "The CLI launch command is invalid. Check the terminal command in Settings.",
	"api.settings.workspaceMissing":           "No workspace is open.",
	"api.settings.lanUsernameRequired":        "Set a username before enabling LAN access.",
	"api.settings.lanPasswordRequired":        "Set a password before enabling LAN access.",
	"api.trajectory.disabled":                 "Trajectory is unavailable.",
	"api.trajectory.invalidLimit":             "limit must be an integer from 1 to 500.",
}
