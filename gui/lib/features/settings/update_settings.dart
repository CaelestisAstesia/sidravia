import 'package:flutter/material.dart';
import 'package:sidravia_gui/platform/integration_failure.dart';
import 'package:sidravia_gui/platform/update/package_update_checker.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

class UpdateSettings extends StatefulWidget {
  const UpdateSettings({super.key, required this.checker});

  final PackageUpdateChecker checker;

  @override
  State<UpdateSettings> createState() => _UpdateSettingsState();
}

class _UpdateSettingsState extends State<UpdateSettings> {
  PackageUpdate? _result;
  String? _status;
  bool _checking = false;
  bool _installing = false;

  Future<void> _check() async {
    if (_checking || _installing) return;
    setState(() {
      _checking = true;
      _result = null;
      _status = null;
    });
    final checker = widget.checker;
    try {
      final result = await checker.checkUpdate();
      if (!mounted || !identical(checker, widget.checker)) return;
      setState(() => _result = result);
      final version = result.version;
      if (result.state == UpdateCheckState.available && version != null) {
        final candidateId = checker.usesSystemPackageManager
            ? null
            : result.installCandidateId;
        final confirmed = await _confirm(
          version,
          result.notes,
          candidateId != null,
        );
        if (confirmed != true ||
            !mounted ||
            !identical(checker, widget.checker)) {
          return;
        }
        if (candidateId == null) {
          await _openRelease(version);
        } else {
          await _install(candidateId);
        }
      }
    } on IntegrationFailure catch (error) {
      if (mounted) setState(() => _status = error.message);
    } finally {
      if (mounted) setState(() => _checking = false);
    }
  }

  Future<bool?> _confirm(String version, String notes, bool canInstall) =>
      showDialog<bool>(
        context: context,
        builder: (dialogContext) => DesignConfirmationDialog(
          title: Text(version),
          content: SizedBox(
            width: 420,
            child: ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.sizeOf(dialogContext).height * .5,
              ),
              child: SingleChildScrollView(
                child: Text(notes.trim().isEmpty ? '暂无更新日志' : notes),
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialogContext, false),
              child: const Text('取消'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(dialogContext, true),
              child: Text(canInstall ? '下载并安装更新' : '查看发行与下载'),
            ),
          ],
        ),
      );

  Future<void> _install(String candidateId) async {
    setState(() => _installing = true);
    try {
      await widget.checker.installUpdate(candidateId);
      if (!mounted) return;
      setState(() {
        _result = null;
        _status = '更新器已接管，请等待更新完成';
      });
    } finally {
      if (mounted) setState(() => _installing = false);
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
    final message = _installing
        ? '正在下载并安装更新…'
        : _status ??
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
            style: AppButtonStyles.inlineAction(context),
            onPressed: _checking || _installing ? null : _check,
            child: const Text('检查更新'),
          ),
        ),
        if (!_checking && !_installing && version != null) ...[
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
