@echo off
setlocal EnableExtensions

rem 按端口释放调试残留进程；没有匹配端口时也正常退出，避免阻塞 VS Code/Cursor 的 preLaunchTask。
for %%P in (%*) do (
  for /f "tokens=5" %%A in ('netstat -ano ^| findstr /R /C:":%%P .*LISTENING"') do (
    taskkill /PID %%A /F >nul 2>nul
  )
)

exit /b 0
