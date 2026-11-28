# 构建 Aether 的 Windows 安装包（MSI）。
# 需要：.NET SDK 与 WiX Toolset 5（MIT 协议）。
#   dotnet tool install --tool-path .\.wix wix --version 5.0.2
#   powershell -ExecutionPolicy Bypass -File installer\build-msi.ps1
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$wix = Join-Path $PSScriptRoot '.wix\wix.exe'

# 先构建编译器（MSI 内嵌 bin\aether.exe）
Push-Location $root
try {
    if (-not (Test-Path 'bin\aether.exe')) {
        go build -o bin\aether.exe .\cmd\aether
    }
    if (-not (Test-Path $wix)) {
        Write-Host '未找到 WiX，正在安装到 installer\.wix ...'
        dotnet tool install --tool-path (Join-Path $PSScriptRoot '.wix') wix --version 5.0.2
    }
    & $wix build (Join-Path $PSScriptRoot 'Aether.wxs') -o (Join-Path $PSScriptRoot 'Aether-0.4.0-x64.msi')
    & $wix msi validate (Join-Path $PSScriptRoot 'Aether-0.4.0-x64.msi')
    Write-Host '已生成 installer\Aether-0.4.0-x64.msi'
}
finally {
    Pop-Location
}
