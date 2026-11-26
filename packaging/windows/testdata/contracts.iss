{ Native test body appended to the real [Code] by test_inno_installer.py.
  Every path and AUMID is redirected to that test's random owned fixture. }
function SetAttributes(Name: String; Attributes: LongWord): Boolean;
  external 'SetFileAttributesW@kernel32.dll stdcall';
function OpenOccupiedFile(Name: String; Access, Sharing: LongWord; Attributes: NativeUInt;
  Disposition, Flags: LongWord; Template: NativeUInt): NativeUInt;
  external 'CreateFileW@kernel32.dll stdcall';
procedure AssertContract(Condition: Boolean; Description: String);
begin
  if not Condition then RaiseException(Description);
end;

procedure Put(Name: String; Value: String);
begin
  AssertContract(ForceDirectories(ExtractFileDir(Name)), 'fixture directory');
  AssertContract(SaveStringToFile(Name, Value, False), 'fixture write');
end;

function InitializeSetup: Boolean;
var
  R, L, A, Key, Token, Backup, OppositeTarget: String;
  Comparison: Integer;
  Find: TFindRec;
  Occupied: NativeUInt;
begin
  Result := False; { Never proceed into an install. }
  R := '@ROOT@\roaming\Sidravia';
  L := '@ROOT@\local\Sidravia';
  A := '@ROOT@\app';
  Key := ToastKey('io.github.astesia.Sidravia.InnoTest.FIXTURE');
  try
    AssertContract(PreserveProtectedFile('@ROOT@\protected-original.json', '@ROOT@\protected-copy.json'), 'protected snapshot creation');
    AssertContract(not SafePath(R + '\namespaces\linked'), 'junction ancestor rejected');
    AssertContract(not ClearExtraData(False), 'junction blocks complete clear');
    AssertContract(FileExists('@ROOT@\outside\sentinel.txt'), 'junction target survives');
    AssertContract(RemoveDir(R + '\namespaces\linked'), 'remove only fixture junction');
    AssertContract(HoldInstaller, 'per-user installer lock');
    ReleaseInstaller;
    AssertContract(ParseManagerJSON('{"version":1,"ok":true,"code":"ok","capabilities":["user_path","user_autostart"],"directory":"D:\\quoted\\\"autostart\\\":false","pathEnabled":true,"changed":false,"autostart":"daemon","guiAvailable":false}'), 'one status JSON contract');
    AssertContract(ManagerPath and (ManagerMode = 'daemon'), 'state fields');
    AssertContract(not ParseManagerJSON('{"version":1,"ok":true,"code":"ok","pathEnabled":true,"autostart":"conflict"}'), 'task conflict');
    AssertContract(not ParseManagerJSON('{"version":1,"ok":true,"code":"ok","pathEnabled":true,"autostart":"gui","ok":false}'), 'duplicate key');
    AssertContract(not ParseManagerJSON('{"version":1,"ok":true,"code":"ok","pathEnabled":true,"autostart":"gui"} garbage'), 'trailing JSON');
    AssertContract(not ParseManagerJSON('{"version":1,"ok":true,"code":"ok","pathEnabled":"true","autostart":"gui"}'), 'typed booleans');
    AssertContract(CompareVersions('0.3.0-alpha.1', '0.3.0-alpha.2', Comparison) and (Comparison < 0), 'alpha upgrade');
    AssertContract(CompareVersions('0.3.0-alpha.10', '0.3.0-alpha.2', Comparison) and (Comparison > 0), 'numeric prerelease');
    AssertContract(CompareVersions('0.3.0', '0.3.0-alpha.2', Comparison) and (Comparison > 0), 'release downgrade');
    AssertContract(not CompareVersions('0.3', '0.3.0', Comparison), 'unknown version');
    AssertContract(not CompareVersions('0.3.0.1', '0.3.0', Comparison), 'extra version part');
    AssertContract(LogName('sidraviad-2026-10-19-123456.log'), 'rotated log');
    AssertContract(not LogName('sidraviad-2026-10-19-123456.log.bak'), 'external log');
    Token := '0123456789abcdef0123456789abcdef';
    AssertContract(MigrationName('.sidravia-migration-' + Token + '.json'), 'migration ownership');
    AssertContract(not MigrationName('.sidravia-migration-foreign.json'), 'foreign migration');
    AssertContract(ValidNamespace('test-A') and not ValidNamespace('..') and not ValidNamespace('prod'), 'namespace grammar');
    AssertContract(ProductExited(True), 'native current-user process query');
    AssertContract(ClearToastHistory('io.github.astesia.Sidravia.InnoTest.FIXTURE'), 'direct Inno WinRT history ABI');
    Put(R + '\configurations.json', '{"synthetic":"secret"}');
    Put(R + '\gui-settings.json', '{"theme":"old"}');
    Put(R + '\.sidravia-' + Token + '.tmp', 'synthetic secret after crash');
    Put(R + '\gui-settings.json.123.4294967295.tmp', 'old GUI temp');
    Put(R + '\.sidravia-foreign.tmp', 'keep');
    Put(R + '\gui-settings.json.123.4294967296.tmp', 'keep');
    Put(R + '\namespaces\interrupted\.sidravia-' + Token + '.tmp', 'synthetic secret');
    AssertContract(not OppositeSetupRegistered, 'absent opposite registration');
    AssertContract(RegWriteStringValue(HKCU64, '@OPPOSITE_KEY@', 'InstallLocation', A), 'synthetic opposite registration');
    AssertContract(OppositeSetupRegistered, 'existing opposite registration blocks setup');
    AssertContract(RegDeleteKeyIncludingSubkeys(HKCU64, '@OPPOSITE_KEY@'), 'remove opposite fixture registration');
    Put(A + '\BUILD-INFO.txt', 'ProductVersion: 0.3.0-alpha.1' + #13#10 + 'Target: {#PayloadTarget}' + #13#10);
    AssertContract(PreserveUpgradeData, 'protected upgrade snapshots');
    AssertContract(FirstEntry(R + '\.sidravia-migration-*.json', Find), 'raw backup exists');
    Backup := R + '\' + Find.Name;
    FindClose(Find);
    AssertContract(GetSHA256OfFile(Backup) = GetSHA256OfFile(R + '\configurations.json'), 'snapshot exact bytes');
    AssertContract(CheckUpgrade = '', 'supported upgrade');
    if '{#PayloadTarget}' = 'windows-arm64' then OppositeTarget := 'windows-amd64'
    else OppositeTarget := 'windows-arm64';
    Put(A + '\BUILD-INFO.txt', 'ProductVersion: 0.3.0-alpha.1' + #13#10 + 'Target: ' + OppositeTarget + #13#10);
    AssertContract(CheckUpgrade <> '', 'other architecture cannot reuse this directory');
    Put(A + '\BUILD-INFO.txt', 'ProductVersion: 0.4.0' + #13#10 + 'Target: {#PayloadTarget}' + #13#10);
    AssertContract(CheckUpgrade <> '', 'downgrade blocked');
    Put(R + '\namespaces\test-A\configurations.json', '{}');
    Put(R + '\namespaces\test-A\foreign.txt', 'keep');
    Put(R + '\namespaces\test-A\institution-profiles\my-profile.json', 'keep');
    Put(L + '\logs\sidraviad-2026-10-19-123456.log', 'owned');
    Put(L + '\logs\external.log', 'keep');
    Put(L + '\logs\gui-settings.json.123.4.tmp', 'keep');
    Put(L + '\announcement-webview\ns-orphan\EBWebView\cache', 'owned');
    Put(L + '\announcement-webview\foreign\external.txt', 'keep');
    AssertContract(RegWriteStringValue(HKCU, Key, 'Foreign', 'keep'), 'synthetic registry');
    AssertContract(RegWriteStringValue(HKCU, Key, 'CustomActivator', '{00000000-0000-0000-0000-000000000001}'), 'unknown activator');
    AssertContract(not ClearExtraData(False), 'unknown activator must block');
    AssertContract(FileExists(R + '\configurations.json'), 'preflight must precede delete');
    AssertContract(RegDeleteValue(HKCU, Key, 'CustomActivator'), 'test reset');
    AssertContract(SetAttributes(R + '\gui-settings.json', 1), 'readonly fixture');
    AssertContract(not ClearExtraData(False), 'partial deletion must fail');
    AssertContract(FileExists(R + '\.cleanup-incomplete'), 'incomplete marker remains');
    AssertContract(SetAttributes(R + '\gui-settings.json', 0), 'readonly reset');
    Occupied := OpenOccupiedFile(R + '\gui-settings.json', $80000000, 0, 0, 3, $80, 0);
    AssertContract(Occupied <> NativeUInt(-1), 'occupied fixture');
    AssertContract(not ClearExtraData(False), 'occupied file must stop cleanup');
    AssertContract(CloseNativeHandle(Occupied), 'release fixture occupation');
    AssertContract(ClearExtraData(False), 'retry full bounded cleanup');
    AssertContract(not FileExists(R + '\.cleanup-incomplete'), 'marker finalized');
    AssertContract(not FileExists(R + '\gui-settings.json') and not FileExists(Backup), 'owned settings/backups removed');
    AssertContract(not FileExists(R + '\.sidravia-' + Token + '.tmp') and
      not FileExists(R + '\gui-settings.json.123.4294967295.tmp') and
      not FileExists(R + '\namespaces\interrupted\.sidravia-' + Token + '.tmp'), 'crashed sensitive temps removed');
    AssertContract(FileExists(R + '\.sidravia-foreign.tmp') and FileExists(R + '\gui-settings.json.123.4294967296.tmp'), 'similar external temps retained');
    AssertContract(not FileExists(L + '\logs\sidraviad-2026-10-19-123456.log'), 'owned rotated log removed');
    AssertContract(not DirExists(L + '\announcement-webview\ns-orphan'), 'orphan cache removed');
    AssertContract(FileExists(L + '\logs\gui-settings.json.123.4.tmp'), 'GUI temp pattern is scoped to config root');
    AssertContract(FileExists(L + '\logs\external.log') and FileExists(R + '\namespaces\test-A\foreign.txt'), 'external files retained');
    AssertContract(FileExists(R + '\namespaces\test-A\institution-profiles\my-profile.json'), 'external profile retained');
    AssertContract(RegValueExists(HKCU, Key, 'Foreign'), 'unknown registry value retained');
    AssertContract(ClearExtraData(True), 'whole-table final verification');
    AssertContract(ClearExtraData(False), 'idempotent retry');
    Put(L + '\announcement-webview\production\external.txt', 'keep');
    AssertContract(not ClearExtraData(False), 'unknown cache sibling blocks recursive delete');
    AssertContract(FileExists(L + '\announcement-webview\production\external.txt'), 'unknown cache data preserved');
    SaveStringToFile('@ROOT@\result.txt', 'passed', False);
  except
    SaveStringToFile('@ROOT@\result.txt', 'FAILED: ' + GetExceptionMessage, False);
  end;
  { Exact random test identity only, never product registry. }
  RegDeleteKeyIncludingSubkeys(HKCU, Key);
  RegDeleteKeyIncludingSubkeys(HKCU64, ExtractFileDir('@OPPOSITE_KEY@'));
end;
