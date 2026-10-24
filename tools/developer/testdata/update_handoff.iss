#ifndef Host
  #error Host must be supplied
#endif
#ifndef Payload
  #error Payload must be supplied
#endif
[Setup]
AppName=Sidravia isolated update handoff
AppVersion=1
DefaultDirName={tmp}\unused-update-fixture
PrivilegesRequired=lowest
SetupArchitecture=x64
Uninstallable=no
CreateUninstallRegKey=no
DisableDirPage=yes
DisableProgramGroupPage=yes
DisableReadyPage=yes
CloseApplications=no
RestartApplications=no
OutputBaseFilename=handoff-fixture
[Files]
Source: "{#Host}"; DestName: "package-ctl.exe"; Flags: dontcopy
Source: "{#Payload}"; DestDir: "{app}"; DestName: "updater.exe"; Flags: ignoreversion
[Code]
var Updater: NativeUInt;
function OpenProcess(Access: LongWord; Inherit: Boolean; PID: LongWord): NativeUInt;
  external 'OpenProcess@kernel32.dll stdcall';
function WaitNative(Handle: NativeUInt; Milliseconds: LongWord): LongWord;
  external 'WaitForSingleObject@kernel32.dll stdcall';
function CloseHandle(Handle: NativeUInt): Boolean;
  external 'CloseHandle@kernel32.dll stdcall';
function CurrentPID: LongWord;
  external 'GetCurrentProcessId@kernel32.dll stdcall';
function Root: String;
begin Result := ExpandConstant('{param:ROOT}'); end;
function Operation: String;
begin Result := ExpandConstant('{param:OP}'); end;
function HostStage(Stage, Extra: String; var ExitCode: Integer): Boolean;
var Tool, Arguments: String;
begin
  Tool := ExpandConstant('{tmp}\package-ctl.exe');
  Arguments := Stage + ' ' + AddQuotes(Root) + ' ' + Operation;
  if Extra <> '' then Arguments := Arguments + ' ' + Extra;
  Result := Exec(Tool, Arguments, Root, SW_HIDE, ewWaitUntilTerminated, ExitCode);
end;
function InitializeSetup: Boolean;
var Code: Integer; Extra: String;
begin
  Result := False;
  Updater := OpenProcess($00100000 or $1000, False, StrToIntDef(ExpandConstant('{param:UPDATER}'), 0));
  if Updater = 0 then Exit;
  ExtractTemporaryFile('package-ctl.exe');
  Extra := ExpandConstant('{param:UPDATER}') + ' ' + IntToStr(CurrentPID);
  Result := HostStage('accept', Extra, Code) and (Code = 0);
end;
function PrepareToInstall(var NeedsRestart: Boolean): String;
var Code: Integer;
begin
  Result := 'updater did not exit';
  if WaitNative(Updater, 1500) <> 0 then Exit;
  Result := 'short host resolved wrong target';
  if not HostStage('inspect', '', Code) or (Code <> 0) then Exit;
  Result := '';
end;
procedure CurStepChanged(Step: TSetupStep);
var Code: Integer;
begin
  if Step <> ssDone then Exit;
  if not HostStage('finalize', '', Code) or (Code <> 0) then RaiseException('finalize failed');
  if not HostStage('resume', '', Code) or (Code <> 0) then RaiseException('resume failed');
  if not HostStage('resume', '', Code) or (Code <> 22) then RaiseException('recovery repeated');
  if not SaveStringToFile(Root + '\complete.txt', 'passed', False) then RaiseException('receipt failed');
end;
procedure DeinitializeSetup;
begin
  if Updater <> 0 then CloseHandle(Updater);
end;
