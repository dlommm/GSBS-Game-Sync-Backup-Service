@echo off
setlocal EnableExtensions EnableDelayedExpansion

set "APP_DIR=%~dp0"
set "SERVER_EXE=%APP_DIR%gsbs-server-windows-amd64.exe"
set "ENV_FILE=%ProgramData%\GSBS\server.env"
set "LOG_DIR=%ProgramData%\GSBS\logs"
set "LOG_FILE=%LOG_DIR%\gsbs-server.log"
set "SERVICE_NAME=GSBSServer"
set "SERVICE_DESC=Game Sync and Backup Service server"

set "ACTION=%~1"
if not defined ACTION set "ACTION=run"

if /I "%ACTION%"=="run" goto :run
if /I "%ACTION%"=="service-install" goto :service_install
if /I "%ACTION%"=="service-start" goto :service_start
if /I "%ACTION%"=="service-stop" goto :service_stop
if /I "%ACTION%"=="service-restart" goto :service_restart
if /I "%ACTION%"=="service-remove" goto :service_remove
if /I "%ACTION%"=="service-status" goto :service_status
if /I "%ACTION%"=="open-admin" goto :open_admin
if /I "%ACTION%"=="open-logs" goto :open_logs

echo [GSBS] Unknown action: %ACTION%
echo [GSBS] Valid actions: run, service-install, service-start, service-stop, service-restart, service-remove, service-status, open-admin, open-logs
exit /b 2

:load_env
if not exist "%ENV_FILE%" (
  echo [GSBS] Missing config file: "%ENV_FILE%"
  echo [GSBS] Run the GSBS Server installer to create it.
  exit /b 1
)
for /f "usebackq tokens=* delims=" %%L in ("%ENV_FILE%") do (
  set "line=%%L"
  if defined line if not "!line:~0,1!"=="#" if not "!line:~0,1!"==";" (
    for /f "tokens=1* delims==" %%A in ("!line!") do (
      set "key=%%~A"
      set "val=%%~B"
      if defined key set "!key!=!val!"
    )
  )
)
exit /b 0

:ensure_logs_dir
if not exist "%LOG_DIR%" mkdir "%LOG_DIR%" >nul 2>&1
exit /b 0

:service_exists
sc query "%SERVICE_NAME%" >nul 2>&1
exit /b %ERRORLEVEL%

:try_server_cmd
"%SERVER_EXE%" %*
exit /b %ERRORLEVEL%

:run
call :load_env
if errorlevel 1 exit /b 1
call :ensure_logs_dir
cd /d "%APP_DIR%"
echo [%date% %time%] starting foreground server >> "%LOG_FILE%"
"%SERVER_EXE%" >> "%LOG_FILE%" 2>&1
exit /b %ERRORLEVEL%

:service_install
call :load_env
if errorlevel 1 exit /b 1
cd /d "%APP_DIR%"
call :try_server_cmd --install-service --env-file "%ENV_FILE%"
if not errorlevel 1 exit /b 0
call :service_exists
if not errorlevel 1 exit /b 0
echo [GSBS] Failed to install Windows service.
exit /b 1

:service_start
cd /d "%APP_DIR%"
call :try_server_cmd --start-service
if not errorlevel 1 exit /b 0
sc start "%SERVICE_NAME%" >nul 2>&1
if errorlevel 1 (
  echo [GSBS] Failed to start service "%SERVICE_NAME%".
  exit /b 1
)
exit /b 0

:service_stop
cd /d "%APP_DIR%"
call :try_server_cmd --stop-service
if not errorlevel 1 exit /b 0
sc stop "%SERVICE_NAME%" >nul 2>&1
if errorlevel 1 (
  echo [GSBS] Service stop returned non-zero. It may already be stopped or missing.
)
exit /b 0

:service_restart
call :service_stop
call :service_start
exit /b %ERRORLEVEL%

:service_remove
cd /d "%APP_DIR%"
call :try_server_cmd --uninstall-service
if not errorlevel 1 exit /b 0
sc delete "%SERVICE_NAME%" >nul 2>&1
if errorlevel 1 (
  call :service_exists
  if not errorlevel 1 (
    echo [GSBS] Failed to remove service "%SERVICE_NAME%".
    exit /b 1
  )
)
exit /b 0

:service_status
sc query "%SERVICE_NAME%"
exit /b %ERRORLEVEL%

:open_admin
call :load_env
if errorlevel 1 exit /b 1
set "GSBS_URL=%GSBS_ADDR%"
if not defined GSBS_URL set "GSBS_URL=:8080"
if "%GSBS_URL:~0,1%"==":" set "GSBS_URL=127.0.0.1%GSBS_URL%"
if /I not "%GSBS_URL:~0,7%"=="http://" if /I not "%GSBS_URL:~0,8%"=="https://" (
  set "GSBS_URL=http://%GSBS_URL%"
)
start "" "%GSBS_URL%/admin"
exit /b 0

:open_logs
call :ensure_logs_dir
start "" explorer.exe "%LOG_DIR%"
exit /b 0
