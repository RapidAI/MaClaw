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

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\dist\${INFO_PROJECTNAME}-Setup.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show

Function .onInit
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
    MessageBox MB_YESNO|MB_ICONQUESTION "Do you want to delete all user data (.cceasy and .cc folders)?$\n$\nThis will remove all AI tools, configurations and cache." IDYES deleteUserData IDNO skipUserData
    
    deleteUserData:
    # Delete user data directories using cmd /c rd for faster deletion
    # RMDir /r is very slow for large directories like node_modules
    # Using rd /s /q is much faster on Windows
    DetailPrint "Deleting user data directories..."
    nsExec::ExecToLog 'cmd /c rd /s /q "$PROFILE\.cceasy"'
    # Also delete config file
    Delete "$PROFILE\.maclaw\config.json"
    
    skipUserData:
SectionEnd

