Unicode true

!include /NONFATAL "datasrv_build_params.nsh.tmp"

!ifndef INFO_COMPANYNAME
!define INFO_COMPANYNAME "RapidAI"
!endif
!ifndef INFO_PRODUCTNAME
!define INFO_PRODUCTNAME "MaClaw Data Service"
!endif
!ifndef INFO_PRODUCTVERSION
!define INFO_PRODUCTVERSION "6.0.0.0"
!endif
!ifndef INFO_COPYRIGHT
!define INFO_COPYRIGHT "Copyright (C) 2026 RapidAI"
!endif
!ifndef PRODUCT_EXECUTABLE
!define PRODUCT_EXECUTABLE "maclaw-data-srv.exe"
!endif
!ifndef SERVICE_NAME
!define SERVICE_NAME "MaClawDataSrv"
!endif
!ifndef ARG_DATASRV_AMD64_BINARY
!define ARG_DATASRV_AMD64_BINARY "..\..\..\dist\maclaw-data-srv_amd64.exe"
!endif
!ifndef ARG_DATASRV_ARM64_BINARY
!define ARG_DATASRV_ARM64_BINARY "..\..\..\dist\maclaw-data-srv_arm64.exe"
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

# Open the Web Console login screen after a successful interactive install.
!define DATASRV_ADMIN_CONSOLE_URL "http://127.0.0.1:18180/ui"
!define DATASRV_READY_URL "http://127.0.0.1:18180/readyz"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

LangString AlreadyInstalled ${LANG_ENGLISH} "${INFO_PRODUCTNAME} is already installed. Do you want to uninstall it first?"
LangString AlreadyInstalled ${LANG_SIMPCHINESE} "${INFO_PRODUCTNAME} is already installed. Uninstall it first?"
LangString UpgradingExisting ${LANG_ENGLISH} "Existing ${INFO_PRODUCTNAME} installation detected. Stopping service and replacing old program files..."
LangString UpgradingExisting ${LANG_SIMPCHINESE} "Detected old ${INFO_PRODUCTNAME}. Stopping service and replacing old program files..."

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\dist\maclaw-data-srv-Setup.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\maclawdatasrv"
ShowInstDetails show
RequestExecutionLevel admin

Function OpenAdminConsole
    IfSilent 0 +2
        Return
    nsExec::ExecToLog `"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -Command "for($$i=0; $$i -lt 20; $$i++){ try { $$r=Invoke-WebRequest -UseBasicParsing -TimeoutSec 1 '${DATASRV_READY_URL}'; if($$r.StatusCode -ge 200 -and $$r.StatusCode -lt 500){ exit 0 } } catch {}; Start-Sleep -Milliseconds 500 }; exit 0"`
    ExecShell "" "${DATASRV_ADMIN_CONSOLE_URL}"
FunctionEnd

Function .onInit
    System::Call 'kernel32::GetUserDefaultUILanguage() i .r0'
    StrCmp $0 "2052" lang_zh_cn
    Goto lang_en

    lang_zh_cn:
        StrCpy $LANGUAGE ${LANG_SIMPCHINESE}
        Goto lang_done
    lang_en:
        StrCpy $LANGUAGE ${LANG_ENGLISH}
    lang_done:

    ReadRegStr $R0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "UninstallString"
    StrCmp $R0 "" notInstalled
    ReadRegStr $R1 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "InstallLocation"
    StrCmp $R1 "" 0 setExistingInstallDir
    Goto existingInstallDirReady
    setExistingInstallDir:
        StrCpy $INSTDIR $R1
    existingInstallDirReady:
    ReadRegStr $R1 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "QuietUninstallString"
    StrCmp $R1 "" 0 haveQuietUninstall
        StrCpy $R1 "$R0 /S"
    haveQuietUninstall:
    DetailPrint "$(UpgradingExisting)"
    ExecWait 'sc.exe stop "${SERVICE_NAME}"'
    Sleep 3000
    ExecWait 'sc.exe delete "${SERVICE_NAME}"'
    Sleep 1000
    ExecWait '$R1 _?=$INSTDIR'
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\README.md"
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"
    DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}"

    notInstalled:
FunctionEnd

Section
    SetShellVarContext all
    SetOutPath $INSTDIR

    ${If} ${IsNativeARM64}
        DetailPrint "Detected ARM64 Architecture"
        File "/oname=${PRODUCT_EXECUTABLE}" "${ARG_DATASRV_ARM64_BINARY}"
    ${ElseIf} ${IsNativeAMD64}
        DetailPrint "Detected AMD64 Architecture"
        File "/oname=${PRODUCT_EXECUTABLE}" "${ARG_DATASRV_AMD64_BINARY}"
    ${Else}
        MessageBox MB_OK|MB_ICONSTOP "Unsupported architecture."
        Abort
    ${EndIf}

    File "/oname=README.md" "..\..\..\datasrv\README.md"

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    CreateDirectory "$SMPROGRAMS\${INFO_COMPANYNAME}"
    CreateShortcut "$SMPROGRAMS\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    WriteUninstaller "$INSTDIR\uninstall.exe"

    DetailPrint "Registering ${SERVICE_NAME} Windows service"
    ExecWait 'sc.exe stop "${SERVICE_NAME}"'
    Sleep 2000
    ExecWait 'sc.exe delete "${SERVICE_NAME}"'
    Sleep 1000
    ExecWait 'sc.exe create "${SERVICE_NAME}" binPath= "$INSTDIR\${PRODUCT_EXECUTABLE}" start= auto DisplayName= "${INFO_PRODUCTNAME}"' $R1
    ${If} $R1 != 0
        MessageBox MB_OK|MB_ICONSTOP "Failed to create ${SERVICE_NAME} Windows service. sc.exe exited with code $R1."
        Abort
    ${EndIf}
    ExecWait 'sc.exe description "${SERVICE_NAME}" "MaClawDataSrv structured data HTTP service"'
    ExecWait 'sc.exe start "${SERVICE_NAME}"' $R1
    ${If} $R1 != 0
        ExecWait 'sc.exe stop "${SERVICE_NAME}"'
        ExecWait 'sc.exe delete "${SERVICE_NAME}"'
        MessageBox MB_OK|MB_ICONSTOP "Installed ${SERVICE_NAME}, but failed to start it. sc.exe exited with code $R1."
        Abort
    ${EndIf}

    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}" "DisplayVersion" "${INFO_PRODUCTVERSION}"

    Call OpenAdminConsole
SectionEnd

Section "uninstall"
    SetShellVarContext all

    ExecWait 'sc.exe stop "${SERVICE_NAME}"'
    ExecWait 'sc.exe delete "${SERVICE_NAME}"'

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\README.md"
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"

    Delete "$SMPROGRAMS\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}.lnk"
    RMDir "$SMPROGRAMS\${INFO_COMPANYNAME}"

    DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}"
SectionEnd
