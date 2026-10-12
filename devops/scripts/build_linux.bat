@echo off
call "%~dp0build.bat" linux %*
exit /b %errorlevel%
