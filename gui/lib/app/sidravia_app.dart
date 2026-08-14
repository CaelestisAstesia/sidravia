import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';

class SidraviaApp extends StatefulWidget {
  const SidraviaApp({super.key, required this.controller});

  final GuiController controller;

  @override
  State<SidraviaApp> createState() => _SidraviaAppState();
}

class _SidraviaAppState extends State<SidraviaApp> {
  @override
  void initState() {
    super.initState();
    widget.controller.start();
  }

  @override
  void dispose() {
    widget.controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Sidravia',
      debugShowCheckedModeBanner: false,
      theme: SidraviaTheme.light(),
      home: SidraviaShell(controller: widget.controller),
    );
  }
}
