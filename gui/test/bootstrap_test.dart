import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';

void main() {
  const valid =
      '{"schemaVersion":1,"endpoint":"ws://127.0.0.1:4711/ipc","token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.2.0-dev","buildId":"fixture","daemonPid":42,"mode":"desktop"}';

  test('strictly decodes the machine bootstrap success document', () {
    final result = decodeGuiBootstrap(valid);

    expect(result.isSuccess, isTrue);
    expect(result.value?.endpoint.toString(), 'ws://127.0.0.1:4711/ipc');
    expect(result.value?.daemonPid, 42);
  });

  test(
    'rejects extra fields, a non-loopback endpoint and an invalid token',
    () {
      expect(
        decodeGuiBootstrap(valid.replaceFirst('}', ',"extra":true}')).isSuccess,
        isFalse,
      );
      expect(
        decodeGuiBootstrap(valid.replaceFirst('127.0.0.1', 'example.test'))
            .isSuccess,
        isFalse,
      );
      expect(
        decodeGuiBootstrap(valid.replaceFirst('0123', 'ABCD')).isSuccess,
        isFalse,
      );
    },
  );

  test(
    'non-Windows bootstrap returns only the stable Unsupported state',
    () async {
      if (Platform.isWindows) return;
      final result = await ProcessGuiBootstrap().bootstrap();

      expect(result.failure, isNotNull);
      expect(result.failure?.code, 'unsupported_platform');
    },
  );
}
