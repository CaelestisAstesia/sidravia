import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
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
  builder: (context) => DesignDialog.information(
    title: '开源许可',
    body: FutureBuilder<List<LicenseEntry>>(
      future: LicenseRegistry.licenses.toList(),
      builder: (context, snapshot) {
        if (snapshot.hasError) return const Center(child: Text('许可加载失败'));
        final entries = snapshot.data;
        if (entries == null) {
          return const Center(child: CircularProgressIndicator());
        }
        return ListView.builder(
          shrinkWrap: true,
          itemCount: entries.length,
          itemBuilder: (context, index) {
            final entry = entries[index];
            return ListTile(
              title: Text(
                entry.packages.join(', '),
                style: const TextStyle(fontSize: 13),
              ),
              trailing: const Icon(Icons.chevron_right, size: 18),
              onTap: () => showDialog<void>(
                context: context,
                builder: (context) => DesignDialog.information(
                  title: entry.packages.join(', '),
                  body: SingleChildScrollView(
                    child: SelectableText(
                      entry.paragraphs.map((p) => p.text).join('\n\n'),
                      style: const TextStyle(fontSize: 12, height: 1.5),
                    ),
                  ),
                ),
              ),
            );
          },
        );
      },
    ),
  ),
);
