# 五行 (Aether) 的 VS Code 插件

为 `.ae` 文件提供语法高亮、注释切换、括号匹配和自动缩进。

## 安装

把整个目录复制到 VS Code 的扩展目录，然后重启 VS Code：

```powershell
Copy-Item -Recurse E:\Aether\editors\vscode "$env:USERPROFILE\.vscode\extensions\aether-0.1.0"
```

也可以用 `vsce package` 打包成 `.vsix`，再通过“从 VSIX 安装”安装。
