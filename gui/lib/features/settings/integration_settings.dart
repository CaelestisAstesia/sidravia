import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/platform/installation_manager.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

import 'settings_dropdown.dart';

class IntegrationSettings extends StatefulWidget {
  const IntegrationSettings({
    super.key,
    this.manager,
    this.createManager,
    this.builder,
  });
  final InstallationManager? manager;
  final InstallationManager Function()? createManager;
  final Widget Function(List<Widget> startupRows, Widget path)? builder;
  @override
  State<IntegrationSettings> createState() => _IntegrationSettingsState();
}

class _IntegrationSettingsState extends State<IntegrationSettings> {
  late final InstallationManager _manager;
  IntegrationStatus? _status;
  UserServiceStatus? _service;
  String? _error;
  String? _serviceError;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _manager =
        widget.manager ?? (widget.createManager ?? createInstallationManager)();
    unawaited(_perform());
  }

  Future<void> _perform([IntegrationChange? change]) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
      _serviceError = null;
    });
    try {
      if (change != null) await _manager.change(change);
      final next = await _manager.status();
      final services = _manager;
      final service = services is UserServiceManager
          ? await (services as UserServiceManager).userService('status')
          : null;
      if (mounted) {
        setState(() {
          _status = next;
          _service = service;
        });
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() {
          _status = null;
          _error = error is IntegrationFailure ? error.message : '无法读取系统集成状态。';
        });
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _changeService(String action) async {
    final manager = _manager;
    if (_busy || manager is! UserServiceManager) return;
    final services = manager as UserServiceManager;
    if (const {
      'replace',
      'takeover',
      'release',
      'recover',
      'discard',
      'discard-damaged',
      'unregister',
      'linger-on',
      'linger-off',
    }.contains(action)) {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('确认修改用户服务'),
          content: Text(
            action.startsWith('linger')
                ? '此设置影响当前用户的所有 systemd 用户服务。'
                : action == 'discard-damaged'
                ? '清除损坏的切换记录。保留当前后台、登记和账号配置。'
                : action == 'discard'
                ? '放弃恢复，停止后台并解除登记；各安装的账号配置会保留。'
                : action == 'takeover' || action == 'release'
                ? '切换会短暂中断认证。一次性认证需要重新输入。'
                : '此操作可能中断认证。各安装的账号配置会保留。',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('确认'),
            ),
          ],
        ),
      );
      if (confirmed != true || !mounted || _busy) return;
    }
    setState(() {
      _busy = true;
      _serviceError = null;
    });
    try {
      final next = await services.userService(action);
      if (mounted) setState(() => _service = next);
    } on IntegrationFailure catch (error) {
      final actual = await services.userService('status');
      if (mounted) {
        setState(() {
          _service = actual;
          _serviceError = error.message;
        });
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  List<Widget> _serviceRows(UserServiceStatus state) {
    final labels = <String, String>{
      if (state.recoveryBlocked) ...{
        if (state.canDiscardDamaged) 'discard-damaged': '清除损坏的切换记录',
      } else if (state.recoveryPending) ...{
        'recover': '恢复原管理方式',
        if (state.canDiscardReplacement) 'discard': '放弃恢复并解绑',
      } else if (state.directory == null)
        'takeover': '切换到 systemd'
      else ...{
        if (!state.matchesInstallation) 'replace': '更换为本安装',
        if (state.matchesInstallation) ...{
          'release': '切回应用管理',
          if (state.running) 'stop': '停止后台' else 'start': '启动后台',
          if (state.enabled) 'disable': '关闭后台自启' else 'enable': '开启后台自启',
        },
        'unregister': '解除登记',
      },
    };
    final current = !state.available || state.recoveryBlocked
        ? '状态未知'
        : state.recoveryPending
        ? '等待恢复'
        : state.directory == null
        ? '应用管理'
        : state.matchesInstallation
        ? 'systemd'
        : 'systemd · 其他安装';
    return [
      DesignRow(
        title: '后台管理方式',
        subtitle:
            _serviceError ??
            (state.recoveryBlocked
                ? state.canDiscardDamaged
                      ? '切换记录已损坏，无法自动恢复。'
                      : '无法确认切换记录，请检查用户服务目录。'
                : null),
        trailing: SettingsDropdown<String>(
          frameKey: const ValueKey('user-service-frame'),
          dropdownKey: const ValueKey('user-service-selector'),
          value: null,
          hint: current,
          labels: labels,
          onChanged: _busy || labels.isEmpty ? null : _changeService,
        ),
      ),
      if (state.available)
        DesignRow(
          title: '注销后保留用户服务',
          subtitle: '开启后，设为自启的用户服务也可在登录前启动',
          trailing: DesignSwitch(
            label: '注销后保留用户服务',
            value: state.linger,
            onChanged: _busy || state.recoveryPending || state.recoveryBlocked
                ? null
                : (value) => _changeService(value ? 'linger-on' : 'linger-off'),
          ),
        ),
    ];
  }

  @override
  void dispose() {
    unawaited(_manager.close());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final status = _status;
    final service = _service;
    Widget withRetry(Widget row) => _error == null
        ? row
        : Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              row,
              Align(
                alignment: AlignmentDirectional.centerStart,
                child: Padding(
                  padding: const EdgeInsetsDirectional.only(
                    start: 12,
                    bottom: 8,
                  ),
                  child: TextButton(
                    onPressed: _busy ? null : () => _perform(),
                    child: const Text('重试'),
                  ),
                ),
              ),
            ],
          );
    final unknownState = _busy ? '正在读取…' : '状态未知';
    final pathRow = DesignRow(
      title: '加入用户 PATH',
      subtitle:
          _error ??
          status?.pathNotice ??
          (status?.userPathSupported == false
              ? 'Linux 暂不支持通过 GUI 修改 PATH'
              : '使新打开的终端可以直接使用命令'),
      trailing: status == null
          ? Text(unknownState)
          : DesignSwitch(
              label: '加入用户 PATH',
              value: status.pathEnabled,
              onChanged: _busy || !status.userPathSupported
                  ? null
                  : (enabled) => _perform(
                      enabled
                          ? IntegrationChange.pathEnable
                          : IntegrationChange.pathDisable,
                    ),
            ),
    );
    final autostartRow = DesignRow(
      title: '开机自启',
      subtitle:
          _error ??
          (status?.autostart == 'conflict'
                  ? '任务存在冲突，未修改\n启用前，建议关闭其他认证器及其开机自启'
                  : (status?.autostartNotice ??
                        (status?.daemonAutostartSupported == false
                            ? '登录桌面会话后启动；Linux 暂不支持仅后台自启'
                            : '启用前，建议关闭其他认证器及其开机自启'))) +
              (status != null && !status.guiAvailable
                  ? '\n图形程序不可用，无法启动到托盘'
                  : ''),
      trailing: SettingsDropdown<String>(
        frameKey: const ValueKey('autostart-frame'),
        dropdownKey: const ValueKey('autostart-selector'),
        value: status == null || status.autostart == 'conflict'
            ? null
            : status.autostart,
        hint: unknownState,
        labels: const {'disabled': '关闭', 'gui': '启动到托盘', 'daemon': '仅后台'},
        disabledValues: {
          if (!(status?.guiAvailable ?? false)) 'gui',
          if (status?.daemonAutostartSupported == false ||
              service?.directory != null)
            'daemon',
        },
        onChanged: _busy || status == null || status.autostart == 'conflict'
            ? null
            : (mode) => _perform(switch (mode) {
                'gui' => IntegrationChange.startGui,
                'daemon' => IntegrationChange.startDaemon,
                _ => IntegrationChange.startDisable,
              }),
      ),
    );
    final startupRows = <Widget>[
      withRetry(autostartRow),
      if (service != null)
        ..._serviceRows(service)
      else if (_serviceError != null)
        DesignRow(
          title: '后台管理方式',
          subtitle: _serviceError,
          trailing: TextButton(
            onPressed: _busy ? null : () => _perform(),
            child: const Text('重试'),
          ),
        ),
    ];
    return widget.builder?.call(startupRows, withRetry(pathRow)) ??
        DesignGroup(children: [...startupRows, withRetry(pathRow)]);
  }
}
