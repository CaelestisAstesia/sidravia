#ifndef ProductVersion
  #error ProductVersion must be supplied by tools/build
#endif
#ifndef HasGUI
  #define HasGUI 0
#endif

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
UninstallDisplayName=Sidravia
DisableProgramGroupPage=yes
LicenseFile=payload\LICENSE

[Types]
Name: "full"; Description: "完整安装"
Name: "custom"; Description: "自定义安装"; Flags: iscustom

[Components]
Name: "core"; Description: "后台、命令行与管理工具"; Types: full custom; Flags: fixed
#if HasGUI
Name: "gui"; Description: "图形界面"; Types: full
#endif

[Tasks]
Name: "autostart"; Description: "开机自启（可能与其他认证器冲突，建议关闭其他认证器的自启并停止运行）"; GroupDescription: "启动设置："
Name: "userpath"; Description: "加入当前用户 PATH，供新终端使用"; GroupDescription: "命令行设置："
#if HasGUI
Name: "desktopicon"; Description: "创建桌面快捷方式"; Flags: unchecked
#endif

#include "files.iss"

[Icons]
#if HasGUI
Name: "{group}\Sidravia"; Filename: "{app}\Sidravia.exe"; Components: gui
Name: "{userdesktop}\Sidravia"; Filename: "{app}\Sidravia.exe"; Components: gui; Tasks: desktopicon
#endif
Name: "{group}\卸载 Sidravia"; Filename: "{uninstallexe}"

[Code]
var
  StartupPage: TInputOptionWizardPage;
  IntegrationFailed: Boolean;

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
#if HasGUI
  Result := not WizardIsComponentSelected('gui');
#else
  Result := False;
#endif
end;

procedure InitializeWizard;
begin
  StartupPage := CreateInputOptionPage(wpSelectTasks, '开机自启模式',
    '选择下次登录时启动的程序',
    '其他认证器可能与 Sidravia 冲突。建议关闭其他认证器的开机自启并停止运行。修改不会停止当前连接。', True, False);
#if HasGUI
  StartupPage.Add('GUI 启动到托盘');
#endif
  StartupPage.Add('仅启动后台，不显示 GUI');
  StartupPage.SelectedValueIndex := 0;
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := (PageID = StartupPage.ID) and not WizardIsTaskSelected('autostart');
end;

procedure CurPageChanged(PageID: Integer);
begin
#if HasGUI
  if (PageID = StartupPage.ID) and not WizardIsComponentSelected('gui') then
    StartupPage.SelectedValueIndex := 1;
#endif
  if (PageID = wpFinished) and IntegrationFailed then
    WizardForm.FinishedLabel.Caption := '程序文件已安装，但系统集成未完成。请检查任务冲突或权限，然后重新运行安装器修复。';
end;

function NextButtonClick(PageID: Integer): Boolean;
begin
  Result := True;
#if HasGUI
  if (PageID = StartupPage.ID) and (StartupPage.SelectedValueIndex = 0) and
    not WizardIsComponentSelected('gui') then begin
    MsgBox('请安装图形界面，或选择仅启动后台。', mbError, MB_OK);
    Result := False;
  end;
#endif
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  if FileExists(ExpandConstant('{app}\sidravia.portable')) then
    Result := '此目录是便携版，请选择另一个安装目录。便携版数据不会自动迁移。';
end;

procedure CurStepChanged(Step: TSetupStep);
var
  Mode: String;
begin
  if Step = ssPostInstall then begin
    if WizardIsTaskSelected('autostart') then begin
      Mode := 'daemon';
#if HasGUI
      if StartupPage.SelectedValueIndex = 0 then Mode := 'gui';
#endif
      IntegrationFailed := not RunManager('autostart set ' + Mode);
    end else
      IntegrationFailed := not RunManager('autostart disable');
    if not IntegrationFailed then begin
      if WizardIsTaskSelected('userpath') then
        IntegrationFailed := not RunManager('path enable')
      else
        IntegrationFailed := not RunManager('path disable');
    end;
    if IntegrationFailed then
      MsgBox('系统集成未完成。可能存在其他安装的自启任务或权限问题。程序文件已保留，可重新运行安装器修复。', mbError, MB_OK);
  end;
end;

function GetCustomSetupExitCode: Integer;
begin
  Result := 0;
  if IntegrationFailed then Result := 1;
end;

function InitializeUninstall: Boolean;
begin
  Result := FileExists(ExpandConstant('{app}\sidraviamgr.exe'));
  if not Result then begin
    MsgBox('维护工具缺失，请重新运行安装器修复后再卸载。', mbError, MB_OK);
    Exit;
  end;
  Result := RunManager('uninstall-check');
  if not Result then
    MsgBox('请先退出此安装的 GUI 和后台，并处理自启任务冲突，再重新卸载。', mbError, MB_OK);
end;

procedure CurUninstallStepChanged(Step: TUninstallStep);
var
  DeleteData: Boolean;
begin
  if Step = usUninstall then begin
    DeleteData := False;
    if not UninstallSilent then
      DeleteData := MsgBox('是否删除账号配置、GUI 设置和日志？选择“否”保留数据。测试实例数据和无关文件始终保留。', mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
    if not RunManager('uninstall-prepare') then
      RaiseException('卸载准备失败，程序文件尚未删除。请退出程序并检查自启任务冲突后重试。');
    if DeleteData and not RunManager('data delete') then
      RaiseException('数据删除未完成，程序文件尚未删除。请关闭当前用户的全部 Sidravia 实例后重试；部分系统集成可能已移除。');
  end;
end;
