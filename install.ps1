# 构建五行 (Aether) 编译器：  powershell -ExecutionPolicy Bypass -File install.ps1 [-AddToPath]
param([switch]$AddToPath)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "找不到 go 命令。请先安装 Go 1.23 或更新版本：https://go.dev/dl/"
}

# 如果 GOCACHE 指向的目录无法创建（例如所在的盘不存在），改用五行自己的缓存目录
if ($env:GOCACHE) {
    try {
        New-Item -ItemType Directory -Force -Path $env:GOCACHE -ErrorAction Stop | Out-Null
    } catch {
        $env:GOCACHE = Join-Path $env:LOCALAPPDATA "aether\go-build"
        Write-Host "GOCACHE 不可用，临时改用 $env:GOCACHE"
    }
}
$env:GOTOOLCHAIN = "local"
$env:CGO_ENABLED = "0"

Push-Location $root
try {
    go build -o bin\aether.exe .\cmd\aether
    if ($LASTEXITCODE -ne 0) { throw "构建失败" }
    Write-Host "已生成 $root\bin\aether.exe"
    & "$root\bin\aether.exe" run examples\hello.ae
} finally {
    Pop-Location
}

if ($AddToPath) {
    $bin = Join-Path $root "bin"
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (($userPath -split ";") -notcontains $bin) {
        [Environment]::SetEnvironmentVariable("Path", "$userPath;$bin", "User")
        Write-Host "已把 $bin 加入用户 PATH（重新打开终端后生效）"
    } else {
        Write-Host "$bin 已经在 PATH 中"
    }
}
