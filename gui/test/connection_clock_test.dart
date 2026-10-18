import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/connection_clock.dart';

void main() {
  testWidgets(
    'ticks at authentication-relative minute; hidden clock sleeps and resume is fresh',
    (tester) async {
      final at = DateTime.utc(2026, 10, 18, 8, 0, 37);
      var now = at.add(const Duration(seconds: 50));
      final clock = ConnectionClock(now: () => now);
      var ticks = 0;
      clock.addListener(() => ticks++);
      clock.update(at);
      now = now.add(const Duration(seconds: 9));
      await tester.pump(const Duration(seconds: 9));
      expect(ticks, 0);
      now = now.add(const Duration(seconds: 1));
      await tester.pump(const Duration(seconds: 1));
      expect(ticks, 1);
      clock.update(at, active: false);
      now = now.add(const Duration(minutes: 5));
      await tester.pump(const Duration(minutes: 5));
      expect(ticks, 1);
      clock.update(at, active: true);
      expect(clock.now.difference(at).inMinutes, 6);
      expect(ticks, 2);
      clock.dispose();
      await tester.pump(const Duration(minutes: 1));
      expect(ticks, 2);
    },
  );
}
