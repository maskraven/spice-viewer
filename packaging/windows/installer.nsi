; NSIS installer for remote-viewer (Windows amd64 product build).
; Build: makensis /DVERSION=0.2.0 /DSOURCE_DIR=dist\windows packaging\windows\installer.nsi
; Requires: remote-viewer.exe and icon.ico available relative to SOURCE_DIR / this script.

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef SOURCE_DIR
  !define SOURCE_DIR "."
!endif

Name "Remote Viewer ${VERSION}"
OutFile "remote-viewer-setup-${VERSION}-amd64.exe"
InstallDir "$PROGRAMFILES64\remote-viewer"
RequestExecutionLevel admin
Unicode true
SetCompressor /SOLID lzma

!include "MUI2.nsh"

!define MUI_ABORTWARNING
!define PRODUCT_NAME "Remote Viewer"
!define PROGID "remote-viewer.vv"
!define MIME "application/x-virt-viewer"

!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  SetOutPath "$INSTDIR"
  File "${SOURCE_DIR}\remote-viewer.exe"
  File /nonfatal "${SOURCE_DIR}\LICENSE"
  File /nonfatal "${SOURCE_DIR}\README.md"

  ; Start Menu
  CreateDirectory "$SMPROGRAMS\${PRODUCT_NAME}"
  CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\Remote Viewer.lnk" "$INSTDIR\remote-viewer.exe"
  CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\Uninstall.lnk" "$INSTDIR\Uninstall.exe"

  ; .vv file association (same MIME as Linux packaging)
  WriteRegStr HKLM "Software\Classes\.vv" "" "${PROGID}"
  WriteRegStr HKLM "Software\Classes\.vv" "Content Type" "${MIME}"
  WriteRegStr HKLM "Software\Classes\${PROGID}" "" "virt-viewer connection file"
  WriteRegStr HKLM "Software\Classes\${PROGID}\DefaultIcon" "" "$INSTDIR\remote-viewer.exe,0"
  WriteRegStr HKLM "Software\Classes\${PROGID}\shell\open\command" "" '"$INSTDIR\remote-viewer.exe" "%1"'

  ; Uninstaller
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\remote-viewer" "DisplayName" "Remote Viewer ${VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\remote-viewer" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\remote-viewer" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\remote-viewer" "Publisher" "virt-viewer authors"
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\remote-viewer" "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\remote-viewer" "NoRepair" 1

  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, i 0, i 0)'
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\remote-viewer.exe"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${PRODUCT_NAME}\Remote Viewer.lnk"
  Delete "$SMPROGRAMS\${PRODUCT_NAME}\Uninstall.lnk"
  RMDir "$SMPROGRAMS\${PRODUCT_NAME}"

  ; Only remove association if it still points at us
  ReadRegStr $0 HKLM "Software\Classes\.vv" ""
  StrCmp $0 "${PROGID}" 0 skip_assoc
  DeleteRegKey HKLM "Software\Classes\.vv"
  DeleteRegKey HKLM "Software\Classes\${PROGID}"
  skip_assoc:

  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\remote-viewer"
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, i 0, i 0)'
SectionEnd
