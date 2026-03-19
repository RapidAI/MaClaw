Unicode true

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
    !define INFO_PRODUCTVERSION "4.1.0.9200"
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

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
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

# Localized strings for uninstall user data dialog
LangString DeleteUserData ${LANG_ENGLISH} "Do you want to delete user data (.cceasy and .maclaw folders)?$\n$\nThis will remove AI tools, configurations and cache.$\nNote: Your memory file (memories.json) will be preserved."
LangString DeleteUserData ${LANG_SIMPCHINESE} "是否删除用户数据（.cceasy 和 .maclaw 文件夹）？$\n$\n这将删除 AI 工具、配置和缓存。$\n注意：记忆文件（memories.json）将被保留。"
LangString DeleteUserData ${LANG_TRADCHINESE} "是否刪除使用者資料（.cceasy 和 .maclaw 資料夾）？$\n$\n這將刪除 AI 工具、設定和快取。$\n注意：記憶檔案（memories.json）將被保留。"
LangString DeleteUserData ${LANG_JAPANESE} "ユーザーデータ（.cceasy と .maclaw フォルダ）を削除しますか？$\n$\nAIツール、設定、キャッシュが削除されます。$\n注意：メモリファイル（memories.json）は保持されます。"
LangString DeleteUserData ${LANG_KOREAN} "사용자 데이터(.cceasy 및 .maclaw 폴더)를 삭제하시겠습니까?$\n$\nAI 도구, 설정 및 캐시가 삭제됩니다.$\n참고: 메모리 파일(memories.json)은 보존됩니다."
LangString DeleteUserData ${LANG_FRENCH} "Voulez-vous supprimer les données utilisateur (dossiers .cceasy et .maclaw) ?$\n$\nCela supprimera les outils IA, configurations et cache.$\nNote : Le fichier mémoire (memories.json) sera conservé."
LangString DeleteUserData ${LANG_GERMAN} "Möchten Sie die Benutzerdaten (.cceasy und .maclaw Ordner) löschen?$\n$\nDies entfernt KI-Tools, Konfigurationen und Cache.$\nHinweis: Die Speicherdatei (memories.json) wird beibehalten."
LangString DeleteUserData ${LANG_SPANISH} "¿Desea eliminar los datos de usuario (carpetas .cceasy y .maclaw)?$\n$\nEsto eliminará herramientas IA, configuraciones y caché.$\nNota: El archivo de memoria (memories.json) se conservará."
LangString DeleteUserData ${LANG_RUSSIAN} "Удалить пользовательские данные (папки .cceasy и .maclaw)?$\n$\nБудут удалены ИИ-инструменты, настройки и кэш.$\nПримечание: Файл памяти (memories.json) будет сохранён."

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\dist\${INFO_PROJECTNAME}-Setup.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show
RequestExecutionLevel ${REQUEST_EXECUTION_LEVEL}

# Launch app as current user (not elevated admin)
Function LaunchAsCurrentUser
    ExecShell "" "$INSTDIR\${PRODUCT_EXECUTABLE}"
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

    # Check if already installed
    ReadRegStr $R0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "UninstallString"
    StrCmp $R0 "" notInstalled
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
    ${ElseIf} ${IsNativeAMD64}
        DetailPrint "Detected AMD64 Architecture"
        File "/oname=${PRODUCT_EXECUTABLE}" "${ARG_WAILS_AMD64_BINARY}"
    ${Else}
        MessageBox MB_OK|MB_ICONSTOP "Unsupported architecture."
        Abort
    ${EndIf}

    # Install other assets if any (e.g., from wails.json assets or specific files)
    # File "..\..\frontend\dist\..." # Frontend is embedded in binary

    # Enable Windows Long Path Support (required for npm cache and AI tools)
    DetailPrint "Enabling Windows Long Path Support..."
    WriteRegDWORD HKLM "SYSTEM\CurrentControlSet\Control\FileSystem" "LongPathsEnabled" 1

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

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}"
    SetRegView 64
    DeleteRegValue HKLM "Software\Microsoft\Windows\CurrentVersion\Run" "${AUTOSTART_REG_NAME}"

    # Ask user if they want to delete user data
    MessageBox MB_YESNO|MB_ICONQUESTION "$(DeleteUserData)" IDYES deleteUserData IDNO skipUserData
    
    deleteUserData:
    # Delete user data directories using cmd /c rd for faster deletion
    # RMDir /r is very slow for large directories like node_modules
    # Using rd /s /q is much faster on Windows
    DetailPrint "Deleting user data directories..."
    nsExec::ExecToLog 'cmd /c rd /s /q "$PROFILE\.cceasy"'

    # Preserve memory file (memories.json) before deleting .maclaw
    IfFileExists "$PROFILE\.maclaw\memories.json" 0 noMemoryBackup
        DetailPrint "Preserving memory file..."
        CopyFiles /SILENT "$PROFILE\.maclaw\memories.json" "$TEMP\maclaw_memories_backup.json"
    noMemoryBackup:

    nsExec::ExecToLog 'cmd /c rd /s /q "$PROFILE\.maclaw"'

    # Restore memory file after cleanup
    IfFileExists "$TEMP\maclaw_memories_backup.json" 0 noMemoryRestore
        CreateDirectory "$PROFILE\.maclaw"
        CopyFiles /SILENT "$TEMP\maclaw_memories_backup.json" "$PROFILE\.maclaw\memories.json"
        Delete "$TEMP\maclaw_memories_backup.json"
    noMemoryRestore:
    
    skipUserData:
SectionEnd

