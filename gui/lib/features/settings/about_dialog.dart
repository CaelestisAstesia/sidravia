import 'package:flutter/material.dart';
import 'package:sidravia_gui/features/settings/license_browser.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

const _projectAddress = 'https://github.com/CaelestisAstesia/sidravia';
const _windowChannel = MethodChannel('sidravia/window');

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
  ScaffoldMessenger.of(context)
      .showSnackBar(const SnackBar(content: Text('无法打开浏览器，请选择并复制项目地址后访问。')));
}

Future<void> showSidraviaAbout(
  BuildContext context, {
  required String version,
}) => showDialog<void>(
  context: context,
  builder: (context) => DesignDialog.information(
    title: '关于 Sidravia',
    actions: [
      TextButton(
        onPressed: () => _showLicenses(context),
        child: const Text('许可'),
      ),
    ],
    body: SingleChildScrollView(
      child: DesignGroup(
        children: [
          DesignRow(title: '版本', value: version),
          const DesignRow(title: '项目', subtitle: 'Sidravia 网络认证客户端'),
          Padding(
            padding: const EdgeInsets.all(12),
            child: MouseRegion(
              cursor: SystemMouseCursors.click,
              child: Semantics(
                link: true,
                child: SelectableText(
                  _projectAddress,
                  onTap: () => _openProjectPage(context),
                  style: TextStyle(
                    fontSize: 12,
                    color: Theme.of(context).colorScheme.primary,
                    decoration: TextDecoration.underline,
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    ),
  ),
);

Future<void> _showLicenses(BuildContext context) => showDialog<void>(
  context: context,
  builder: (context) => const LicenseBrowser(),
);
