Unicode true

!include /NONFATAL "build_params.nsh.tmp"

!ifndef INFO_PROJECTNAME
!define INFO_PROJECTNAME "MaClaw"
!endif
!ifndef INFO_COMPANYNAME
    !define INFO_COMPANYNAME "RapidAI"
!endif
!ifndef INFO_PRODUCTNAME
!define INFO_PRODUCTNAME "MaClaw"
!endif
!ifndef INFO_PRODUCTVERSION
    !define INFO_PRODUCTVERSION "5.4.2.9920"
!endif
!ifndef INFO_COPYRIGHT
    !define INFO_COPYRIGHT "Copyright (C) 2026 RapidAI"
!endif
!ifndef PRODUCT_EXECUTABLE
!define PRODUCT_EXECUTABLE "MaClaw.exe"
!endif
!ifndef REQUEST_EXECUTION_LEVEL
    !define REQUEST_EXECUTION_LEVEL "admin"
!endif
!ifndef AUTOSTART_REG_NAME
!define AUTOSTART_REG_NAME "${INFO_PROJECTNAME}"
!endif

# Define Wails binaries (passed from command line or hardcoded here for manual build)
!ifndef ARG_WAILS_AMD64_BINARY
!define ARG_WAILS_AMD64_BINARY "..\..\..\dist\MaClaw_amd64.exe"
!endif
!ifndef ARG_WAILS_ARM64_BINARY
!define ARG_WAILS_ARM64_BINARY "..\..\..\dist\MaClaw_arm64.exe"
!endif
!ifndef ARG_MACLAWCLI_AMD64_BINARY
!define ARG_MACLAWCLI_AMD64_BINARY "..\..\..\dist\maclaw-cli_amd64.exe"
!endif
!ifndef ARG_MACLAWCLI_ARM64_BINARY
!define ARG_MACLAWCLI_ARM64_BINARY "..\..\..\dist\maclaw-cli_arm64.exe"
!endif

VIProductVersion "${INFO_PRODUCTVERSION}"
VIFileVersion    "${INFO_PRODUCTVERSION}"
VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI2.nsh"
!include "x64.nsh"
!include "WinMessages.nsh"

!ifndef MUI_ICON_PATH
!define MUI_ICON_PATH "..\icon.ico"
!endif
!define MUI_ICON "${MUI_ICON_PATH}"
!define MUI_UNICON "${MUI_ICON_PATH}"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

# Launch app checkbox on finish page (checked by default)
# Use ShellExec to avoid launching app with admin privileges
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "$(LaunchAfterInstall)"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchAsCurrentUser

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES

# Languages - order matters: first language is the fallback default
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "TradChinese"
!insertmacro MUI_LANGUAGE "Japanese"
!insertmacro MUI_LANGUAGE "Korean"
!insertmacro MUI_LANGUAGE "French"
!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "Spanish"
!insertmacro MUI_LANGUAGE "Russian"

# Localized strings for finish page
LangString LaunchAfterInstall ${LANG_ENGLISH} "Launch ${INFO_PRODUCTNAME}"
LangString LaunchAfterInstall ${LANG_SIMPCHINESE} "启动 ${INFO_PRODUCTNAME}"
LangString LaunchAfterInstall ${LANG_TRADCHINESE} "啟動 ${INFO_PRODUCTNAME}"
LangString LaunchAfterInstall ${LANG_JAPANESE} "${INFO_PRODUCTNAME} を起動"
LangString LaunchAfterInstall ${LANG_KOREAN} "${INFO_PRODUCTNAME} 실행"
LangString LaunchAfterInstall ${LANG_FRENCH} "Lancer ${INFO_PRODUCTNAME}"
LangString LaunchAfterInstall ${LANG_GERMAN} "${INFO_PRODUCTNAME} starten"
LangString LaunchAfterInstall ${LANG_SPANISH} "Iniciar ${INFO_PRODUCTNAME}"
LangString LaunchAfterInstall ${LANG_RUSSIAN} "Запустить ${INFO_PRODUCTNAME}"

# Localized strings for already-installed dialog
LangString AlreadyInstalled ${LANG_ENGLISH} "${INFO_PRODUCTNAME} is already installed. Do you want to uninstall it first?"
LangString AlreadyInstalled ${LANG_SIMPCHINESE} "${INFO_PRODUCTNAME} 已安装。是否先卸载？"
LangString AlreadyInstalled ${LANG_TRADCHINESE} "${INFO_PRODUCTNAME} 已安裝。是否先解除安裝？"
LangString AlreadyInstalled ${LANG_JAPANESE} "${INFO_PRODUCTNAME} は既にインストールされています。先にアンインストールしますか？"
LangString AlreadyInstalled ${LANG_KOREAN} "${INFO_PRODUCTNAME}이(가) 이미 설치되어 있습니다. 먼저 제거하시겠습니까?"
LangString AlreadyInstalled ${LANG_FRENCH} "${INFO_PRODUCTNAME} est déjà installé. Voulez-vous le désinstaller d'abord ?"
LangString AlreadyInstalled ${LANG_GERMAN} "${INFO_PRODUCTNAME} ist bereits installiert. Möchten Sie es zuerst deinstallieren?"
LangString AlreadyInstalled ${LANG_SPANISH} "${INFO_PRODUCTNAME} ya está instalado. ¿Desea desinstalarlo primero?"
LangString AlreadyInstalled ${LANG_RUSSIAN} "${INFO_PRODUCTNAME} уже установлен. Удалить сначала?"

# Localized strings for running app dialog
LangString AppIsRunning ${LANG_ENGLISH} "${INFO_PRODUCTNAME} is still running. Stop it and continue installing?"
LangString AppIsRunning ${LANG_SIMPCHINESE} "${INFO_PRODUCTNAME} 正在运行。是否停止旧进程并继续安装？"
LangString AppIsRunning ${LANG_TRADCHINESE} "${INFO_PRODUCTNAME} 正在執行。是否停止舊行程並繼續安裝？"
LangString AppIsRunning ${LANG_JAPANESE} "${INFO_PRODUCTNAME} はまだ実行中です。停止してインストールを続行しますか？"
LangString AppIsRunning ${LANG_KOREAN} "${INFO_PRODUCTNAME}이(가) 아직 실행 중입니다. 중지하고 설치를 계속하시겠습니까?"
LangString AppIsRunning ${LANG_FRENCH} "${INFO_PRODUCTNAME} est encore en cours d'exécution. L'arrêter et continuer l'installation ?"
LangString AppIsRunning ${LANG_GERMAN} "${INFO_PRODUCTNAME} wird noch ausgeführt. Beenden und Installation fortsetzen?"
LangString AppIsRunning ${LANG_SPANISH} "${INFO_PRODUCTNAME} aún se está ejecutando. ¿Detenerlo y continuar la instalación?"
LangString AppIsRunning ${LANG_RUSSIAN} "${INFO_PRODUCTNAME} всё ещё запущен. Остановить и продолжить установку?"

LangString AppStillRunning ${LANG_ENGLISH} "${INFO_PRODUCTNAME} could not be stopped. Please close it and run the installer again."
LangString AppStillRunning ${LANG_SIMPCHINESE} "无法停止 ${INFO_PRODUCTNAME}。请手动关闭后重新运行安装程序。"
LangString AppStillRunning ${LANG_TRADCHINESE} "無法停止 ${INFO_PRODUCTNAME}。請手動關閉後重新執行安裝程式。"
LangString AppStillRunning ${LANG_JAPANESE} "${INFO_PRODUCTNAME} を停止できませんでした。手動で閉じてからインストーラーを再実行してください。"
LangString AppStillRunning ${LANG_KOREAN} "${INFO_PRODUCTNAME}을(를) 중지할 수 없습니다. 직접 닫은 후 설치 프로그램을 다시 실행해 주세요."
LangString AppStillRunning ${LANG_FRENCH} "Impossible d'arrêter ${INFO_PRODUCTNAME}. Fermez-le manuellement puis relancez l'installation."
LangString AppStillRunning ${LANG_GERMAN} "${INFO_PRODUCTNAME} konnte nicht beendet werden. Bitte manuell schließen und Installer erneut ausführen."
LangString AppStillRunning ${LANG_SPANISH} "No se pudo detener ${INFO_PRODUCTNAME}. Ciérrelo manualmente y vuelva a ejecutar el instalador."
LangString AppStillRunning ${LANG_RUSSIAN} "Не удалось остановить ${INFO_PRODUCTNAME}. Закройте его вручную и запустите установщик снова."

# Localized strings for uninstall user data dialog
LangString DeleteUserData ${LANG_ENGLISH} "Do you want to delete user data (.cceasy and part of .maclaw)?$\n$\nThis will remove AI tools and cache.$\nNote: .maclaw/data, .maclaw/models, config.json and memories.json will be preserved."
LangString DeleteUserData ${LANG_SIMPCHINESE} "是否删除用户数据（.cceasy 和部分 .maclaw 内容）？$\n$\n这将删除 AI 工具和缓存。$\n注意：.maclaw/data、.maclaw/models、config.json 和 memories.json 将被保留。"
LangString DeleteUserData ${LANG_TRADCHINESE} "是否刪除使用者資料（.cceasy 和部分 .maclaw 內容）？$\n$\n這將刪除 AI 工具和快取。$\n注意：.maclaw/data、.maclaw/models、config.json 和 memories.json 將被保留。"
LangString DeleteUserData ${LANG_JAPANESE} "ユーザーデータ（.cceasy と .maclaw の一部）を削除しますか？$\n$\nAIツールとキャッシュが削除されます。$\n注意：.maclaw/data、.maclaw/models、config.json、memories.json は保持されます。"
LangString DeleteUserData ${LANG_KOREAN} "사용자 데이터(.cceasy 및 .maclaw 일부)를 삭제하시겠습니까?$\n$\nAI 도구 및 캐시가 삭제됩니다.$\n참고: .maclaw/data, .maclaw/models, config.json 및 memories.json은 보존됩니다."
LangString DeleteUserData ${LANG_FRENCH} "Voulez-vous supprimer les données utilisateur (.cceasy et une partie de .maclaw) ?$\n$\nCela supprimera les outils IA et le cache.$\nNote : .maclaw/data, .maclaw/models, config.json et memories.json seront conservés."
LangString DeleteUserData ${LANG_GERMAN} "Möchten Sie die Benutzerdaten (.cceasy und einen Teil von .maclaw) löschen?$\n$\nDies entfernt KI-Tools und Cache.$\nHinweis: .maclaw/data, .maclaw/models, config.json und memories.json werden beibehalten."
LangString DeleteUserData ${LANG_SPANISH} "¿Desea eliminar los datos de usuario (.cceasy y parte de .maclaw)?$\n$\nEsto eliminará herramientas IA y caché.$\nNota: .maclaw/data, .maclaw/models, config.json y memories.json se conservarán."
LangString DeleteUserData ${LANG_RUSSIAN} "Удалить пользовательские данные (.cceasy и часть .maclaw)?$\n$\nБудут удалены ИИ-инструменты и кэш.$\nПримечание: .maclaw/data, .maclaw/models, config.json и memories.json будут сохранены."

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\dist\${INFO_PROJECTNAME}-Setup.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show
RequestExecutionLevel ${REQUEST_EXECUTION_LEVEL}

# Launch app as current user (not elevated admin)
Function LaunchAsCurrentUser
    ExecShell "" "$INSTDIR\${PRODUCT_EXECUTABLE}"
FunctionEnd

Function AddInstallDirToMachinePath
    # Read current system PATH, append $INSTDIR if not already present.
    # Uses semicolon-delimited entry matching (case-insensitive) to avoid
    # substring false-positives (e.g. "TigerClaw" vs "TigerClawOld").
    ReadRegStr $0 HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path"
    # Pre-compute normalized INSTDIR (without trailing backslash)
    Push $INSTDIR
    Call TrimBackslash
    Pop $7
    StrCmp $0 "" _AddPath_empty
    # Iterate PATH entries separated by ';', compare case-insensitively
    StrCpy $1 $0
    _AddPath_check_loop:
        # Find next ';'
        StrCpy $3 0
        StrLen $4 $1
        _AddPath_find_semi:
            IntCmp $3 $4 _AddPath_found_end _AddPath_found_end
            StrCpy $5 $1 1 $3
            StrCmp $5 ";" _AddPath_found_semi
            IntOp $3 $3 + 1
            Goto _AddPath_find_semi
        _AddPath_found_semi:
            StrCpy $2 $1 $3       ; $2 = current entry
            IntOp $3 $3 + 1
            StrCpy $1 $1 "" $3    ; $1 = remainder after ';'
            Goto _AddPath_compare
        _AddPath_found_end:
            StrCpy $2 $1          ; $2 = last entry (no trailing ;)
            StrCpy $1 ""          ; no remainder
        _AddPath_compare:
        # Skip empty entries (from consecutive semicolons)
        StrCmp $2 "" _AddPath_next_entry
        # Case-insensitive compare (StrCmp is case-insensitive in NSIS)
        Push $2
        Call TrimBackslash
        Pop $6
        StrCmp $6 $7 _AddPath_already_exists _AddPath_next_entry
        _AddPath_next_entry:
        StrCmp $1 "" _AddPath_not_found
        Goto _AddPath_check_loop
    _AddPath_not_found:
    # Not found — append
    WriteRegExpandStr HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path" "$0;$INSTDIR"
    Goto _AddPath_done
    _AddPath_empty:
    WriteRegExpandStr HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path" "$INSTDIR"
    Goto _AddPath_done
    _AddPath_already_exists:
    _AddPath_done:
    SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
FunctionEnd

; TrimBackslash — remove trailing '\' from string on stack
Function TrimBackslash
    Exch $R0
    Push $R1
    StrLen $R1 $R0
    IntCmp $R1 0 _TrimBS_done _TrimBS_done
    IntOp $R1 $R1 - 1
    StrCpy $R1 $R0 1 $R1
    StrCmp $R1 "\" 0 _TrimBS_done
        StrLen $R1 $R0
        IntOp $R1 $R1 - 1
        StrCpy $R0 $R0 $R1
    _TrimBS_done:
    Pop $R1
    Exch $R0
FunctionEnd

Function un.RemoveInstallDirFromMachinePath
    # Read current system PATH, rebuild without $INSTDIR entry.
    # Iterates entries separated by ';', compares case-insensitively with trailing '\' normalization.
    ReadRegStr $0 HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path"
    StrCmp $0 "" _RemPath_done
    # Normalize $INSTDIR for comparison
    Push $INSTDIR
    Call un.TrimBackslash
    Pop $7              ; $7 = normalized INSTDIR
    StrCpy $1 $0       ; $1 = remaining input
    StrCpy $8 ""       ; $8 = rebuilt PATH (output)
    _RemPath_loop:
        StrCmp $1 "" _RemPath_write
        # Find next ';'
        StrCpy $3 0
        StrLen $4 $1
        _RemPath_find_semi:
            IntCmp $3 $4 _RemPath_last_entry _RemPath_last_entry
            StrCpy $5 $1 1 $3
            StrCmp $5 ";" _RemPath_got_semi
            IntOp $3 $3 + 1
            Goto _RemPath_find_semi
        _RemPath_got_semi:
            StrCpy $2 $1 $3       ; $2 = current entry
            IntOp $3 $3 + 1
            StrCpy $1 $1 "" $3    ; $1 = remainder
            Goto _RemPath_test
        _RemPath_last_entry:
            StrCpy $2 $1
            StrCpy $1 ""
        _RemPath_test:
        # Skip empty entries (from consecutive semicolons)
        StrCmp $2 "" _RemPath_loop
        # Compare entry (trimmed) against normalized INSTDIR
        Push $2
        Call un.TrimBackslash
        Pop $6
        StrCmp $6 $7 _RemPath_loop  ; match → skip this entry
        # No match → keep entry
        StrCmp $8 "" 0 +3
            StrCpy $8 $2
            Goto _RemPath_loop
        StrCpy $8 "$8;$2"
        Goto _RemPath_loop
    _RemPath_write:
    WriteRegExpandStr HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path" "$8"
    _RemPath_done:
    SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
FunctionEnd

; un.TrimBackslash — remove trailing '\' from string on stack (uninstaller copy)
Function un.TrimBackslash
    Exch $R0
    Push $R1
    StrLen $R1 $R0
    IntCmp $R1 0 _unTrimBS_done _unTrimBS_done
    IntOp $R1 $R1 - 1
    StrCpy $R1 $R0 1 $R1
    StrCmp $R1 "\" 0 _unTrimBS_done
        StrLen $R1 $R0
        IntOp $R1 $R1 - 1
        StrCpy $R0 $R0 $R1
    _unTrimBS_done:
    Pop $R1
    Exch $R0
FunctionEnd

Function .onInit
    # Auto-detect system language (no dialog)
    System::Call 'kernel32::GetUserDefaultUILanguage() i .r0'
    # Chinese Simplified: 0x0804, Traditional: 0x0404, Japanese: 0x0411, Korean: 0x0412
    # French: 0x040C, German: 0x0407, Spanish: 0x0C0A, Russian: 0x0419
    StrCmp $0 "2052" lang_zh_cn
    StrCmp $0 "1028" lang_zh_tw
    StrCmp $0 "1041" lang_ja
    StrCmp $0 "1042" lang_ko
    StrCmp $0 "1036" lang_fr
    StrCmp $0 "1031" lang_de
    StrCmp $0 "3082" lang_es
    StrCmp $0 "1049" lang_ru
    Goto lang_en

    lang_zh_cn:
        StrCpy $LANGUAGE ${LANG_SIMPCHINESE}
        Goto lang_done
    lang_zh_tw:
        StrCpy $LANGUAGE ${LANG_TRADCHINESE}
        Goto lang_done
    lang_ja:
        StrCpy $LANGUAGE ${LANG_JAPANESE}
        Goto lang_done
    lang_ko:
        StrCpy $LANGUAGE ${LANG_KOREAN}
        Goto lang_done
    lang_fr:
        StrCpy $LANGUAGE ${LANG_FRENCH}
        Goto lang_done
    lang_de:
        StrCpy $LANGUAGE ${LANG_GERMAN}
        Goto lang_done
    lang_es:
        StrCpy $LANGUAGE ${LANG_SPANISH}
        Goto lang_done
    lang_ru:
        StrCpy $LANGUAGE ${LANG_RUSSIAN}
        Goto lang_done
    lang_en:
        StrCpy $LANGUAGE ${LANG_ENGLISH}
    lang_done:

    # Stop old running GUI only after user confirms; otherwise exit.
    nsExec::ExecToStack 'cmd /c tasklist /FI "IMAGENAME eq ${PRODUCT_EXECUTABLE}" /NH | find /I "${PRODUCT_EXECUTABLE}" >nul'
    Pop $R1
    StrCmp $R1 "0" appRunning appNotRunning

    appRunning:
    MessageBox MB_YESNO|MB_ICONEXCLAMATION "$(AppIsRunning)" IDYES stopApp
    Abort

    stopApp:
    ExecWait 'taskkill /F /IM ${PRODUCT_EXECUTABLE}'
    Sleep 1000
    nsExec::ExecToStack 'cmd /c tasklist /FI "IMAGENAME eq ${PRODUCT_EXECUTABLE}" /NH | find /I "${PRODUCT_EXECUTABLE}" >nul'
    Pop $R1
    StrCmp $R1 "0" appStillRunning appNotRunning

    appStillRunning:
    MessageBox MB_OK|MB_ICONSTOP "$(AppStillRunning)"
    Abort

    appNotRunning:
    # Stop helper CLI if a long-running watch/poll is active before upgrade.
    ExecWait 'taskkill /F /IM maclaw-cli.exe'

    # Check if already installed
    ReadRegStr $R0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "UninstallString"
    StrCmp $R0 "" notInstalled
    # Preserve user's original install directory if they chose a custom path
    ReadRegStr $R1 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "InstallLocation"
    StrCmp $R1 "" +2
        StrCpy $INSTDIR $R1
    MessageBox MB_YESNO|MB_ICONEXCLAMATION "$(AlreadyInstalled)" IDYES uninstall
    Abort
    
    uninstall:
    ExecWait '"$R0" /S _?=$INSTDIR'
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"
    
    notInstalled:
FunctionEnd

Section
    SetShellVarContext all
    SetOutPath $INSTDIR

    # Architecture detection and file installation
    ${If} ${IsNativeARM64}
        DetailPrint "Detected ARM64 Architecture"
        File "/oname=${PRODUCT_EXECUTABLE}" "${ARG_WAILS_ARM64_BINARY}"
        File "/oname=maclaw-cli.exe" "${ARG_MACLAWCLI_ARM64_BINARY}"
    ${ElseIf} ${IsNativeAMD64}
        DetailPrint "Detected AMD64 Architecture"
        File "/oname=${PRODUCT_EXECUTABLE}" "${ARG_WAILS_AMD64_BINARY}"
        File "/oname=maclaw-cli.exe" "${ARG_MACLAWCLI_AMD64_BINARY}"
    ${Else}
        MessageBox MB_OK|MB_ICONSTOP "Unsupported architecture."
        Abort
    ${EndIf}

    # Install other assets if any (e.g., from wails.json assets or specific files)
    # File "..\..\frontend\dist\..." # Frontend is embedded in binary

    # Enable Windows Long Path Support (required for npm cache and AI tools)
    DetailPrint "Enabling Windows Long Path Support..."
    WriteRegDWORD HKLM "SYSTEM\CurrentControlSet\Control\FileSystem" "LongPathsEnabled" 1

    # Make bundled CLI discoverable by subprocess-based agents.
    DetailPrint "Adding install directory to machine PATH for maclaw-cli..."
    Call AddInstallDirToMachinePath

    # Create Shortcuts
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    
    # Taskbar pinning is restricted by Windows. 
    # We can't programmatically pin to taskbar reliably on Win10/11 without using non-standard methods.
    
    # Write Uninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"
    
    # Registry keys for Add/Remove programs
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "DisplayVersion" "${INFO_PRODUCTVERSION}"

    # Start automatically after Windows sign-in
    SetRegView 64
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Run" "${AUTOSTART_REG_NAME}" "$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\" autostart"
SectionEnd

Section "uninstall"
    SetShellVarContext all
    
    # Kill app if running
    ExecWait "taskkill /F /IM ${PRODUCT_EXECUTABLE}"
    ExecWait "taskkill /F /IM maclaw-cli.exe"

    # Remove WebView2 user data directory (Wails stores it in %APPDATA%\<exe_name>)
    RMDir /r "$APPDATA\${PRODUCT_EXECUTABLE}"

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\maclaw-cli.exe"
    Delete "$INSTDIR\uninstall.exe"
    # Remove install dir — use /r to handle any leftover files (logs, crash dumps, etc.)
    RMDir /r "$INSTDIR"

    Call un.RemoveInstallDirFromMachinePath

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}"
    SetRegView 64
    DeleteRegValue HKLM "Software\Microsoft\Windows\CurrentVersion\Run" "${AUTOSTART_REG_NAME}"

    # Ask user if they want to delete user data
    # In silent mode (/S), skip user data deletion — silent uninstall is typically
    # triggered by the installer during upgrade, where we must preserve user data.
    IfSilent skipUserData
    MessageBox MB_YESNO|MB_ICONQUESTION "$(DeleteUserData)" IDYES deleteUserData IDNO skipUserData
    
    deleteUserData:
    # Delete user data directories
    DetailPrint "Deleting user data directories..."
    nsExec::ExecToLog 'cmd /c rd /s /q "$PROFILE\.cceasy"'

    # Preserve .maclaw/data, .maclaw/models, config.json and memories.json while cleaning other .maclaw content
    DetailPrint "Cleaning .maclaw while preserving data, models, config.json and memories.json..."
    nsExec::ExecToLog 'powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$$d=Join-Path $$env:USERPROFILE ''.maclaw''; if(Test-Path $$d){Get-ChildItem $$d -Directory | Where-Object{$$_.Name -notin @(''data'',''models'')} | Remove-Item -Recurse -Force; Get-ChildItem $$d -File | Where-Object{$$_.Name -notin @(''memories.json'',''config.json'')} | Remove-Item -Force}"'

    skipUserData:
SectionEnd

