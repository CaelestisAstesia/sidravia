import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/features/settings/license_browser.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

const _projectAddress = 'https://github.com/CaelestisAstesia/sidravia';
const _windowChannel = MethodChannel('sidravia/window');

List<Widget> projectSettingsRows(
  BuildContext context, {
  required String version,
}) => [
  DesignRow(title: '软件版本', value: version),
  DesignRow(
    title: '许可信息',
    onTap: () => showDialog<void>(
      context: context,
      builder: (_) => const LicenseBrowser(),
    ),
  ),
  DesignRow(
    title: '项目地址',
    trailing: Icon(
      Icons.open_in_new,
      size: 18,
      color: Theme.of(context).colorScheme.onSurfaceVariant,
    ),
    onTap: () => _openProjectPage(context),
  ),
];

Future<void> _openProjectPage(BuildContext context) async {
  try {
    await _windowChannel.invokeMethod<void>('openProjectPage');
  } on PlatformException {
    if (context.mounted) _showProjectOpenFailure(context);
  } on MissingPluginException {
    if (context.mounted) _showProjectOpenFailure(context);
  }
}

void _showProjectOpenFailure(BuildContext context) {
  ScaffoldMessenger.of(context).showSnackBar(
    const SnackBar(content: SelectableText('无法打开浏览器\n$_projectAddress')),
  );
}
