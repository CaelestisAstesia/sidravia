import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/platform/installation_manager.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

class IntegrationSettings extends StatefulWidget {
  const IntegrationSettings({super.key, this.manager});
  final InstallationManager? manager;
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
    _manager = widget.manager ?? createInstallationManager();
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
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const DesignSectionTitle('系统集成'),
        DesignGroup(
          children: [
            DesignRow(
              title: '系统集成状态',
              subtitle:
                  _error ??
                  (_busy
                      ? '正在读取或修改…'
                      : status == null
                      ? '状态未知'
                      : '当前用户设置'),
              trailing: TextButton(
                onPressed: _busy ? null : () => _perform(),
                child: const Text('刷新'),
              ),
            ),
            DesignRow(
              title: '加入用户 PATH',
              subtitle: '使新打开的终端可以直接使用命令',
              trailing: DesignSwitch(
                label: '加入用户 PATH',
                value: status?.pathEnabled ?? false,
                onChanged: _busy || status == null
                    ? null
                    : (enabled) => _perform(
                        enabled
                            ? IntegrationChange.pathEnable
                            : IntegrationChange.pathDisable,
                      ),
              ),
            ),
            DesignRow(
              title: '开机自启',
              subtitle: status?.autostart == 'conflict'
                  ? '任务存在冲突，未修改'
                  : '修改将在下次登录时生效',
              trailing: DropdownButtonHideUnderline(
                child: DropdownButton<String>(
                  value: status == null || status.autostart == 'conflict'
                      ? null
                      : status.autostart,
                  hint: const Text('状态未知'),
                  items: [
                    const DropdownMenuItem(
                      value: 'disabled',
                      child: Text('关闭'),
                    ),
                    DropdownMenuItem(
                      value: 'gui',
                      enabled: status?.guiAvailable ?? false,
                      child: const Text('GUI 托盘'),
                    ),
                    const DropdownMenuItem(value: 'daemon', child: Text('仅后台')),
                  ],
                  onChanged:
                      _busy || status == null || status.autostart == 'conflict'
                      ? null
                      : (mode) => _perform(switch (mode) {
                          'gui' => IntegrationChange.startGui,
                          'daemon' => IntegrationChange.startDaemon,
                          _ => IntegrationChange.startDisable,
                        }),
                ),
              ),
            ),
          ],
        ),
        const Padding(
          padding: EdgeInsets.only(top: 8),
          child: Text('其他认证器可能与 Sidravia 冲突。启用自启前，建议关闭其他认证器的开机自启并停止运行。'),
        ),
        const SizedBox(height: 24),
      ],
    );
  }
}
