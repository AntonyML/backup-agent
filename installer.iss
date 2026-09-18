; Script de Inno Setup para Backup Agent Enterprise
; Generado y firmado para Antony Monge López — Costa Rica

#define MyAppName "Backup Agent Enterprise"
#ifndef MyAppVersion
  #define MyAppVersion "4.0.0"
#endif
#define MyAppPublisher "Antony Monge López"
#define MyAppURL "https://github.com/AntonyML/backup-agent"
#define MyAppExeName "backup-agent.exe"

[Setup]
AppId={{9F82C10D-5778-4D2F-A05A-9D725C2B4109}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\BackupAgent
DefaultGroupName=Backup Agent
AllowNoIcons=yes
LicenseFile=TERMS_AND_CONDITIONS.txt
OutputDir=dist
OutputBaseFilename=BackupAgent-Setup-v{#MyAppVersion}
SetupIconFile=public\icon\logo.ico
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\{#MyAppExeName}

[Languages]
Name: "spanish"; MessagesFile: "compiler:Languages\Spanish.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
; Binario principal del agente
Source: "bin\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
; Icono oficial de la aplicación
Source: "public\icon\logo.ico"; DestDir: "{app}"; Flags: ignoreversion
; Licencia y Términos y Condiciones
Source: "TERMS_AND_CONDITIONS.txt"; DestDir: "{app}"; Flags: ignoreversion
; Plantilla de configuración inicial (no sobrescribe si ya existe una personalizada)
Source: "bin\config.json"; DestDir: "{app}"; Flags: onlyifdoesntexist

[Icons]
; Acceso directo en el menú de inicio
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "tui"; IconFilename: "{app}\logo.ico"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
; Acceso directo opcional en el Escritorio para abrir directamente la TUI
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "tui"; IconFilename: "{app}\logo.ico"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "tui"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent
