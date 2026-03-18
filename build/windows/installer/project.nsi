Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
## !define AUTOSTART_REG_NAME  "RunEntryName"         # Default "${INFO_PROJECTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Include the wails tools
####
!include "wails_tools.nsh"

!ifndef AUTOSTART_REG_NAME
!define AUTOSTART_REG_NAME "${INFO_PROJECTNAME}"
!endif

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}"
VIFileVersion    "${INFO_PRODUCTVERSION}"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI2.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

# Launch app checkbox on finish page (checked by default)
# Use ShellExec to avoid launching app with admin privileges
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "$(LaunchAfterInstall)"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchAsCurrentUser

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

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

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\dist\${INFO_PROJECTNAME}-Setup.exe" # Name of the installer's file.
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}" # Default installing folder ($PROGRAMFILES is Program Files folder).
ShowInstDetails show # This will always show the installation details.

# Launch app as current user (not elevated admin)
Function LaunchAsCurrentUser
   ExecShell "" "$INSTDIR\${PRODUCT_EXECUTABLE}"
FunctionEnd

Function .onInit
   # Auto-detect system language (no dialog)
   System::Call 'kernel32::GetUserDefaultUILanguage() i .r0'
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

   !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    # Start automatically after Windows sign-in
    SetRegView 64
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Run" "${AUTOSTART_REG_NAME}" "$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\" autostart"

    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    SetRegView 64
    DeleteRegValue HKLM "Software\Microsoft\Windows\CurrentVersion\Run" "${AUTOSTART_REG_NAME}"

    !insertmacro wails.deleteUninstaller
SectionEnd
