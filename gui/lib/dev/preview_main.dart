import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';
import 'package:sidravia_gui/dev/preview_fixtures.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';

void main() => runApp(const PreviewCatalog());

enum PreviewSection {
  home('主页', Icons.home_outlined),
  configuration('配置', Icons.manage_accounts_outlined),
  settings('设置', Icons.settings_outlined);

  const PreviewSection(this.label, this.icon);
  final String label;
  final IconData icon;
}

class PreviewCatalog extends StatefulWidget {
  const PreviewCatalog({
    super.key,
    this.initialScenario = PreviewScenario.authenticated,
    this.initialSection = PreviewSection.home,
    this.showToolbar = true,
  });

  final PreviewScenario initialScenario;
  final PreviewSection initialSection;
  final bool showToolbar;

  @override
  State<PreviewCatalog> createState() => _PreviewCatalogState();
}

class _PreviewCatalogState extends State<PreviewCatalog> {
  late PreviewScenario _scenario = widget.initialScenario;
  late PreviewSection _section = widget.initialSection;
  late GuiController _controller = _createController(_scenario);

  GuiController _createController(PreviewScenario scenario) {
    final controller = createPreviewController(scenario);
    unawaited(controller.start());
    return controller;
  }

  void _selectScenario(PreviewScenario? scenario) {
    if (scenario == null || scenario == _scenario) return;
    final old = _controller;
    setState(() {
      _scenario = scenario;
      _controller = _createController(scenario);
    });
    old.dispose();
  }

  void _selectSection(PreviewSection section) {
    setState(() => _section = section);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Sidravia visual preview',
      debugShowCheckedModeBanner: false,
      theme: SidraviaTheme.light(),
      home: Scaffold(
        body: SafeArea(
          child: Column(
            children: [
              if (widget.showToolbar) _buildToolbar(),
              Expanded(child: _buildSurface()),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildToolbar() {
    return SizedBox(
      width: double.infinity,
      child: Material(
        color: const Color(0xFFF0F2F6),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 10),
          child: Wrap(
            spacing: 20,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              const Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(Icons.visibility_outlined, size: 20),
                  SizedBox(width: 8),
                  Text('开发预览 · 不连接 daemon'),
                ],
              ),
              DropdownButton<PreviewScenario>(
                key: const ValueKey('preview-scenario'),
                value: _scenario,
                underline: const SizedBox.shrink(),
                onChanged: _selectScenario,
                items: [
                  for (final scenario in PreviewScenario.values)
                    DropdownMenuItem(
                      value: scenario,
                      child: Text(scenario.label),
                    ),
                ],
              ),
              SegmentedButton<PreviewSection>(
                key: const ValueKey('preview-section'),
                segments: [
                  for (final section in PreviewSection.values)
                    ButtonSegment(
                      value: section,
                      icon: Icon(section.icon),
                      label: Text(section.label),
                    ),
                ],
                selected: {_section},
                showSelectedIcon: false,
                onSelectionChanged: (selection) =>
                    _selectSection(selection.single),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildSurface() => switch (_section) {
    PreviewSection.home => HomePage(
      controller: _controller,
      onOpenConfiguration: () => _selectSection(PreviewSection.configuration),
    ),
    PreviewSection.configuration => ConfigurationPage(controller: _controller),
    PreviewSection.settings => const SettingsPage(),
  };
}
