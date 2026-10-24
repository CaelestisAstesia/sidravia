#ifndef ProductVersion
  #error ProductVersion must be supplied by tools/build
#endif
#ifndef HasGUI
  #define HasGUI 0
#endif
#define IncludeGUI Int(HasGUI)

[Setup]
AppId={{EB2663BD-8DEB-446A-958E-43DF637C1F53}
AppName=Sidravia
AppVersion={#ProductVersion}
DefaultDirName={localappdata}\Programs\Sidravia
DefaultGroupName=Sidravia
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
SetupArchitecture=x64
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
CloseApplicationsFilter=*.exe,*.dll,*.json
UsePreviousAppDir=yes
UsePreviousSetupType=yes
UsePreviousTasks=yes
UninstallDisplayName=Sidravia
SetupIconFile=payload\app_icon.ico
UninstallDisplayIcon={app}\app_icon.ico
DisableProgramGroupPage=yes
DisableWelcomePage=no
DisableDirPage=no
DisableReadyPage=no
LicenseFile=payload\LICENSE

[Languages]
Name: "zhcn"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[Messages]
WelcomeLabel1=安装 Sidravia
WelcomeLabel2=Sidravia 是网络认证客户端，支持图形界面和后台运行。%n%n本向导将为当前 Windows 账户安装 Sidravia。安装过程中，你可以选择安装组件、开机自启和命令行设置。%n%n开始前，请先退出正在运行的 Sidravia。
WizardLicense=许可协议
LicenseLabel=请阅读以下许可协议，再继续安装 Sidravia。
LicenseLabel3=GNU Affero General Public License v3.0
LicenseAccepted=我接受此协议(&A)
LicenseNotAccepted=我不接受此协议(&D)
WizardSelectDir=安装位置
SelectDirDesc=选择 Sidravia 的安装文件夹。
SelectDirLabel3=默认安装到当前 Windows 账户的应用目录，仅供此账户使用。
SelectDirBrowseLabel=如需更改安装位置，请点击“浏览”。
WizardSelectComponents=安装组件
SelectComponentsDesc=选择要安装的 Sidravia 组件。
SelectComponentsLabel2=不安装图形界面时，需通过命令行配置和管理连接
WizardSelectTasks=安装选项
SelectTasksDesc=选择安装时需要启用的功能。
SelectTasksLabel2=勾选需要启用的选项，然后点击“下一步”。
WizardReady=准备安装
ReadyLabel1=确认以下设置，点击“安装”开始。
ReadyLabel2a=如需调整设置，请点击“上一步”。
ReadyLabel2b=确认以下设置，点击“安装”开始。
WizardInstalling=正在安装 Sidravia
InstallingLabel=请稍候，安装程序正在复制文件并应用所选设置。
StatusExtractFiles=正在复制文件…
FinishedHeadingLabel=Sidravia 安装完成
FinishedLabel=Sidravia 已安装到你的电脑。
FinishedLabelNoIcons=Sidravia 已安装到你的电脑。

[Types]
Name: "full"; Description: "完整安装"
Name: "custom"; Description: "自定义安装"; Flags: iscustom

[Components]
Name: "core"; Description: "核心与命令行工具（必需）"; Types: full custom; Flags: fixed
#if IncludeGUI
Name: "gui"; Description: "图形界面"; Types: full
#endif

[Tasks]
Name: "autostart"; Description: "开机自启"; GroupDescription: "启动："
Name: "userpath"; Description: "加入用户 PATH"; GroupDescription: "命令行："
#if IncludeGUI
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式："; Components: gui; Flags: unchecked
#endif

#include "files.iss"

[Icons]
#if IncludeGUI
Name: "{group}\Sidravia"; Filename: "{app}\Sidravia.exe"; Components: gui
Name: "{userdesktop}\Sidravia"; Filename: "{app}\Sidravia.exe"; Components: gui; Tasks: desktopicon
#endif
Name: "{group}\卸载 Sidravia"; Filename: "{uninstallexe}"

[Run]
#if IncludeGUI
Filename: "{app}\Sidravia.exe"; Description: "启动 Sidravia"; Components: gui; Flags: nowait postinstall skipifsilent; BeforeInstall: ReleaseInstaller
#endif

[Code]
type
  TPrivateAttributes = record
    Size: LongWord;
    Padding: LongWord;
    Descriptor: NativeUInt;
    InheritHandle: LongWord;
    Padding2: LongWord;
  end;
  IToastHistory = interface(IUnknown)
    '{5CADDC63-01D3-4C97-986F-0533483FEE14}'
    procedure GetIids;
    procedure GetRuntimeClassName;
    procedure GetTrustLevel;
    procedure RemoveGroup;
    procedure RemoveGroupWithId;
    procedure RemoveGroupedTagWithId;
    procedure RemoveGroupedTag;
    procedure Remove;
    procedure Clear;
    procedure ClearWithId(ApplicationId: NativeUInt); safecall;
  end;
  IToastStatics = interface(IUnknown)
    '{7AB93C52-0E48-4750-BA9D-1A4113981847}'
    procedure GetIids;
    procedure GetRuntimeClassName;
    procedure GetTrustLevel;
    function GetHistory: IToastHistory; safecall;
  end;

var
  StartupPage: TInputOptionWizardPage;
  IntegrationFailed: Boolean;
  StartupHint: TNewStaticText;
  SettingsDirectory: String;
  SettingsError: String;
  IntegrationDetail: String;
  IntegrationConfirmed: Boolean;
  ManagerMode: String;
  ManagerPath: Boolean;
  InstallerMutex: NativeUInt;
  UpdateOperation: String;
  UpdatePrepared, UpdateFilesStarted: Boolean;

{ Fixed Windows API bindings used by this installer, not a file executor. }
function FileAttributes(Name: String): LongWord;
  external 'GetFileAttributesW@kernel32.dll stdcall';
function NativeError: LongWord;
  external 'GetLastError@kernel32.dll stdcall';
function CurrentPID: LongWord;
  external 'GetCurrentProcessId@kernel32.dll stdcall';
function CopyProtected(Source, Destination: String; FailIfExists: Boolean): Boolean;
  external 'CopyFileW@kernel32.dll stdcall';
function NewGUID(var Value: TGUID): Integer;
  external 'CoCreateGuid@ole32.dll stdcall';
function GUIDText(const Value: TGUID; Buffer: String; Size: Integer): Integer;
  external 'StringFromGUID2@ole32.dll stdcall';
function RoInitialize(Mode: LongWord): Integer;
  external 'RoInitialize@combase.dll stdcall';
procedure RoUninitialize;
  external 'RoUninitialize@combase.dll stdcall';
function NewHString(Value: String; Len: LongWord; var Handle: NativeUInt): Integer;
  external 'WindowsCreateString@combase.dll stdcall';
function FreeHString(Handle: NativeUInt): Integer;
  external 'WindowsDeleteString@combase.dll stdcall';
function RoFactory(Name: NativeUInt; const IID: TGUID; var Factory: IUnknown): Integer;
  external 'RoGetActivationFactory@combase.dll stdcall';

function FirstEntry(Pattern: String; var Find: TFindRec): Boolean;
var Error: LongWord;
begin
  Result := FindFirst(Pattern, Find);
  if not Result then begin
    Error := NativeError;
    if (Error <> 2) and (Error <> 3) and (Error <> 18) then
      RaiseException('无法枚举清理位置，停止操作。');
  end;
end;

function NextEntry(var Find: TFindRec): Boolean;
begin
  Result := FindNext(Find);
  if not Result and (NativeError <> 18) then
    RaiseException('枚举未完成，停止操作。');
end;

function FileSecurity(Name: String; Kind, Information: LongWord;
  var Owner, Group, DACL, SACL, Descriptor: NativeUInt): LongWord;
  external 'GetNamedSecurityInfoW@advapi32.dll stdcall';
function ProtectDescriptor(Descriptor: NativeUInt; Bits, SetBits: Word): Boolean;
  external 'SetSecurityDescriptorControl@advapi32.dll stdcall';
function CreatePrivateFile(Name: String; Access, Sharing: LongWord;
  var Attributes: TPrivateAttributes; Disposition, Flags: LongWord;
  Template: NativeUInt): NativeUInt;
  external 'CreateFileW@kernel32.dll stdcall';
function CloseNativeHandle(Handle: NativeUInt): Boolean;
  external 'CloseHandle@kernel32.dll stdcall';
function FreeNativeMemory(Address: NativeUInt): NativeUInt;
  external 'LocalFree@kernel32.dll stdcall';

function PreserveProtectedFile(Source, Destination: String): Boolean;
var
  Owner, Group, DACL, SACL, Descriptor, Handle: NativeUInt;
  Attributes: TPrivateAttributes;
begin
  Result := False;
  Descriptor := 0;
  Handle := 0;
  { CopyFile does NOT promise to copy the DACL. Create the destination with the
    source DACL protected against extra inheritance before copying any bytes. }
  if FileSecurity(Source, 1, 4, Owner, Group, DACL, SACL, Descriptor) <> 0 then Exit;
  try
    if not ProtectDescriptor(Descriptor, $1000, $1000) then Exit;
    Attributes.Size := 24; { pinned x64 Setup }
    Attributes.Padding := 0;
    Attributes.Descriptor := Descriptor;
    Attributes.InheritHandle := 0;
    Attributes.Padding2 := 0;
    Handle := CreatePrivateFile(Destination, $40000000, 0, Attributes, 1, $80, 0);
    if Handle = NativeUInt(-1) then Exit;
    if not CloseNativeHandle(Handle) then Exit;
    Handle := 0;
    if not CopyProtected(Source, Destination, False) then Exit;
    Result := True;
  finally
    if (Handle <> 0) and (Handle <> NativeUInt(-1)) then CloseNativeHandle(Handle);
    if Descriptor <> 0 then FreeNativeMemory(Descriptor);
  end;
end;

function PathPresent(Name: String): Boolean;
begin
  Result := FileAttributes(Name) <> $FFFFFFFF;
  if not Result then
    if (NativeError <> 2) and (NativeError <> 3) then
      RaiseException('无法检查路径，停止操作。');
end;

function SafePath(Name: String): Boolean;
var
  Attr: LongWord;
  Parent: String;
begin
  Result := False;
  if (Name = '') or (ExpandFileName(Name) <> Name) then Exit;
  repeat
    Attr := FileAttributes(Name);
    if Attr = $FFFFFFFF then begin
      if (NativeError <> 2) and (NativeError <> 3) then Exit;
    end else if (Attr and $400) <> 0 then Exit;
    Parent := ExtractFileDir(Name);
    if Parent = Name then Break;
    Name := Parent;
  until Name = '';
  Result := True;
end;

{ Read only the bounded flat JSON object returned by the bundled MGR. Arrays
  are only strings (capabilities). Strings are scanned, never searched for keys
  inside directory text. No script host, secondary wire format or parser DLL. }
procedure JsonSpace(S: String; var P: Integer);
begin
  while P <= Length(S) do begin
    if Pos(Copy(S, P, 1), ' ' + #9 + #13 + #10) = 0 then Break;
    P := P + 1;
  end;
end;

function JsonString(S: String; var P: Integer; var Value: String): Boolean;
var
  C: String;
  I: Integer;
begin
  Result := False;
  Value := '';
  if Copy(S, P, 1) <> '"' then Exit;
  P := P + 1;
  while P <= Length(S) do begin
    C := Copy(S, P, 1);
    P := P + 1;
    if C = '"' then begin Result := True; Exit; end;
    if Ord(C[1]) < 32 then Exit;
    if C = '\' then begin
      C := Copy(S, P, 1);
      P := P + 1;
      if C = 'u' then begin
        for I := 0 to 3 do
          if Pos(Lowercase(Copy(S, P + I, 1)), '0123456789abcdef') = 0 then Exit;
        if P + 3 > Length(S) then Exit;
        P := P + 4;
        C := '?'; { only ignored directory text may use Unicode escapes }
      end else if Pos(C, '"\/bfnrt') = 0 then Exit;
    end;
    Value := Value + C;
  end;
end;

function JsonField(Fields: TStringList; Key: String): String;
var I: Integer;
begin
  Result := #1;
  for I := 0 to Fields.Count - 1 do
    if Copy(Fields[I], 1, Length(Key) + 1) = Key + '=' then begin
      Result := Copy(Fields[I], Length(Key) + 2, MaxInt); Exit;
    end;
end;

function ParseManagerJSON(S: String): Boolean;
var
  P, Start: Integer;
  Key, Value, Ignored: String;
  Fields: TStringList;
begin
  Result := False;
  if Length(S) > 32768 then Exit;
  Fields := TStringList.Create;
  try
    P := 1;
    JsonSpace(S, P);
    if Copy(S, P, 1) <> '{' then Exit;
    P := P + 1;
    repeat
      JsonSpace(S, P);
      if not JsonString(S, P, Key) then Exit;
      if JsonField(Fields, Key) <> #1 then Exit;
      JsonSpace(S, P);
      if Copy(S, P, 1) <> ':' then Exit;
      P := P + 1;
      JsonSpace(S, P);
      if Copy(S, P, 1) = '"' then begin
        if (Key = 'version') or (Key = 'ok') or (Key = 'pathEnabled') or
          (Key = 'changed') or (Key = 'guiAvailable') then Exit;
        if not JsonString(S, P, Value) then Exit;
      end else if Copy(S, P, 1) = '[' then begin
        P := P + 1;
        JsonSpace(S, P);
        if Copy(S, P, 1) <> ']' then repeat
          if not JsonString(S, P, Ignored) then Exit;
          JsonSpace(S, P);
          if Copy(S, P, 1) <> ',' then Break;
          P := P + 1;
          JsonSpace(S, P);
        until False;
        if Copy(S, P, 1) <> ']' then Exit;
        P := P + 1;
        Value := 'array';
      end else begin
        Start := P;
        while P <= Length(S) do begin
          if Pos(Copy(S, P, 1), ',}' + ' ' + #9 + #13 + #10) > 0 then Break;
          P := P + 1;
        end;
        Value := Copy(S, Start, P - Start);
        if (Value <> 'true') and (Value <> 'false') and (Value <> '1') then Exit;
      end;
      Fields.Add(Key + '=' + Value);
      JsonSpace(S, P);
      if Copy(S, P, 1) <> ',' then Break;
      P := P + 1;
    until False;
    if Copy(S, P, 1) <> '}' then Exit;
    P := P + 1;
    JsonSpace(S, P);
    if P <= Length(S) then Exit;
    if (JsonField(Fields, 'version') <> '1') or (JsonField(Fields, 'ok') <> 'true') or
      (JsonField(Fields, 'code') <> 'ok') then Exit;
    ManagerMode := JsonField(Fields, 'autostart');
    if (ManagerMode <> 'disabled') and (ManagerMode <> 'gui') and (ManagerMode <> 'daemon') then Exit;
    Value := JsonField(Fields, 'pathEnabled');
    if (Value <> 'true') and (Value <> 'false') then Exit;
    ManagerPath := Value = 'true';
    Result := True;
  finally
    Fields.Free;
  end;
end;

function QueryManager(Temporary: Boolean): Boolean;
var
  ExitCode: Integer;
  Output: TExecOutput;
  Tool, Arguments: String;
begin
  Result := False;
  try
    Tool := ExpandConstant('{app}\sidraviamgr.exe');
    Arguments := 'status';
    if Temporary then begin
      ExtractTemporaryFile('sidraviamgr.exe');
      Tool := ExpandConstant('{tmp}\sidraviamgr.exe');
      Arguments := 'status --directory ' + AddQuotes(ExpandConstant('{app}') + '\.');
    end;
    if not SafePath(Tool) then Exit;
    if not ExecAndCaptureOutput(Tool, Arguments, ExtractFileDir(Tool), SW_HIDE,
      ewWaitUntilTerminated, ExitCode, Output) then Exit;
    if (ExitCode <> 0) or Output.Error or (GetArrayLength(Output.StdOut) <> 1) then Exit;
    Result := ParseManagerJSON(Output.StdOut[0]);
  except
    Result := False;
  end;
end;

function NewInstallerMutex(Attributes: NativeUInt; Owner: Boolean; Name: String): NativeUInt;
  external 'CreateMutexW@kernel32.dll stdcall';
function WaitNative(Handle: NativeUInt; Timeout: LongWord): LongWord;
  external 'WaitForSingleObject@kernel32.dll stdcall';
function ReleaseNativeMutex(Handle: NativeUInt): Boolean;
  external 'ReleaseMutex@kernel32.dll stdcall';

function HoldInstaller: Boolean;
var WMI, Processes, Process, SID: Variant;
    Status: LongWord;
begin
  Result := False;
  if InstallerMutex <> 0 then Exit;
  try
    WMI := CreateOleObject('WbemScripting.SWbemLocator');
    WMI := WMI.ConnectServer('.', 'root\CIMV2');
    Processes := WMI.ExecQuery('SELECT * FROM Win32_Process WHERE ProcessId=' + IntToStr(CurrentPID));
    if Processes.Count <> 1 then Exit;
    Process := Processes.ItemIndex(0);
    if Process.GetOwnerSid(SID) <> 0 then Exit;
    InstallerMutex := NewInstallerMutex(0, False, 'Global\SidraviaInstall-' + SID);
    if InstallerMutex = 0 then Exit;
    Status := WaitNative(InstallerMutex, 0);
    Result := (Status = 0) or (Status = $80);
    if not Result then begin CloseNativeHandle(InstallerMutex); InstallerMutex := 0; end;
  except Result := False; end;
end;

procedure ReleaseInstaller;
begin
  if InstallerMutex <> 0 then begin
    ReleaseNativeMutex(InstallerMutex);
    CloseNativeHandle(InstallerMutex);
    InstallerMutex := 0;
  end;
end;

function InitializeSetup: Boolean;
begin
  Result := HoldInstaller;
  if not Result then MsgBox('另一个安装/卸载操作正在进行，或无法核实当前账户。请稍后重试。', mbError, MB_OK);
end;

procedure AbortApplicationUpdate; forward;

procedure DeinitializeSetup;
begin
  ReleaseInstaller;
  if UpdatePrepared and not UpdateFilesStarted then AbortApplicationUpdate;
end;

procedure DeinitializeUninstall;
begin
  ReleaseInstaller;
end;

function ProductExited(AllCopies: Boolean): Boolean;
var
  WMI, Processes, Process: Variant;
  SID, Owner: Variant;
  I: Integer;
  Path: String;
begin
  Result := False;
  try
    WMI := CreateOleObject('WbemScripting.SWbemLocator');
    WMI := WMI.ConnectServer('.', 'root\CIMV2');
    Processes := WMI.ExecQuery('SELECT * FROM Win32_Process WHERE ProcessId=' + IntToStr(CurrentPID));
    if Processes.Count <> 1 then Exit;
    Process := Processes.ItemIndex(0);
    if Process.GetOwnerSid(SID) <> 0 then Exit;
    Processes := WMI.ExecQuery('SELECT * FROM Win32_Process WHERE Name="Sidravia.exe" OR Name="sidraviad.exe" OR Name="sidraviactl.exe" OR Name="sidraviaupdater.exe"');
    for I := 0 to Processes.Count - 1 do begin
      Process := Processes.ItemIndex(I);
      if Process.GetOwnerSid(Owner) <> 0 then Exit;
      if Owner = SID then begin
        if AllCopies then Exit;
        if VarIsNull(Process.ExecutablePath) then Exit;
        Path := ExtractFileDir(Process.ExecutablePath);
        if CompareText(Path, ExpandConstant('{app}')) = 0 then Exit;
      end;
    end;
    Result := True;
  except
    Result := False;
  end;
end;

function ValidNamespace(Name: String): Boolean;
var
  I: Integer;
begin
  Result := False;
  if (Length(Name) < 1) or (Length(Name) > 64) then Exit;
  if (Name[1] = '.') or (Name[Length(Name)] = '.') or (Pos('..', Name) > 0) then Exit;
  if (Lowercase(Name) = 'production') or (Lowercase(Name) = 'default') or (Lowercase(Name) = 'prod') then Exit;
  for I := 1 to Length(Name) do
    if Pos(Lowercase(Name[I]), 'abcdefghijklmnopqrstuvwxyz0123456789._-') = 0 then Exit;
  Result := True;
end;

function HexToken(Value: String): Boolean;
var
  I: Integer;
begin
  Result := False;
  if Length(Value) <> 32 then Exit;
  for I := 1 to 32 do
    if Pos(Value[I], '0123456789abcdef') = 0 then Exit;
  Result := True;
end;

function MigrationName(Name: String): Boolean;
var
  Prefix: String;
begin
  Prefix := '.sidravia-migration-';
  if Pos('.sidravia-gui-migration-', Name) = 1 then Prefix := '.sidravia-gui-migration-';
  Result := (Pos(Prefix, Name) = 1) and
    (Copy(Name, Length(Name) - 4, 5) = '.json') and
    HexToken(Copy(Name, Length(Prefix) + 1, Length(Name) - Length(Prefix) - 5));
end;

function LogName(Name: String): Boolean;
var
  I: Integer;
begin
  Result := (Name = 'sidraviad.log') or (Name = 'sidraviad.log.1');
  if Result then Exit;
  if (Length(Name) <> 31) or (Copy(Name, 1, 10) <> 'sidraviad-') or
    (Copy(Name, 28, 4) <> '.log') then Exit;
  for I := 11 to 27 do begin
    if (I = 15) or (I = 18) or (I = 21) then begin
      if Name[I] <> '-' then Exit;
    end else if Pos(Name[I], '0123456789') = 0 then Exit;
  end;
  Result := True;
end;

function DataTempName(Name: String; WithGUI: Boolean): Boolean;
var Rest, Part: String;
    P: Integer;
begin
  Result := (Copy(Name, 1, 10) = '.sidravia-') and
    (Copy(Name, Length(Name) - 3, 4) = '.tmp') and
    HexToken(Copy(Name, 11, Length(Name) - 14));
  if Result then Exit;
  if not WithGUI or (Copy(Name, 1, 18) <> 'gui-settings.json.') then Exit;
  Rest := Copy(Name, 19, MaxInt);
  P := Pos('.', Rest);
  if P < 2 then Exit;
  Part := Copy(Rest, 1, P - 1);
  if (Length(Part) > 10) or (Part[1] = '0') then Exit;
  if (Length(Part) = 10) and (CompareStr(Part, '4294967295') > 0) then Exit;
  for P := 1 to Length(Part) do
    if Pos(Part[P], '0123456789') = 0 then Exit;
  Delete(Rest, 1, Length(Part) + 1);
  if Copy(Rest, Length(Rest) - 3, 4) <> '.tmp' then Exit;
  Part := Copy(Rest, 1, Length(Rest) - 4);
  if (Length(Part) < 1) or (Length(Part) > 10) then Exit;
  if (Length(Part) > 1) and (Part[1] = '0') then Exit;
  for P := 1 to Length(Part) do
    if Pos(Part[P], '0123456789') = 0 then Exit;
  if (Length(Part) = 10) and (CompareStr(Part, '4294967295') > 0) then Exit;
  Result := True;
end;

procedure AddIfPresent(Files: TStringList; Name: String);
begin
  if PathPresent(Name) then begin
    if Files.IndexOf(Name) < 0 then Files.Add(Name);
  end;
end;

procedure AddTemporaryFiles(Files: TStringList; Root: String; WithGUI: Boolean);
var Find: TFindRec;
begin
  if not SafePath(Root) then RaiseException('临时数据位置异常。');
  if FirstEntry(Root + '\*', Find) then try
    repeat
      if DataTempName(Find.Name, WithGUI) then AddIfPresent(Files, Root + '\' + Find.Name);
    until not NextEntry(Find);
  finally FindClose(Find); end;
end;

procedure AddDataFiles(Files, Directories: TStringList; Config, Cache: String);
var
  Find: TFindRec;
  Name: String;
begin
  if not SafePath(Config) or not SafePath(Cache) or not SafePath(Cache + '\logs') then
    RaiseException('数据位置包含链接或无法核实。');
  AddTemporaryFiles(Files, Config, True);
  if CompareText(Config, Cache) <> 0 then AddTemporaryFiles(Files, Cache, False);
  AddTemporaryFiles(Files, Cache + '\logs', False);
  AddIfPresent(Files, Config + '\configurations.json');
  AddIfPresent(Files, Config + '\gui-settings.json');
  AddIfPresent(Files, Cache + '\runtime.json');
  if FirstEntry(Config + '\*', Find) then try
    repeat
      Name := Find.Name;
      if MigrationName(Name) or ((Pos('.sidravia-upgrade-', Name) = 1) and
        (Copy(Name, Length(Name) - 3, 4) = '.txt') and
        HexToken(Copy(Name, 19, Length(Name) - 22))) then AddIfPresent(Files, Config + '\' + Name);
    until not NextEntry(Find);
  finally FindClose(Find); end;
  if FirstEntry(Cache + '\logs\*', Find) then try
    repeat
      if LogName(Find.Name) then AddIfPresent(Files, Cache + '\logs\' + Find.Name);
    until not NextEntry(Find);
  finally FindClose(Find); end;
  Directories.Add(Cache + '\logs');
  Directories.Add(Cache);
  if CompareText(Config, Cache) <> 0 then Directories.Add(Config);
end;

function SafeTree(Name: String; Depth: Integer; var Count: Integer): Boolean;
var
  Find: TFindRec;
  Attr: LongWord;
begin
  Result := False;
  Count := Count + 1;
  if (Count > 100000) or (Depth > 64) or not SafePath(Name) then Exit;
  Attr := FileAttributes(Name);
  if Attr = $FFFFFFFF then begin Result := not PathPresent(Name); Exit; end;
  if (Attr and $10) = 0 then Exit;
  if FirstEntry(Name + '\*', Find) then try
    repeat
      if (Find.Name <> '.') and (Find.Name <> '..') then begin
        if (Find.Attributes and $400) <> 0 then Exit;
        if (Find.Attributes and $10) <> 0 then begin
          if not SafeTree(Name + '\' + Find.Name, Depth + 1, Count) then Exit;
        end else begin
          Count := Count + 1;
          if (Count > 100000) or not SafePath(Name + '\' + Find.Name) then Exit;
        end;
      end;
    until not NextEntry(Find);
    Result := True;
  finally FindClose(Find); end
  else Result := not DirExists(Name) or (NativeError = 2) or (NativeError = 18);
end;

function OwnedCacheRoot(Name: String): Boolean;
var Find: TFindRec;
begin
  Result := False;
  if not SafePath(Name) or not DirExists(Name) then Exit;
  if FirstEntry(Name + '\*', Find) then try
    repeat
      if (Find.Name <> '.') and (Find.Name <> '..') then begin
        { WebView2 owns EBWebView. Unknown siblings are not browser data. }
        if (Find.Name <> 'EBWebView') or ((Find.Attributes and $10) = 0) then Exit;
      end;
    until not NextEntry(Find);
  finally FindClose(Find); end;
  Result := True;
end;

function DeleteCacheTree(Name: String; Depth: Integer): Boolean;
var
  Find: TFindRec;
  Child: String;
begin
  Result := False;
  if (Depth > 64) or not SafePath(Name) then Exit;
  if not PathPresent(Name) then begin Result := True; Exit; end;
  if not DirExists(Name) then Exit;
  if FirstEntry(Name + '\*', Find) then try
    repeat
      if (Find.Name <> '.') and (Find.Name <> '..') then begin
        Child := Name + '\' + Find.Name;
        if not SafePath(Child) then Exit;
        if (Find.Attributes and $10) <> 0 then begin
          if not DeleteCacheTree(Child, Depth + 1) then Exit;
        end else if not DeleteFile(Child) then Exit;
      end;
    until not NextEntry(Find);
  finally FindClose(Find); end;
  Result := RemoveDir(Name);
end;

{ SDK ABI: IInspectable's three slots precede the notification methods. }
function ClearToastHistory(AUMID: String): Boolean;
var
  ClassName, AppName: NativeUInt;
  Factory: IUnknown;
  Statics: IToastStatics;
  History: IToastHistory;
  IID: TGUID;
  Initialized: Boolean;
begin
  Result := False;
  ClassName := 0;
  AppName := 0;
  Initialized := False;
  try
    OleCheck(RoInitialize(0));
    Initialized := True;
    OleCheck(NewHString('Windows.UI.Notifications.ToastNotificationManager',
      Length('Windows.UI.Notifications.ToastNotificationManager'), ClassName));
    OleCheck(NewHString(AUMID, Length(AUMID), AppName));
    IID := StringToGUID('{7AB93C52-0E48-4750-BA9D-1A4113981847}');
    OleCheck(RoFactory(ClassName, IID, Factory));
    Statics := IToastStatics(Factory);
    History := Statics.GetHistory;
    History.ClearWithId(AppName);
    Result := True;
  except
    Result := False;
  end;
  History := nil;
  Statics := nil;
  Factory := nil;
  if AppName <> 0 then FreeHString(AppName);
  if ClassName <> 0 then FreeHString(ClassName);
  if Initialized then RoUninitialize;
end;

function ToastKey(AUMID: String): String;
begin
  Result := 'Software\Classes\AppUserModelId\' + AUMID;
end;

function KnownToastValues(AUMID: String): Boolean;
var
  Key, Value: String;
begin
  Key := ToastKey(AUMID);
  Result := False;
  { The old GUI used random, unrecorded activator CLSIDs. A syntactically valid
    GUID is insufficient proof of ownership; preserve it and stop full cleanup. }
  if RegValueExists(HKCU, Key, 'CustomActivator') then Exit;
  if RegValueExists(HKCU, Key, 'DisplayName') then begin
    if not RegQueryStringValue(HKCU, Key, 'DisplayName', Value) then Exit;
    if Value <> 'Sidravia' then Exit;
  end;
  Result := True;
end;

function ClearExtraData(VerifyOnly: Boolean): Boolean;
var
  Files, Caches, Directories, Identities: TStringList;
  Find: TFindRec;
  Keys: TArrayOfString;
  R, L, Root, Name, AUMID, Marker: String;
  I, Count: Integer;
  Attr: LongWord;
  MarkerText: AnsiString;
begin
  Result := False;
  Files := TStringList.Create;
  Caches := TStringList.Create;
  Directories := TStringList.Create;
  Identities := TStringList.Create;
  try
    try
      R := ExpandConstant('{userappdata}\Sidravia');
      L := ExpandConstant('{localappdata}\Sidravia');
      Marker := R + '\.cleanup-incomplete';
      if not SafePath(Marker) or not SafePath(L) then Exit;
      if PathPresent(Marker) then begin
        if not LoadStringFromFile(Marker, MarkerText) or (MarkerText <> 'sidravia-extra-cleanup-v1') then Exit;
      end;
      AddDataFiles(Files, Directories, R, L);
      Root := R + '\namespaces';
      if not SafePath(Root) then Exit;
      if FirstEntry(Root + '\*', Find) then try
        repeat
          Name := Find.Name;
          if ValidNamespace(Name) then begin
            if (Find.Attributes and $10) = 0 then Exit;
            AddDataFiles(Files, Directories, Root + '\' + Name, Root + '\' + Name);
          end;
        until not NextEntry(Find);
      finally FindClose(Find); end;
      Root := L + '\announcement-webview';
      if not SafePath(Root) then Exit;
      if FirstEntry(Root + '\*', Find) then try
        repeat
          Name := Find.Name;
          if (Name = 'production') or ((Copy(Name, 1, 3) = 'ns-') and
            ValidNamespace(Copy(Name, 4, MaxInt)) and (Name = Lowercase(Name))) then
            Caches.Add(Root + '\' + Name);
        until not NextEntry(Find);
      finally FindClose(Find); end;
      AUMID := 'io.github.astesia.Sidravia.Announcements';
      Identities.Add(AUMID);
      if not RegGetSubkeyNames(HKCU, 'Software\Classes\AppUserModelId', Keys) then begin
        if RegKeyExists(HKCU, 'Software\Classes\AppUserModelId') then Exit;
      end;
      for I := 0 to GetArrayLength(Keys) - 1 do begin
        Name := Copy(Keys[I], Length(AUMID) + 5, MaxInt);
        if (Copy(Keys[I], 1, Length(AUMID) + 4) = AUMID + '.ns.') and
          (Name = Lowercase(Name)) and ValidNamespace(Name) then Identities.Add(Keys[I]);
      end;
      for I := 0 to Files.Count - 1 do begin
        if not SafePath(Files[I]) then Exit;
        Attr := FileAttributes(Files[I]);
        if (Attr = $FFFFFFFF) or ((Attr and $10) <> 0) then Exit;
      end;
      Count := 0;
      for I := 0 to Caches.Count - 1 do
        if not OwnedCacheRoot(Caches[I]) or not SafeTree(Caches[I], 0, Count) then Exit;
      for I := 0 to Identities.Count - 1 do
        if not KnownToastValues(Identities[I]) then Exit;
      if not ProductExited(True) then Exit;
      if VerifyOnly then begin
        Result := (Files.Count = 0) and (Caches.Count = 0);
        for I := 0 to Identities.Count - 1 do
          if RegValueExists(HKCU, ToastKey(Identities[I]), 'DisplayName') then Result := False;
        Exit;
      end;
      if not ForceDirectories(R) or not SafePath(R) then Exit;
      { A fixed non-sensitive incomplete marker, never an operation journal. }
      if not SaveStringToFile(Marker, 'sidravia-extra-cleanup-v1', False) then Exit;
      if not SafePath(Marker) then Exit;
      for I := 0 to Identities.Count - 1 do begin
        if not KnownToastValues(Identities[I]) then Exit;
        if not ClearToastHistory(Identities[I]) then Exit;
        if RegValueExists(HKCU, ToastKey(Identities[I]), 'DisplayName') then
          if not RegDeleteValue(HKCU, ToastKey(Identities[I]), 'DisplayName') then Exit;
        { Empty keys only; unknown values and subkeys survive. }
        if RegKeyExists(HKCU, ToastKey(Identities[I])) then
          RegDeleteKeyIfEmpty(HKCU, ToastKey(Identities[I]));
      end;
      for I := 0 to Files.Count - 1 do begin
        if not SafePath(Files[I]) then Exit;
        if PathPresent(Files[I]) then begin
          if DirExists(Files[I]) or not DeleteFile(Files[I]) then Exit;
        end;
      end;
      for I := 0 to Caches.Count - 1 do
        if not DeleteCacheTree(Caches[I], 0) then Exit;
      for I := 0 to Files.Count - 1 do
        if PathPresent(Files[I]) then Exit;
      for I := 0 to Caches.Count - 1 do
        if PathPresent(Caches[I]) then Exit;
      for I := 0 to Identities.Count - 1 do
        if not KnownToastValues(Identities[I]) or
          RegValueExists(HKCU, ToastKey(Identities[I]), 'DisplayName') then Exit;
      if not ClearExtraData(True) then Exit;
      if not SafePath(Marker) or not DeleteFile(Marker) then Exit;
      for I := 0 to Directories.Count - 1 do
        if SafePath(Directories[I]) then RemoveDir(Directories[I]);
      RemoveDir(R + '\namespaces');
      RemoveDir(L + '\announcement-webview');
      RemoveDir(R);
      RemoveDir(L);
      Result := True;
    except
      Result := False;
    end;
  finally
    Files.Free;
    Caches.Free;
    Directories.Free;
    Identities.Free;
  end;
end;

function InfoValue(FileName, Key: String): String;
var
  Lines: TArrayOfString;
  I: Integer;
begin
  Result := '';
  if not SafePath(FileName) or not LoadStringsFromFile(FileName, Lines) then Exit;
  if GetArrayLength(Lines) > 1000 then Exit;
  for I := 0 to GetArrayLength(Lines) - 1 do
    if Pos(Key + ': ', Lines[I]) = 1 then begin
      if Result <> '' then begin Result := ''; Exit; end;
      Result := Copy(Lines[I], Length(Key) + 3, MaxInt);
    end;
end;

function TakePart(var Value: String; Separator: String): String;
var
  P: Integer;
begin
  P := Pos(Separator, Value);
  if P = 0 then begin Result := Value; Value := ''; end
  else begin Result := Copy(Value, 1, P - 1); Delete(Value, 1, P); end;
end;

function Numeric(Value: String): Boolean;
var I: Integer;
begin
  Result := False;
  if Value = '' then Exit;
  for I := 1 to Length(Value) do
    if Pos(Value[I], '0123456789') = 0 then Exit;
  Result := True;
end;

function CompareNumber(A, B: String): Integer;
begin
  Result := Length(A) - Length(B);
  if Result = 0 then Result := CompareStr(A, B);
end;

function SplitVersion(Value: String; var Core, Pre: String): Boolean;
var
  Rest, Item: String;
  I, J: Integer;
begin
  Result := False;
  if (Value = '') or (Value[Length(Value)] = '-') then Exit;
  Core := TakePart(Value, '-');
  if (Core = '') or (Core[Length(Core)] = '.') then Exit;
  Pre := Value;
  Rest := Core;
  Result := False;
  for I := 1 to 3 do begin
    if (I < 3) and (Pos('.', Rest) = 0) then Exit;
    Item := TakePart(Rest, '.');
    if not Numeric(Item) then Exit;
    if (Length(Item) > 1) and (Item[1] = '0') then Exit;
  end;
  if Rest <> '' then Exit;
  Rest := Pre;
  while Rest <> '' do begin
    Item := TakePart(Rest, '.');
    if Item = '' then Exit;
    for J := 1 to Length(Item) do
      if Pos(Item[J], 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-') = 0 then Exit;
    if Numeric(Item) and (Length(Item) > 1) and (Item[1] = '0') then Exit;
  end;
  if (Pre <> '') and (Pre[Length(Pre)] = '.') then Exit;
  Result := True;
end;

function CompareVersions(A, B: String; var Comparison: Integer): Boolean;
var
  AC, BC, AP, BP, X, Y: String;
  I: Integer;
begin
  Result := SplitVersion(A, AC, AP) and SplitVersion(B, BC, BP);
  Comparison := 0;
  if not Result then Exit;
  for I := 1 to 3 do begin
    X := TakePart(AC, '.'); Y := TakePart(BC, '.');
    Comparison := CompareNumber(X, Y);
    if Comparison <> 0 then Exit;
  end;
  if (AP = '') and (BP = '') then Exit;
  if AP = '' then begin Comparison := 1; Exit; end;
  if BP = '' then begin Comparison := -1; Exit; end;
  while (AP <> '') and (BP <> '') do begin
    X := TakePart(AP, '.'); Y := TakePart(BP, '.');
    if Numeric(X) and Numeric(Y) then Comparison := CompareNumber(X, Y)
    else if Numeric(X) then Comparison := -1
    else if Numeric(Y) then Comparison := 1
    else Comparison := CompareStr(X, Y);
    if Comparison <> 0 then Exit;
  end;
  if AP <> '' then Comparison := 1;
  if BP <> '' then Comparison := -1;
end;

function CheckUpgrade: String;
var
  Version: String;
  Comparison: Integer;
begin
  Result := '';
  if not PathPresent(ExpandConstant('{app}\BUILD-INFO.txt')) then begin
    if PathPresent(ExpandConstant('{app}\sidraviad.exe')) or
      PathPresent(ExpandConstant('{app}\sidraviactl.exe')) or
      PathPresent(ExpandConstant('{app}\Sidravia.exe')) then
      Result := '目录中有无法确认版本的程序，请先核实来源或选择空目录。';
    Exit;
  end;
  Version := InfoValue(ExpandConstant('{app}\BUILD-INFO.txt'), 'ProductVersion');
  if not CompareVersions(Version, '{#ProductVersion}', Comparison) then
    Result := '无法确认现有版本，程序文件尚未修改。请使用准确原版安装器修复。'
  else if Comparison > 0 then
    Result := '不支持直接降级。请先保存新版本数据；如需恢复旧版，使用与旧版匹配的恢复副本及准确安装包。';
end;

function PreserveUpgradeData: Boolean;
var
  R, ID, Original, CopyName, Description: String;
  GUID: TGUID;
  I: Integer;
begin
  Result := False;
  if not PathPresent(ExpandConstant('{app}\BUILD-INFO.txt')) then begin Result := True; Exit; end;
  R := ExpandConstant('{userappdata}\Sidravia');
  if not SafePath(R) then Exit;
  if not PathPresent(R + '\configurations.json') and not PathPresent(R + '\gui-settings.json') then begin
    Result := True; Exit;
  end;
  if NewGUID(GUID) <> 0 then Exit;
  ID := StringOfChar(' ', 39);
  if GUIDText(GUID, ID, 39) <> 39 then Exit;
  ID := Lowercase(Copy(ID, 1, 38));
  StringChangeEx(ID, '{', '', True);
  StringChangeEx(ID, '}', '', True);
  StringChangeEx(ID, '-', '', True);
  if not HexToken(ID) then Exit;
  Description := 'SourceVersion: ' + InfoValue(ExpandConstant('{app}\BUILD-INFO.txt'), 'ProductVersion') + #13#10 +
    'TargetVersion: {#ProductVersion}' + #13#10;
  for I := 0 to 1 do begin
    Original := R + '\configurations.json';
    CopyName := '.sidravia-migration-' + ID + '.json';
    if I = 1 then begin
      Original := R + '\gui-settings.json';
      CopyName := '.sidravia-gui-migration-' + ID + '.json';
    end;
    if not SafePath(Original) then Exit;
    if PathPresent(Original) then begin
      if DirExists(Original) then Exit;
      { Never read credentials into script strings or diagnostics. }
      if not PreserveProtectedFile(Original, R + '\' + CopyName) then Exit;
      if GetSHA256OfFile(Original) <> GetSHA256OfFile(R + '\' + CopyName) then Exit;
      Description := Description + ExtractFileName(Original) + ': ' + CopyName + #13#10;
    end;
  end;
  Result := SaveStringToFile(R + '\.sidravia-upgrade-' + ID + '.txt', Description, False);
end;

function RunManager(Arguments: String): Boolean;
var
  ExitCode: Integer;
begin
  Result := Exec(ExpandConstant('{app}\sidraviamgr.exe'), Arguments,
    ExpandConstant('{app}'), SW_HIDE, ewWaitUntilTerminated, ExitCode);
  Result := Result and (ExitCode = 0);
end;

function RemoveGUIComponent: Boolean;
begin
#if IncludeGUI
  Result := not WizardIsComponentSelected('gui');
#else
  Result := False;
#endif
end;

function InstallationConflict(PortableExists, GUIExists: Boolean): String;
begin
  Result := '';
  if PortableExists then
    Result := '此文件夹中已有 Sidravia 便携版，请选择其他安装位置。便携版的配置不会自动迁移';
#if IncludeGUI
#else
  if (Result = '') and GUIExists then
    Result := '此目录已有图形界面。请使用含图形界面的安装器覆盖，并取消图形界面组件；此安装器不会遗留或删除旧图形界面文件。';
#endif
end;

function SelectedStartupMode(GUISelected: Boolean; SelectedIndex: Integer): String;
begin
  Result := 'daemon';
#if IncludeGUI
  if GUISelected and (SelectedIndex = 0) then Result := 'gui';
#endif
end;

function HasSelectedGUI: Boolean;
begin
#if IncludeGUI
  Result := WizardIsComponentSelected('gui');
#else
  Result := False;
#endif
end;

function EffectiveStartupMode: String;
begin
  Result := SelectedStartupMode(HasSelectedGUI, StartupPage.SelectedValueIndex);
end;

function HasExplicitTaskSelection: Boolean;
var
  I: Integer;
  Value, FileName: String;
begin
  Result := True;
  for I := 1 to ParamCount do begin
    Value := Uppercase(ParamStr(I));
    if Pos('/TASKS=', Value) = 1 then Exit;
    if Pos('/LOADINF=', Value) = 1 then begin
      FileName := ExpandFileName(RemoveQuotes(Copy(ParamStr(I), 10, MaxInt)));
      { An INF without Tasks does not override actual integration defaults. }
      if GetIniString('Setup', 'Tasks', '#sidravia-missing-tasks#', FileName) <>
        '#sidravia-missing-tasks#' then Exit;
    end;
  end;
  Result := False;
end;

function IntegrationTaskDefault(TaskName: String; ActualSelected: Boolean): Boolean;
var
  I, Separator: Integer;
  Value, Item: String;
begin
  Result := ActualSelected;
  for I := 1 to ParamCount do begin
    if Pos('/MERGETASKS=', Uppercase(ParamStr(I))) = 1 then begin
      Value := Lowercase(RemoveQuotes(Copy(ParamStr(I), 13, MaxInt))) + ',';
      while Value <> '' do begin
        Separator := Pos(',', Value);
        Item := Trim(Copy(Value, 1, Separator - 1));
        Delete(Value, 1, Separator);
        while (Length(Item) > 0) and
          ((Copy(Item, 1, 1) = '!') or (Copy(Item, 1, 1) = '*')) do
          Delete(Item, 1, 1);
        if Item = TaskName then begin
          { Inno has already interpreted select/deselect and child-task syntax.
            Preserve only the explicitly mentioned task from that result. }
          Result := WizardIsTaskSelected(TaskName);
          Exit;
        end;
      end;
    end;
  end;
end;

function ReadIntegrationState: Boolean;
var
  Options, Mode: String;
begin
  Result := False;
  if (SettingsDirectory = ExpandConstant('{app}')) and (SettingsError = '') then begin
    Result := True;
    Exit;
  end;
  SettingsDirectory := ExpandConstant('{app}');
  SettingsError := '无法确认现有自启和 PATH 设置，请处理任务冲突或权限问题后重试。程序文件尚未修改。';
  if not QueryManager(True) then Exit;
  Mode := ManagerMode;
  if FileExists(SettingsDirectory + '\BUILD-INFO.txt') or
    FileExists(SettingsDirectory + '\sidraviad.exe') then begin
    Options := '';
    if IntegrationTaskDefault('autostart', Mode <> 'disabled') then Options := 'autostart';
    if IntegrationTaskDefault('userpath', ManagerPath) then begin
      if Options <> '' then Options := Options + ',';
      Options := Options + 'userpath';
    end;
#if IncludeGUI
    if WizardIsTaskSelected('desktopicon') then begin
      if Options <> '' then Options := Options + ',';
      Options := Options + 'desktopicon';
    end;
    if Mode = 'daemon' then StartupPage.SelectedValueIndex := 1
    else StartupPage.SelectedValueIndex := 0;
#endif
    if not HasExplicitTaskSelection then WizardSelectTasks(Options);
  end;
  SettingsError := '';
  Result := True;
end;

procedure SetChoiceListPadding(List: TNewCheckListBox);
begin
  { Inno 7.1.0 reserves the legacy 13px checkbox bitmap width but draws
    DPI-scaled theme glyphs. Allow for that growth plus 4 logical pixels
    inside the list, so a level-zero checkbox/radio cannot start offscreen. }
  List.Offset := ScaleX(4) + ScaleX(13) - 13;
end;

procedure InitializeWizard;
begin
  SetChoiceListPadding(WizardForm.ComponentsList);
  SetChoiceListPadding(WizardForm.TasksList);
  SetChoiceListPadding(WizardForm.RunList);
  StartupPage := CreateInputOptionPage(wpSelectTasks, '开机自启方式',
    '登录 Windows 后，如何启动 Sidravia？',
    '选择适合你的启动方式。', True, False);
#if IncludeGUI
  StartupPage.Add('启动到托盘' + #13#10 +
    '启动图形界面并隐藏主窗口，可通过托盘图标管理连接');
#endif
  StartupPage.Add('仅启动后台' + #13#10 +
    '不显示窗口和托盘图标，通过命令行管理连接');
  SetChoiceListPadding(StartupPage.CheckListBox);
  StartupPage.SelectedValueIndex := 0;
#if IncludeGUI
  if GetPreviousData('StartupMode', 'gui') = 'daemon' then
    StartupPage.SelectedValueIndex := 1;
#endif
  StartupHint := TNewStaticText.Create(StartupPage);
  StartupHint.Parent := StartupPage.Surface;
  StartupHint.AutoSize := False;
  StartupHint.WordWrap := True;
  StartupHint.SetBounds(0, StartupPage.SurfaceHeight - ScaleY(36),
    StartupPage.SurfaceWidth, ScaleY(36));
  StartupHint.Anchors := [akLeft, akRight, akBottom];
  StartupHint.Caption := '启用前，建议关闭其他认证器及其开机自启';
  StartupPage.CheckListBox.Height := StartupHint.Top - StartupPage.CheckListBox.Top - ScaleY(8);
end;

procedure RegisterPreviousData(PreviousDataKey: Integer);
begin
  SetPreviousData(PreviousDataKey, 'StartupMode', EffectiveStartupMode);
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := (PageID = StartupPage.ID) and
    (not WizardIsTaskSelected('autostart') or not HasSelectedGUI);
end;

procedure CurPageChanged(PageID: Integer);
var
  I: Integer;
begin
  if (PageID = wpReady) and FileExists(ExpandConstant('{app}\BUILD-INFO.txt')) then begin
    WizardForm.ReadyLabel.Caption := '将覆盖更新此目录的 Sidravia：' +
      InfoValue(ExpandConstant('{app}\BUILD-INFO.txt'), 'ProductVersion') + ' → {#ProductVersion}' + #13#10 +
      '连接将暂时断开，已保存配置会保留。支持的旧版会正常退出；其他版本请手动退出。';
  end;
  if PageID = wpSelectComponents then begin
    for I := 0 to WizardForm.ComponentsList.Items.Count - 1 do begin
      if WizardForm.ComponentsList.ItemCaption[I] = '核心与命令行工具（必需）' then
        WizardForm.ComponentsList.ItemCaption[I] := '核心与命令行工具（必需）' + #13#10 +
          '提供网络认证、后台运行和系统设置管理';
      if WizardForm.ComponentsList.ItemCaption[I] = '图形界面' then
        WizardForm.ComponentsList.ItemCaption[I] := '图形界面' + #13#10 +
          '通过窗口管理连接和设置';
    end;
  end;
  if PageID = wpSelectTasks then begin
    for I := 0 to WizardForm.TasksList.Items.Count - 1 do begin
      if WizardForm.TasksList.ItemCaption[I] = '开机自启' then
        WizardForm.TasksList.ItemCaption[I] := '开机自启' + #13#10 +
          '登录 Windows 后自动启动 Sidravia';
      if WizardForm.TasksList.ItemCaption[I] = '加入用户 PATH' then
        WizardForm.TasksList.ItemCaption[I] := '加入用户 PATH' + #13#10 +
          '在新打开的终端中直接使用 Sidravia 命令';
    end;
  end;
  if (PageID = wpFinished) and IntegrationFailed then begin
    WizardForm.FinishedHeadingLabel.Caption := 'Sidravia 已安装，部分设置未完成';
    WizardForm.FinishedLabel.Caption := '程序文件已安装，但开机自启或 PATH 设置未能完成。' + #13#10#13#10 +
      IntegrationDetail + #13#10 + '请重跑安装器，在安装选项页重新确认本次需要的设置。不会自动重放先前选择。';
  end;
end;

function UpdateReadyMemo(Space, NewLine, MemoUserInfoInfo, MemoDirInfo,
  MemoTypeInfo, MemoComponentsInfo, MemoGroupInfo, MemoTasksInfo: String): String;
var
  Components, Startup, Options: String;
begin
  Components := '核心与命令行工具';
  if HasSelectedGUI then Components := Components + '、图形界面';
  Startup := '关闭';
  if WizardIsTaskSelected('autostart') then begin
    if EffectiveStartupMode = 'gui' then Startup := '启动到托盘'
    else Startup := '仅启动后台';
    if not HasSelectedGUI then
      Startup := Startup + NewLine + Space + '启用前，建议关闭其他认证器及其开机自启';
  end;
  Options := '';
  if WizardIsTaskSelected('userpath') then Options := Space + '加入用户 PATH' + NewLine;
#if IncludeGUI
  if HasSelectedGUI and WizardIsTaskSelected('desktopicon') then
    Options := Options + Space + '创建桌面快捷方式' + NewLine;
#endif
  if Options = '' then Options := Space + '无' + NewLine;
  Result := '安装位置' + NewLine + Space + WizardDirValue + NewLine + NewLine +
    '安装组件' + NewLine + Space + Components + NewLine + NewLine +
    '开机自启' + NewLine + Space + Startup + NewLine + NewLine +
    '其他选项' + NewLine + Options;
end;

function NextButtonClick(PageID: Integer): Boolean;
begin
  Result := True;
  if PageID = wpSelectTasks then IntegrationConfirmed := True;
  if PageID = wpSelectDir then begin
    Result := ReadIntegrationState;
    if not Result then MsgBox(SettingsError, mbError, MB_OK);
    if not Result then Exit;
  end;
#if IncludeGUI
  if (PageID = StartupPage.ID) and (StartupPage.SelectedValueIndex = 0) and
    not WizardIsComponentSelected('gui') then begin
    MsgBox('请安装图形界面，或选择仅启动后台。', mbError, MB_OK);
    Result := False;
  end;
#endif
end;

{ The application owns drafts, connection intent and normal exit. Pascal
  invokes fixed short CTL stages and still checks all target processes itself. }
function RunUpdateStage(Stage: String): Boolean;
var Code: Integer; Args: String;
begin
  Args := 'update-internal ' + Stage + ' --operation ' + UpdateOperation +
    ' --coordinator ' + IntToStr(CurrentPID);
  if not WizardSilent then Args := Args + ' --interactive';
  Result := Exec(ExpandConstant('{app}\sidraviactl.exe'), Args, '', SW_HIDE,
    ewWaitUntilTerminated, Code) and (Code = 0);
end;

procedure AbortApplicationUpdate;
begin
  if not RunUpdateStage('abort') then begin
    Log('Application recovery was not confirmed; program files were not changed.');
    if not WizardSilent then MsgBox('程序文件尚未修改，但未能确认恢复原运行状态。请手动启动 Sidravia；未恢复的连接请手动连接。', mbError, MB_OK);
  end;
end;

function PrepareApplicationUpdate: String;
var Code: Integer; ID: String; GUID: TGUID;
begin
  Result := '';
  if UpdatePrepared then begin
    Result := '上次更新准备尚未结束，请取消安装以恢复原程序后重试。'; Exit;
  end;
  if not FileExists(ExpandConstant('{app}\sidraviactl.exe')) or
    not Exec(ExpandConstant('{app}\sidraviactl.exe'), 'update-internal capabilities',
      '', SW_HIDE, ewWaitUntilTerminated, Code) or (Code <> 0) then begin
    Result := '此版本不支持自动正常退出，请手动退出此目录的 GUI、后台和命令行后重试。'; Exit;
  end;
  if NewGUID(GUID) <> 0 then begin Result := '无法创建更新操作。'; Exit; end;
  ID := StringOfChar(' ', 39);
  if GUIDText(GUID, ID, 39) <> 39 then begin Result := '无法创建更新操作。'; Exit; end;
  UpdateOperation := Lowercase(Copy(ID, 1, 38));
  StringChangeEx(UpdateOperation, '{', '', True);
  StringChangeEx(UpdateOperation, '}', '', True);
  StringChangeEx(UpdateOperation, '-', '', True);
  if not HexToken(UpdateOperation) then begin Result := '更新操作无效。'; Exit; end;
  UpdatePrepared := True;
  if not RunUpdateStage('prepare') then begin
    Result := '程序未同意更新准备，或仍有未保存修改。请取消安装以恢复操作后重试。'; Exit;
  end;
  if not RunUpdateStage('stop') then
    Result := '未能确认程序正常退出。程序文件尚未修改，请取消安装并核实运行状态。';
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := InstallationConflict(
    PathPresent(ExpandConstant('{app}\sidravia.portable')),
    FileExists(ExpandConstant('{app}\Sidravia.exe')));
  if (Result = '') and not SafePath(ExpandConstant('{app}')) then
    Result := '安装位置包含链接、无法访问或异常路径，请选择普通文件夹。';
  if (Result = '') and not ReadIntegrationState then Result := SettingsError;
  if (Result = '') and WizardSilent and not HasExplicitTaskSelection then
    Result := '静默运行必须通过 /TASKS 或含 Tasks 的 INF 明确本次自启和 PATH 目标；请交互运行以确认。';
  if (Result = '') and not WizardSilent and not IntegrationConfirmed then
    Result := '请返回安装选项页，确认本次开机自启和 PATH 选择。';
  if Result = '' then Result := CheckUpgrade;
  if (Result = '') and not ProductExited(False) then Result := PrepareApplicationUpdate;
  if (Result = '') and not ProductExited(False) then
    Result := '仍有此目录的程序占用，请取消安装后核实；无法确认退出时不会覆盖文件。';
end;

procedure CurStepChanged(Step: TSetupStep);
var
  Mode: String;
begin
  if Step = ssInstall then begin
    if not SafePath(ExpandConstant('{app}')) or not ProductExited(False) then
      RaiseException('安装前检查失败，请退出产品并重试。程序文件尚未修改。');
    if not PreserveUpgradeData then
      RaiseException('无法保存升级前的恢复副本，程序文件尚未修改。');
    UpdateFilesStarted := True;
  end;
  if Step = ssPostInstall then begin
    WizardForm.StatusLabel.Caption := '正在设置开机自启…';
    if WizardIsTaskSelected('autostart') then begin
      Mode := EffectiveStartupMode;
      IntegrationFailed := not RunManager('autostart set ' + Mode);
    end else
      IntegrationFailed := not RunManager('autostart disable');
    if IntegrationFailed then
      IntegrationDetail := '未完成：开机自启和 PATH；PATH 尚未尝试。'
    else begin
      WizardForm.StatusLabel.Caption := '正在设置命令行环境…';
      if WizardIsTaskSelected('userpath') then
        IntegrationFailed := not RunManager('path enable')
      else
        IntegrationFailed := not RunManager('path disable');
      if IntegrationFailed then IntegrationDetail := '开机自启已完成；未完成：PATH。';
    end;
    if IntegrationFailed then
      MsgBox('程序文件已安装。' + #13#10 + IntegrationDetail + #13#10 +
        '请检查冲突或权限问题后重跑安装器，重新确认未完成选项。重跑按当前状态显示默认值，不自动重试旧选择。', mbError, MB_OK);
  end;
end;

function GetCustomSetupExitCode: Integer;
begin
  Result := 0;
  if IntegrationFailed then Result := 1;
end;

function InitializeUninstall: Boolean;
begin
  Result := HoldInstaller and SafePath(ExpandConstant('{app}')) and
    FileExists(ExpandConstant('{app}\sidraviamgr.exe'));
  if Result then Result := ProductExited(False) and QueryManager(False);
  if not Result then
    MsgBox('卸载前检查失败。请退出产品并处理任务冲突；工具缺失或损坏时，先用可信同源安装器修复。程序文件尚未删除。', mbError, MB_OK);
end;

procedure CurUninstallStepChanged(Step: TUninstallStep);
var
  DeleteData: Boolean;
begin
  if Step = usUninstall then begin
    DeleteData := False;
    if not UninstallSilent then
      DeleteData := MsgBox('是否完整清除当前账户的账号、GUI 设置、日志、迁移副本和公告缓存/通知状态？这也影响同账户的隔离实例及便携版共享缓存。外部导出文件、用户机构覆盖和便携目录内数据保留。选择“否”保留数据。', mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
    if FileExists(ExpandConstant('{userappdata}\Sidravia\.cleanup-incomplete')) then begin
      if not DeleteData then
        RaiseException('此前完整清理尚未完成。请交互重跑并选择完整清除；不会将其报告为保留数据的完整卸载。');
    end;
    if not ProductExited(DeleteData) or not QueryManager(False) then
      RaiseException('请先正常退出相关产品实例并处理任务冲突；程序文件尚未删除。');
    if not RunManager('autostart disable') then
      RaiseException('撤销自启失败；程序和数据尚未删除。请核实当前状态后重试。');
    if not RunManager('path disable') then
      RaiseException('撤销 PATH 失败；程序和数据尚未删除。自启可能已撤销，请重试。');
    if DeleteData and not ClearExtraData(False) then
      RaiseException('完整清理未完成，程序和卸载入口保留。部分数据/系统集成可能已移除；请处理占用或未知归属后重跑完整清理。');
  end;
end;
