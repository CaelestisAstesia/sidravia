import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/platform/sidravia_platform.dart';

void main() {
  test('future mobile targets use explicit unsupported adapters', () async {
    for (final kind in [
      SidraviaPlatformKind.android,
      SidraviaPlatformKind.ios,
    ]) {
      final platform = SidraviaPlatform.forKind(kind);

      expect(platform.kind, kind);
      expect(platform.supportsDesktopPresence, isFalse);
      expect(platform.supportsLocalBootstrap, isFalse);
      expect(
        (await platform.bootstrapper.bootstrap()).failure?.code,
        'unsupported_platform',
      );
      expect(platform.desktopPresence.enabled, isFalse);
    }
  });

  test(
    'unsupported bootstrapper preserves the stable failure contract',
    () async {
      const bootstrapper = UnsupportedGuiBootstrapper();

      final result = await bootstrapper.bootstrap();

      expect(result.isSuccess, isFalse);
      expect(result.failure, GuiBootstrapFailure.unsupportedPlatform);
    },
  );

  test('platform kind has an explicit Linux boundary', () {
    final platform = SidraviaPlatform.forKind(SidraviaPlatformKind.linux);

    expect(platform.kind, SidraviaPlatformKind.linux);
    expect(platform.supportsDesktopPresence, isFalse);
    expect(platform.supportsLocalBootstrap, isFalse);
  });
}
