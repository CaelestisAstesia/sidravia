import 'package:flutter/material.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';

class SidraviaApp extends StatelessWidget {
  const SidraviaApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Sidravia',
      debugShowCheckedModeBanner: false,
      theme: SidraviaTheme.light(),
      home: const SidraviaShell(),
    );
  }
}
