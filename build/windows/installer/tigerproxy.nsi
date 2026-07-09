Unicode true

!ifndef INFO_PROJECTNAME
!define INFO_PROJECTNAME "TigerProxy"
!endif
!ifndef INFO_COMPANYNAME
!define INFO_COMPANYNAME "QianXin"
!endif
!ifndef INFO_PRODUCTNAME
!define INFO_PRODUCTNAME "TigerProxy"
!endif
!ifndef INFO_PRODUCTVERSION
!define INFO_PRODUCTVERSION "0.1.0.1"
!endif
!ifndef INFO_COPYRIGHT
!define INFO_COPYRIGHT "Copyright (C) 2026 QianXin"
!endif
!ifndef PRODUCT_EXECUTABLE
!define PRODUCT_EXECUTABLE "TigerProxy.exe"
!endif

!ifndef ARG_WAILS_AMD64_BINARY
!define ARG_WAILS_AMD64_BINARY "..\..\..\dist\TigerProxy_amd64.exe"
!endif
!ifndef ARG_WAILS_ARM64_BINARY
!define ARG_WAILS_ARM64_BINARY "..\..\..\dist\TigerProxy_arm64.exe"
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
!define MUI_ICON_PATH "..\..\..\TigerProxy\assets\maclaw.ico"
!endif
!define MUI_ICON "${MUI_ICON_PATH}"
!define MUI_UNICON "${MUI_ICON_PATH}"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${INFO_PRODUCTNAME}"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchAsCurrentUser

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

LangString LaunchAfterInstall ${LANG_ENGLISH} "Launch ${INFO_PRODUCTNAME}"
LangString LaunchAfterInstall ${LANG_SIMPCHINESE} "启动 ${INFO_PRODUCTNAME}"

LangString AppIsRunning ${LANG_ENGLISH} "${INFO_PRODUCTNAME} is still running. Stop it and continue installing?"
LangString AppIsRunning ${LANG_SIMPCHINESE} "${INFO_PRODUCTNAME} 正在运行。是否停止并继续安装？"

LangString AppStillRunning ${LANG_ENGLISH} "${INFO_PRODUCTNAME} could not be stopped. Please close it and run the installer again."
LangString AppStillRunning ${LANG_SIMPCHINESE} "无法停止 ${INFO_PRODUCTNAME}。请手动关闭后重新运行安装程序。"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\dist\${INFO_PROJECTNAME}-Setup.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show
RequestExecutionLevel admin

Function LaunchAsCurrentUser
    ExecShell "" "$INSTDIR\${PRODUCT_EXECUTABLE}"
FunctionEnd

Function .onInit
    System::Call 'kernel32::GetUserDefaultUILanguage() i .r0'
    StrCmp $0 "2052" lang_zh_cn lang_en
    lang_zh_cn:
        StrCpy $LANGUAGE ${LANG_SIMPCHINESE}
        Goto lang_done
    lang_en:
        StrCpy $LANGUAGE ${LANG_ENGLISH}
    lang_done:

    # Stop running instance
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
    # Check if already installed
    ReadRegStr $R0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "UninstallString"
    StrCmp $R0 "" notInstalled
    ReadRegStr $R1 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "InstallLocation"
    StrCmp $R1 "" +2
        StrCpy $INSTDIR $R1
    ExecWait '"$R0" /S _?=$INSTDIR'
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"
    notInstalled:
FunctionEnd

Section
    SetShellVarContext all
    SetOutPath $INSTDIR

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

    # Create shortcuts
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    # Write uninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    # Registry for Add/Remove Programs
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "DisplayVersion" "${INFO_PRODUCTVERSION}"

    # Auto-start on login (--hidden flag starts minimized to tray)
    SetRegView 64
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Run" "${INFO_PROJECTNAME}" "$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\" --hidden"
SectionEnd

Section "uninstall"
    SetShellVarContext all

    ExecWait "taskkill /F /IM ${PRODUCT_EXECUTABLE}"
    Sleep 500

    RMDir /r "$APPDATA\${PRODUCT_EXECUTABLE}"

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\uninstall.exe"
    RMDir /r "$INSTDIR"

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}"
    SetRegView 64
    DeleteRegValue HKLM "Software\Microsoft\Windows\CurrentVersion\Run" "${INFO_PROJECTNAME}"
SectionEnd
