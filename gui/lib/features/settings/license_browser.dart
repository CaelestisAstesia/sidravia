import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/shared/widgets/design_search_field.dart';

class LicenseBrowser extends StatefulWidget {
  const LicenseBrowser({super.key});
  @override
  State<LicenseBrowser> createState() => _LicenseBrowserState();
}

class _LicenseBrowserState extends State<LicenseBrowser> {
  final _query = TextEditingController();
  late Future<Map<String, List<LicenseEntry>>> _licenses = _load();

  Future<Map<String, List<LicenseEntry>>> _load() async {
    final groups = <String, List<LicenseEntry>>{};
    await for (final entry in LicenseRegistry.licenses) {
      final packages = entry.packages.toList();
      for (final package in packages.isEmpty ? ['其他组件'] : packages.toSet()) {
        (groups[package] ??= []).add(entry);
      }
    }
    return groups;
  }

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => DesignDialog.information(
    title: '开源许可',
    body: SizedBox(
      height: 420,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          DesignSearchField(
            controller: _query,
            hint: '查找组件',
            onChanged: (_) => setState(() {}),
          ),
          const SizedBox(height: 12),
          Flexible(
            child: FutureBuilder<Map<String, List<LicenseEntry>>>(
              future: _licenses,
              builder: (context, snapshot) {
                if (snapshot.hasError) {
                  return Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Text('许可加载失败'),
                      TextButton(
                        onPressed: () => setState(() => _licenses = _load()),
                        child: const Text('重试'),
                      ),
                    ],
                  );
                }
                final groups = snapshot.data;
                if (groups == null) {
                  return const Center(child: CircularProgressIndicator());
                }
                final query = _query.text.trim().toLowerCase();
                final names =
                    groups.keys
                        .where((name) => name.toLowerCase().contains(query))
                        .toList()
                      ..sort((a, b) {
                        final result = a.toLowerCase().compareTo(
                          b.toLowerCase(),
                        );
                        return result == 0 ? a.compareTo(b) : result;
                      });
                if (names.isEmpty) {
                  return Padding(
                    padding: const EdgeInsets.all(16),
                    child: Text(
                      groups.isEmpty ? '暂无许可信息' : '未找到匹配组件',
                      style: TextStyle(
                        fontSize: 12,
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                  );
                }
                return ListView.builder(
                  // Reset to the top of a new query instead of leaving its results offscreen.
                  key: ValueKey(query),
                  shrinkWrap: true,
                  itemCount: names.length,
                  itemBuilder: (context, index) {
                    final name = names[index];
                    final entries = groups[name]!;
                    return ListTile(
                      title: Text(name),
                      subtitle: Text('${entries.length} 份许可'),
                      trailing: const Icon(Icons.chevron_right, size: 18),
                      onTap: () => showDialog<void>(
                        context: context,
                        builder: (_) =>
                            _LicenseDetails(name: name, entries: entries),
                      ),
                    );
                  },
                );
              },
            ),
          ),
        ],
      ),
    ),
  );
}

class _LicenseDetails extends StatelessWidget {
  const _LicenseDetails({required this.name, required this.entries});
  final String name;
  final List<LicenseEntry> entries;
  @override
  Widget build(BuildContext context) => DesignDialog.information(
    title: name,
    body: SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (var i = 0; i < entries.length; i++) ...[
            if (i > 0)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 16),
                child: Divider(height: 1),
              ),
            DesignSectionTitle('许可 ${i + 1}'),
            if (entries[i].packages.length > 1) ...[
              Text(
                '适用组件 · ${entries[i].packages.join(', ')}',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 12),
            ],
            // Keep every original paragraph and its indentation; shared entries
            // appear under each associated component, without deduplicating terms.
            for (final paragraph in entries[i].paragraphs)
              Padding(
                padding: EdgeInsets.only(
                  left: paragraph.indent > 0 ? paragraph.indent * 12.0 : 0,
                  bottom: 12,
                ),
                child: SelectableText(
                  paragraph.text,
                  textAlign: paragraph.indent == LicenseParagraph.centeredIndent
                      ? TextAlign.center
                      : TextAlign.start,
                  style: const TextStyle(fontSize: 12, height: 1.5),
                ),
              ),
          ],
        ],
      ),
    ),
  );
}
