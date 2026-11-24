import 'package:flutter_test/flutter_test.dart';

/// Existing credential contract tests enter the explicit new-password editor.
Future<void> openPasswordEditor(WidgetTester tester) async {
  final change = find.text('更改');
  if (change.evaluate().isNotEmpty) {
    await tester.ensureVisible(change);
    await tester.tap(change);
    await tester.pumpAndSettle();
  }
}

/// Accepts the existing save confirmation when a scenario requires it.
Future<bool> tapConfigurationSaveConfirmation(WidgetTester tester) async {
  if (find.text('保存连接配置？').evaluate().isEmpty) {
    return false;
  }
  final disconnect = find.text('保存并断开');
  final stop = find.text('保存并停止');
  final confirm = disconnect.evaluate().isNotEmpty
      ? disconnect
      : stop.evaluate().isNotEmpty
      ? stop
      : find.text('保存');
  await tester.tap(confirm);
  return true;
}
