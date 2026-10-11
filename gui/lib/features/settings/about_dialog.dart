import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

Future<void> showSidraviaAbout(
  BuildContext context, {
  required String version,
}) => showDialog<void>(
  context: context,
  builder: (context) => _InfoDialog(
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
          const DesignRow(title: '项目', subtitle: 'Sidravia 校园网客户端'),
          const Padding(
            padding: EdgeInsets.all(12),
            child: SelectableText(
              'https://github.com/CaelestisAstesia/sidravia',
              style: TextStyle(fontSize: 12),
            ),
          ),
        ],
      ),
    ),
  ),
);

Future<void> _showLicenses(BuildContext context) => showDialog<void>(
  context: context,
  builder: (context) => _InfoDialog(
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
                builder: (context) => _InfoDialog(
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

class _InfoDialog extends StatelessWidget {
  const _InfoDialog({
    required this.title,
    required this.body,
    this.actions = const [],
  });
  final String title;
  final Widget body;
  final List<Widget> actions;
  @override
  Widget build(BuildContext context) => Dialog(
    child: SizedBox(
      width: 520,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * .8,
        ),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              DesignHeader(
                title: title,
                actions: actions,
                onBack: () => Navigator.pop(context),
                bottomSpacing: 16,
              ),
              Flexible(child: body),
            ],
          ),
        ),
      ),
    ),
  );
}
