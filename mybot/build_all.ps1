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

Write-Host "Done! All files are in the '$Build_Dir' folder." -ForegroundColor Green
