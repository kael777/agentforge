@echo off
setlocal EnableExtensions
set "SCRIPT_DIR=%~dp0"
for %%I in ("%SCRIPT_DIR%..\..") do set "DEMO_ROOT=%%~fI"
set "BACKEND_DIR=%DEMO_ROOT%\backend\api"
set "OUTPUT_ROOT=%DEMO_ROOT%\dist"
cd /d "%BACKEND_DIR%"

set "TARGET=%~1"
if not defined TARGET set "TARGET=all"
set "TARGET_ARCH=%~2"
if not defined TARGET_ARCH set "TARGET_ARCH=amd64"

if not defined VERSION (
  for /f "delims=" %%V in ('git describe --tags --always --dirty 2^>nul') do set "VERSION=%%V"
)
if not defined VERSION set "VERSION=dev"

if not defined COMMIT (
  for /f "delims=" %%C in ('git rev-parse --short HEAD 2^>nul') do set "COMMIT=%%C"
)
if not defined COMMIT set "COMMIT=unknown"

if not defined BUILD_TIME (
  for /f "delims=" %%T in ('powershell -NoProfile -Command "[DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')"') do set "BUILD_TIME=%%T"
)

if /I "%TARGET%"=="all" (
  call :build windows amd64 || exit /b 1
  call :build linux amd64 || exit /b 1
  call :build linux arm64 || exit /b 1
  call :build darwin amd64 || exit /b 1
  call :build darwin arm64 || exit /b 1
  goto :success
)

if /I not "%TARGET%"=="windows" if /I not "%TARGET%"=="linux" if /I not "%TARGET%"=="darwin" (
  echo Unsupported target: %TARGET%
  echo Usage: build.bat [all^|windows^|linux^|darwin] [amd64^|arm64]
  exit /b 2
)

call :build "%TARGET%" "%TARGET_ARCH%" || exit /b 1

:success
echo.
echo Build completed. Output: %OUTPUT_ROOT%
exit /b 0

:build
set "BUILD_OS=%~1"
set "BUILD_ARCH=%~2"
set "EXT="
if /I "%BUILD_OS%"=="windows" set "EXT=.exe"
set "OUT_DIR=%OUTPUT_ROOT%\%BUILD_OS%-%BUILD_ARCH%"
if not exist "%OUT_DIR%" mkdir "%OUT_DIR%"

echo Building %BUILD_OS%/%BUILD_ARCH%...
set "CGO_ENABLED=0"
set "GOOS=%BUILD_OS%"
set "GOARCH=%BUILD_ARCH%"
set "API_LDFLAGS=-s -w -X github.com/kael777/agentforge/backend/api/internal/buildinfo.Version=%VERSION% -X github.com/kael777/agentforge/backend/api/internal/buildinfo.Commit=%COMMIT% -X github.com/kael777/agentforge/backend/api/internal/buildinfo.BuildTime=%BUILD_TIME%"

go build -trimpath -ldflags "%API_LDFLAGS%" -o "%OUT_DIR%\api%EXT%" ./cmd/api
if errorlevel 1 exit /b 1
go build -trimpath -ldflags "-s -w" -o "%OUT_DIR%\migrate%EXT%" ./cmd/migrate
if errorlevel 1 exit /b 1
exit /b 0
