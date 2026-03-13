$App_Name = "mybot"
$Build_Dir = "build_output"
$Src = ".\cmd\bot"

# Create output folder
if (Test-Path $Build_Dir) {
    Remove-Item -Recurse -Force -Path $Build_Dir
}
New-Item -ItemType Directory -Force -Path $Build_Dir | Out-Null

# Define version based on git (if available) - optional but nice
$GitVersion = ""
try {
    $GitVersion = git describe --tags --always --dirty 2>$null
}
catch {}
if (-not $GitVersion) {
    $GitVersion = "dev"
}
$LdFlags = "-s -w -X mybot/internal/commands.BotVersion=$GitVersion"

# Build Windows
Write-Host "Building for Windows..." -ForegroundColor Cyan
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -ldflags $LdFlags -trimpath -o "$Build_Dir\$App_Name-windows.exe" $Src
if ($LASTEXITCODE -ne 0) { Write-Host "Windows build failed!" -ForegroundColor Red; exit 1 }

# Build Linux
Write-Host "Building for Linux..." -ForegroundColor Cyan
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -ldflags $LdFlags -trimpath -o "$Build_Dir\$App_Name-linux" $Src
if ($LASTEXITCODE -ne 0) { Write-Host "Linux build failed!" -ForegroundColor Red; exit 1 }

# Copy sample config
Write-Host "Copying config.example.json to config.json..." -ForegroundColor Cyan
Copy-Item ".\config.example.json" -Destination "$Build_Dir\config.json"

# Create modules directory and copy script modules.
# Each subfolder with a command.go = an editable command (no recompilation needed).
$ModulesDir = "$Build_Dir\modules"
New-Item -ItemType Directory -Force -Path $ModulesDir | Out-Null

# Copy script modules from source
$ScriptSrc = ".\modules"
if (Test-Path $ScriptSrc) {
    Copy-Item -Path "$ScriptSrc\*" -Destination $ModulesDir -Recurse -Force
    Write-Host "Copied script modules from modules/ into build output." -ForegroundColor Cyan
}

# Create data directory for runtime storage (SQLite DB, etc.)
$DataDir = "$Build_Dir\data"
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
Write-Host "Created data/ directory." -ForegroundColor Cyan

# If UPX is installed, compress the generated binaries to reduce size
$upxCmd = Get-Command upx -ErrorAction SilentlyContinue
if ($upxCmd) {
    Write-Host "Compressing binaries with UPX (best)..." -ForegroundColor Cyan
    & upx --best --lzma "$Build_Dir\$App_Name-windows.exe"
    if ($LASTEXITCODE -ne 0) { Write-Host "UPX failed on Windows binary" -ForegroundColor Yellow }
    & upx --best --lzma "$Build_Dir\$App_Name-linux"
    if ($LASTEXITCODE -ne 0) { Write-Host "UPX failed on Linux binary" -ForegroundColor Yellow }
} else {
    Write-Host "UPX not found: binaries will NOT be compressed." -ForegroundColor Yellow
    Write-Host "To install UPX on Windows: `choco install upx` or visit https://upx.github.io/" -ForegroundColor Yellow
}

Write-Host "Done! All files are in the '$Build_Dir' folder." -ForegroundColor Green
