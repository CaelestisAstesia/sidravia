import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';

void main() {
  testWidgets('disabled announcements do not add an entry', (tester) async {
    final controller = AnnouncementController.disabled();
    await tester.pumpWidget(
      MaterialApp(
        home: AnnouncementEntry(controller: controller, onOpen: () {}),
      ),
    );
    expect(find.text('校园网公告'), findsNothing);
    controller.dispose();
  });
}
