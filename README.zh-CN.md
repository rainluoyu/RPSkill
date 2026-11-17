# RPSkill

[English](README.md) · 简体中文

本地文件优先的小说式 RP 工作台，由 Go Server/CLI、内嵌 Vue 前端和 Agent Skill 组成。

**用户在网页输入提示，由启动服务的同一个 Codex 对话读取世界、创作场景，再通过 Skill 内的 CLI 保存正文。** RPSkill 不另开 Agent 会话，也不维护任务或执行事件账本。

## 构建与安装

仓库只保存源码，不包含预编译程序或编译后的前端。需要 `go.mod` 指定的 Go 工具链、`web/package.json` 支持的 Node.js，以及 npm、GNU Make 和 Bash。

```sh
make build
./build/rps install-skill --target /path/to/skills
```

`make build` 会按锁文件安装前端依赖、构建 Vue、同步嵌入资源，再编译当前平台的 CLI。安装结果为 `<skills>/rp`，包含 Skill 文档及 `bin/rps`（Windows 为 `rps.exe`），不会安装到系统 `PATH`。

不使用 Make 时，也可手动执行：

```sh
npm --prefix web ci
npm --prefix web run build
bash scripts/sync-web.sh
go build -trimpath -o build/rps ./cmd/rps
./build/rps install-skill --target /path/to/skills
```

Windows 下用 Bash 环境执行同步脚本，并将输出文件设为 `build/rps.exe`。部署后的程序无需 Go 或 Node.js。打包与升级说明见[安装文档](docs/INSTALL.md)。

## 启动项目

在负责演绎故事的 Codex 对话中运行：

```sh
/path/to/skills/rp/bin/rps init /path/to/MyRP
/path/to/skills/rp/bin/rps serve /path/to/MyRP --callback codex
```

Server 从 `CODEX_THREAD_ID` 取得接收通知的原对话。手动启动时，显式传入 `--owner-thread <既有对话ID>`。打开启动时输出的带令牌链接，结束时按 Ctrl+C。没有固定服务器配置、系统服务或开机自启。

默认投递方式要求 Codex CLI 支持 `codex queue --thread ... --message ...`。RPSkill 只向既有对话排队通知，不会静默启动 App Server 或另开执行会话。队列接收成功不等于对话已消费输入。也可显式选择宿主提供的收件桥接，见[安装文档](docs/INSTALL.md)。

## 工作台使用

- **输入身份**：交流（Chat）用于剧情外沟通；导演（Director）用于场景指令；也可使用普通角色。
- **输出类型**：Chat 对应 Assistant；Director 和普通角色对应 Story。一条 Story 可包含多个人物、对白、动作和环境描写。
- **故事设置**：保存人称、润色等级（`off`、`basic`、`full`）、角色和世界书启停覆盖、关键词回看深度；媒体管理也在故事弹窗内。
- **角色控制**：普通角色实际发言后保护该角色；导演清除保护；交流不改变保护。仅选择角色不改变保护对象。
- **历史操作**：编辑消息、从指定消息删除至最新消息，或复制历史前缀为独立故事。优先加载最新消息，向上滚动懒加载历史。
- **界面语言**：主题/语言弹窗支持英文和简体中文。

Server 先保存输入，再投递通知。投递失败时会明确告知输入已保存，应在原对话中要求处理已有输入，而不是再次提交。只有最新输入接受输出。没有自动任务恢复队列、生成状态监控、历史版本检查或输出去重；写入结果不明时，先检查文件再决定是否重试。

## 文件结构

真实 RP 项目应保存在**源码仓库外**。

```text
MyRP/
  rp.yaml                         # 仅 last_chat
  worldbook/<分类>/<条目>.md
  characters/<角色>/card.md        # 可附头像和附件
  chats/<故事>/
    settings.yaml
    history.jsonl                 # 仅输入、输出
    characters/<角色>.md          # 当前故事的角色状态
    media/                        # 按需创建
```

名称取文件夹/文件名，不另存重复的 `id`、`name` 字段。附件路径相对故事或角色目录。分支把历史前缀及**当前**设置和状态复制为另一个平级故事，不重建分支时刻的旧角色状态，也不建立历史树。

**从文件读取，通过 CLI 写入。** 服务运行期间 Server 是唯一正式写入者；停止后可整体复制 Workspace 到另一台机器。故事数据不包含 Codex 会话 ID、API Token、请求账本或数据库。

## CLI 与检查

输入通知会提供私有连接文件，用 `--connection` 传入；不要把其中的令牌复制到提示词或源码中。

```sh
/path/to/skills/rp/bin/rps status --project /path/to/MyRP --connection /path/to/connection.json --json
/path/to/skills/rp/bin/rps chat settings main polishing.level '"full"' --project /path/to/MyRP --connection /path/to/connection.json
```

正式输出必须通过 `emit --chat --input --kind` 投递，详见 [Skill](skill/rp/SKILL.md)。Agent 的普通回复不会自动成为剧情正文。

执行 `make build` 后，可运行：

```sh
go test ./...
go vet ./...
npm --prefix web test
```

受控收件夹具用于验证 Server/CLI 协议，不证明模型实际生成成功。原对话真实消费、文学质量和各平台原生运行需分别确认。当前版本为 `1.0.0-rc.1`。

- [安装](docs/INSTALL.md)
- [故障排查](docs/TROUBLESHOOTING.md)
- [示例项目](examples/README.md)

采用 MIT 许可证，见 [LICENSE](LICENSE)。
