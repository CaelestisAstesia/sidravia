import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/platform/installation/installation_manager.dart';
import 'package:sidravia_gui/platform/integration_failure.dart';
import 'package:sidravia_gui/platform/update/package_update_checker.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

import 'settings_dropdown.dart';
import 'update_settings.dart';

class IntegrationSettings extends StatefulWidget {
  const IntegrationSettings({
    super.key,
    this.manager,
    this.createManager,
    this.builder,
    this.onManagedAction,
    this.onServiceChanged,
    this.advancedOnly = false,
  });
  final InstallationManager? manager;
  final InstallationManager Function()? createManager;
  final Widget Function(List<Widget> startupRows, Widget path, Widget? update)?
  builder;
  final Future<bool> Function(String action)? onManagedAction;
  final Future<void> Function(String? action)? onServiceChanged;
  final bool advancedOnly;
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
    });
    String? mutationFailure;
    try {
      if (change != null) {
        try {
          await _manager.change(change);
        } on Object catch (error) {
          mutationFailure = error is IntegrationFailure
              ? error.message
              : '系统集成操作失败。';
        }
      }
      if (!mounted) return;
      final next = await _manager.status();
      final services = _manager;
      var service = next.userService;
      if (service == null && services is UserServiceManager) {
        try {
          service = await (services as UserServiceManager).userService(
            'status',
          );
        } on Object {
          if (mounted) _serviceError ??= '无法确认后台管理状态。';
        }
      }
      if (mounted) {
        setState(() {
          _status = next;
          if (mutationFailure != null) _error = '$mutationFailure 已重新读取当前状态。';
          _service = service;
          if (service != null) _serviceError = null;
        });
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() {
          _status = null;
          _service = null;
          _error = mutationFailure == null
              ? (error is IntegrationFailure ? error.message : '无法读取系统集成状态。')
              : '$mutationFailure 当前状态尚未确认，请重试查询。';
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
      'stop',
      'restart',
    }.contains(action)) {
      final confirmed = await showDesignConfirmation(
        context,
        title: Text(switch (action) {
          'stop' => '停止后台',
          'restart' => '重启后台',
          _ => '确认修改用户服务',
        }),
        content: Text(
          action.startsWith('linger')
              ? '此设置影响当前用户的所有 systemd 用户服务。'
              : action == 'discard-damaged'
              ? '清除损坏的切换记录。保留当前后台、登记和账号配置。'
              : action == 'discard'
              ? '放弃恢复，停止后台并解除登记；各安装的账号配置会保留。'
              : action == 'takeover' || action == 'release'
              ? '切换会短暂中断认证。一次性认证需要重新输入。'
              : action == 'stop'
              ? '当前连接会断开。再次启动后台后，可重新连接。'
              : action == 'restart'
              ? '当前连接会断开。后台按「启动时自动登录」的设置决定是否重新认证。'
              : '此操作可能中断认证。各安装的账号配置会保留。',
        ),
      );
      if (confirmed != true || !mounted || _busy) return;
    }
    setState(() {
      _busy = true;
      _serviceError = null;
    });
    String? failure;
    try {
      final managedAction = widget.onManagedAction;
      if (managedAction != null &&
          const {'start', 'stop', 'restart'}.contains(action)) {
        if (!await managedAction(action)) failure = '后台操作未完成，请查看当前状态。';
      } else {
        await services.userService(action);
      }
    } on Object catch (error) {
      failure = error is IntegrationFailure ? error.message : '后台服务操作失败。';
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    if (!mounted) return;
    await _perform();
    if (mounted && failure != null) setState(() => _serviceError = failure);
    if (mounted) {
      await widget.onServiceChanged?.call(failure == null ? action : null);
    }
  }

  List<Widget> _serviceRows(UserServiceStatus state) {
    final canChoose =
        state.available &&
        !state.recoveryBlocked &&
        !state.recoveryPending &&
        (state.directory == null || state.matchesInstallation);
    final needsRecovery =
        state.recoveryPending ||
        state.recoveryBlocked ||
        (state.directory != null && !state.matchesInstallation);
    return [
      DesignRow(
        title: '后台管理方式',
        subtitle: _serviceError ?? (needsRecovery ? '请到连接与诊断的高级排障处理。' : null),
        trailing: SettingsDropdown<String>(
          frameKey: const ValueKey('user-service-frame'),
          dropdownKey: const ValueKey('user-service-selector'),
          value: canChoose
              ? (state.matchesInstallation ? 'takeover' : 'release')
              : null,
          hint: needsRecovery ? '需要处理' : '状态未知',
          labels: const {'release': '应用管理', 'takeover': 'systemd'},
          onChanged: _busy || !canChoose
              ? null
              : (action) {
                  if ((action == 'takeover') != state.matchesInstallation) {
                    unawaited(_changeService(action));
                  }
                },
        ),
      ),
      if (state.canControl)
        DesignRow(
          title: '后台运行状态',
          subtitle: state.running ? '正在运行' : '已停止',
          trailing: OutlinedButton(
            style: AppButtonStyles.inlineAction(context),
            onPressed: _busy
                ? null
                : () => _changeService(state.running ? 'stop' : 'start'),
            child: Text(state.running ? '停止后台' : '启动后台'),
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

  Widget _advancedServiceRows() {
    final state = _service;
    if (state == null) return const SizedBox.shrink();
    final actions = <String, String>{
      if (state.recoveryBlocked) ...{
        if (state.canDiscardDamaged) 'discard-damaged': '清除损坏的切换记录',
      } else if (state.recoveryPending) ...{
        'recover': '恢复原管理方式',
        if (state.canDiscardReplacement) 'discard': '放弃恢复并解绑',
      } else if (state.directory != null) ...{
        if (state.available && !state.matchesInstallation) 'replace': '更换为本安装',
        if (state.canControl && state.running) 'restart': '重启后台',
        if (state.canControl) 'unregister': '解除当前安装登记',
      },
    };
    if (actions.isEmpty && _serviceError == null) {
      return const SizedBox.shrink();
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const SizedBox(height: 12),
        DesignGroup(
          children: [
            if (_serviceError case final error?)
              DesignRow(title: '后台服务', subtitle: error),
            for (final MapEntry(:key, :value) in actions.entries)
              DesignRow(
                title: value,
                onTap: _busy ? null : () => _changeService(key),
              ),
          ],
        ),
      ],
    );
  }

  @override
  void dispose() {
    if (widget.manager == null) unawaited(_manager.close());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (widget.advancedOnly) return _advancedServiceRows();
    final status = _status;
    final service = _service;
    final serviceUnknown = service == null && _serviceError != null;
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
      title: '自动启动',
      subtitle:
          _error ??
          (status?.autostart == 'conflict'
                  ? status?.autostartNotice ?? '自启登记存在冲突'
                  : (status?.autostartNotice ??
                        (status?.daemonAutostartSupported == false
                            ? '当前安装不支持仅后台自启'
                            : '启用前，建议关闭其他认证器及其开机自启'))) +
              (status != null && !status.guiAvailable
                  ? '\n当前安装没有可启动的图形界面'
                  : ''),
      trailing: SettingsDropdown<String>(
        frameKey: const ValueKey('autostart-frame'),
        dropdownKey: const ValueKey('autostart-selector'),
        value:
            status == null ||
                status.autostart == 'conflict' ||
                status.autostartNeedsRepair
            ? null
            : status.autostart,
        hint: status?.autostartNeedsRepair == true ? '自启待确认' : unknownState,
        labels: const {'disabled': '关闭', 'gui': '图形界面和后台', 'daemon': '仅后台'},
        disabledValues: {
          if (serviceUnknown) ...const {'disabled', 'gui', 'daemon'},
          if (!(status?.guiAvailable ?? false)) 'gui',
          if (status?.daemonAutostartSupported == false ||
              (service == null && _serviceError != null))
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
      if (service != null &&
          (service.available ||
              !service.registrationKnown ||
              service.directory != null))
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
    final manager = _manager;
    final checker = manager is PackageUpdateChecker
        ? manager as PackageUpdateChecker
        : null;
    final update = checker?.supportsUpdateCheck == true
        ? UpdateSettings(checker: checker!)
        : null;
    return widget.builder?.call(startupRows, withRetry(pathRow), update) ??
        DesignGroup(children: [...startupRows, withRetry(pathRow)]);
  }
}
