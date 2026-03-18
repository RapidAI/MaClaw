Unicode true

!ifndef INFO_PROJECTNAME
!define INFO_PROJECTNAME "CodeClaw"
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
!define PRODUCT_EXECUTABLE "CodeClaw.exe"
!endif
!ifndef REQUEST_EXECUTION_LEVEL
    !define REQUEST_EXECUTION_LEVEL "admin"
!endif
!ifndef AUTOSTART_REG_NAME
!define AUTOSTART_REG_NAME "${INFO_PROJECTNAME}"
!endif

# Define Wails binaries (passed from command line or hardcoded here for manual build)
!ifndef ARG_WAILS_AMD64_BINARY
!define ARG_WAILS_AMD64_BINARY "..\..\..\dist\CodeClaw_amd64.exe"
!endif
!ifndef ARG_WAILS_ARM64_BINARY
!define ARG_WAILS_ARM64_BINARY "..\..\..\dist\CodeClaw_arm64.exe"
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

!include "MUI.nsh"
!include "x64.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

# Launch app checkbox on finish page (checked by default)
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "$(LaunchAfterInstall)"

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

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\dist\${INFO_PROJECTNAME}-Setup.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show

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
    MessageBox MB_YESNO|MB_ICONEXCLAMATION "${INFO_PRODUCTNAME} is already installed. Do you want to uninstall it first?" IDYES uninstall IDNO quit
    
    uninstall:
    ExecWait '"$R0" /S _?=$INSTDIR'
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"
    
    notInstalled:
    
    quit:
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
    MessageBox MB_YESNO|MB_ICONQUESTION "Do you want to delete user data (.cceasy and .maclaw folders)?$\n$\nThis will remove AI tools, configurations and cache.$\nNote: Your memory file (memories.json) will be preserved." IDYES deleteUserData IDNO skipUserData
    
    deleteUserData:
    # Delete user data directories using cmd /c rd for faster deletion
    # RMDir /r is very slow for large directories like node_modules
    # Using rd /s /q is much faster on Windows
    DetailPrint "Deleting user data directories..."
    nsExec::ExecToLog 'cmd /c rd /s /q "$PROFILE\.cceasy"'

    # Preserve memory file (memories.json) before deleting .maclaw
    IfFileExists "$PROFILE\.maclaw\memories.json" 0 +3
        DetailPrint "Preserving memory file..."
        CopyFiles /SILENT "$PROFILE\.maclaw\memories.json" "$TEMP\maclaw_memories_backup.json"

    nsExec::ExecToLog 'cmd /c rd /s /q "$PROFILE\.maclaw"'

    # Restore memory file after cleanup
    IfFileExists "$TEMP\maclaw_memories_backup.json" 0 +4
        CreateDirectory "$PROFILE\.maclaw"
        CopyFiles /SILENT "$TEMP\maclaw_memories_backup.json" "$PROFILE\.maclaw\memories.json"
        Delete "$TEMP\maclaw_memories_backup.json"
    
    skipUserData:
SectionEnd

