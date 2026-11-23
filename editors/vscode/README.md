# 五行 (Aether) 的 VS Code 插件

- 语法高亮、注释切换、括号匹配、自动缩进；
- 通过 `aether lsp`（语言服务器）提供：
  - **实时诊断**：边输入边显示中文的错误、警告和修改提示；
  - **悬停**：显示变量的类型（包括窄化后的类型）、函数与方法的签名、字段和属性；
  - **跳转到定义**（F12）：包括其它模块中的函数和类型；
  - **补全**：输入 `.` 后列出成员（字段、方法、属性、扩展方法、模块成员），其它位置列出可用的名字和关键字；
  - **大纲**：文件中的函数、类型及其成员。

插件本身不依赖任何 npm 包。

## 安装

1. 安装五行，并确保 `aether` 在 PATH 中（`install.ps1 -AddToPath`）；也可以在设置中把 `aether.path` 设为 `aether.exe` 的完整路径。在五行的仓库中打开时，会自动使用仓库里的 `bin/aether.exe`。
2. 把整个目录复制到 VS Code 的扩展目录，然后重启 VS Code：

```powershell
Copy-Item -Recurse E:\Aether\editors\vscode "$env:USERPROFILE\.vscode\extensions\aether-0.2.0"
```

也可以用 `vsce package` 打包成 `.vsix`，再通过“从 VSIX 安装”安装。

如果语言服务器出了问题，可以在命令面板中运行“五行：重启语言服务器”；它的输出在“输出”面板的“五行 (Aether)”频道中。
