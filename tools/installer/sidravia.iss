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

[Code]
var
  StartupPage: TInputOptionWizardPage;
  IntegrationFailed: Boolean;
  StartupHint: TNewStaticText;

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

procedure InitializeWizard;
begin
  StartupPage := CreateInputOptionPage(wpSelectTasks, '开机自启方式',
    '登录 Windows 后，如何启动 Sidravia？',
    '选择适合你的启动方式。', True, False);
#if IncludeGUI
  StartupPage.Add('启动到托盘' + #13#10 +
    '启动图形界面并隐藏主窗口，可通过托盘图标管理连接');
#endif
  StartupPage.Add('仅启动后台' + #13#10 +
    '不显示窗口和托盘图标，通过命令行管理连接');
  StartupPage.SelectedValueIndex := 0;
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

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := (PageID = StartupPage.ID) and
    (not WizardIsTaskSelected('autostart') or not HasSelectedGUI);
end;

procedure CurPageChanged(PageID: Integer);
var
  I: Integer;
begin
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
      '请重新运行安装程序以重试这些设置。';
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
#if IncludeGUI
  if (PageID = StartupPage.ID) and (StartupPage.SelectedValueIndex = 0) and
    not WizardIsComponentSelected('gui') then begin
    MsgBox('请安装图形界面，或选择仅启动后台。', mbError, MB_OK);
    Result := False;
  end;
#endif
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := InstallationConflict(
    FileExists(ExpandConstant('{app}\sidravia.portable')),
    FileExists(ExpandConstant('{app}\Sidravia.exe')));
end;

procedure CurStepChanged(Step: TSetupStep);
var
  Mode: String;
begin
  if Step = ssPostInstall then begin
    WizardForm.StatusLabel.Caption := '正在设置开机自启…';
    if WizardIsTaskSelected('autostart') then begin
      Mode := EffectiveStartupMode;
      IntegrationFailed := not RunManager('autostart set ' + Mode);
    end else
      IntegrationFailed := not RunManager('autostart disable');
    if not IntegrationFailed then begin
      WizardForm.StatusLabel.Caption := '正在设置命令行环境…';
      if WizardIsTaskSelected('userpath') then
        IntegrationFailed := not RunManager('path enable')
      else
        IntegrationFailed := not RunManager('path disable');
    end;
    if IntegrationFailed then
      MsgBox('程序文件已安装，但开机自启或 PATH 设置未能完成。请检查自启任务冲突或权限问题，然后重新运行安装程序以重试这些设置。', mbError, MB_OK);
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
