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
  final Widget Function(Widget autostart, Widget path)? builder;
  @override
  State<IntegrationSettings> createState() => _IntegrationSettingsState();
}

class _IntegrationSettingsState extends State<IntegrationSettings> {
  late final InstallationManager _manager;
  IntegrationStatus? _status;
  String? _error;
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
    try {
      if (change != null) await _manager.change(change);
      final next = await _manager.status();
      if (mounted) setState(() => _status = next);
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

  @override
  void dispose() {
    unawaited(_manager.close());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final status = _status;
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
      subtitle: _error ?? '使新打开的终端可以直接使用命令',
      trailing: status == null
          ? Text(unknownState)
          : DesignSwitch(
              label: '加入用户 PATH',
              value: status.pathEnabled,
              onChanged: _busy
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
              : '启用前，建议关闭其他认证器及其开机自启'),
      trailing: SettingsDropdown<String>(
        frameKey: const ValueKey('autostart-frame'),
        dropdownKey: const ValueKey('autostart-selector'),
        value: status == null || status.autostart == 'conflict'
            ? null
            : status.autostart,
        hint: unknownState,
        labels: const {'disabled': '关闭', 'gui': '启动到托盘', 'daemon': '仅后台'},
        disabledValues: {if (!(status?.guiAvailable ?? false)) 'gui'},
        onChanged: _busy || status == null || status.autostart == 'conflict'
            ? null
            : (mode) => _perform(switch (mode) {
                'gui' => IntegrationChange.startGui,
                'daemon' => IntegrationChange.startDaemon,
                _ => IntegrationChange.startDisable,
              }),
      ),
    );
    return widget.builder?.call(withRetry(autostartRow), withRetry(pathRow)) ??
        DesignGroup(children: [withRetry(autostartRow), withRetry(pathRow)]);
  }
}
