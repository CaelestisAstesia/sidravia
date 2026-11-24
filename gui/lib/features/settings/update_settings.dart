import 'package:flutter/material.dart';
import 'package:sidravia_gui/platform/installation/installation_manager.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

class UpdateSettings extends StatefulWidget {
  const UpdateSettings({super.key, required this.checker});

  final PackageUpdateChecker checker;

  @override
  State<UpdateSettings> createState() => _UpdateSettingsState();
}

class _UpdateSettingsState extends State<UpdateSettings> {
  PackageUpdate? _result;
  String? _error;
  bool _checking = false;

  Future<void> _check() async {
    if (_checking) return;
    setState(() {
      _checking = true;
      _result = null;
      _error = null;
    });
    try {
      final result = await widget.checker.checkUpdate();
      if (mounted) setState(() => _result = result);
    } on IntegrationFailure catch (error) {
      if (mounted) setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _checking = false);
    }
  }

  Future<void> _openRelease(String version) async {
    try {
      await widget.checker.openReleasePage(version);
    } on IntegrationFailure catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(error.message)));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final result = _result;
    final version = result?.state == UpdateCheckState.available
        ? result?.version
        : null;
    final message =
        _error ??
        switch (result?.state) {
          UpdateCheckState.available => '发现新版本 ${result?.version}',
          UpdateCheckState.upToDate => '当前已是最新版本',
          UpdateCheckState.aheadOfChannel => '当前版本高于此通道已发布的版本',
          UpdateCheckState.noCompatibleRelease => '未找到适用于此安装的发行包',
          null =>
            _checking
                ? '正在检查…'
                : widget.checker.usesSystemPackageManager
                ? '通过系统包管理器安装更新'
                : null,
        };
    return Column(
      children: [
        DesignRow(
          title: '软件更新',
          subtitle: message,
          trailing: OutlinedButton(
            onPressed: _checking ? null : _check,
            child: const Text('检查更新'),
          ),
        ),
        if (version != null) ...[
          Divider(
            height: 1,
            thickness: 1,
            color: Theme.of(context).dividerColor,
          ),
          DesignRow(title: '查看发行与下载', onTap: () => _openRelease(version)),
        ],
      ],
    );
  }
}
