import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'Linux uses the accepted 256px application icon embedded in Windows ICO',
    () {
      final ico = File('windows/runner/resources/app_icon.ico')
          .readAsBytesSync();
      final directory = ByteData.sublistView(ico);
      expect(directory.getUint16(2, Endian.little), 1);
      final count = directory.getUint16(4, Endian.little);
      Uint8List? canonical;
      for (var i = 0; i < count; i++) {
        final entry = 6 + i * 16;
        if (ico[entry] == 0 && ico[entry + 1] == 0) {
          final length = directory.getUint32(entry + 8, Endian.little);
          final offset = directory.getUint32(entry + 12, Endian.little);
          canonical = Uint8List.sublistView(ico, offset, offset + length);
        }
      }
      expect(canonical, isNotNull);
      expect(canonical!.take(8), [137, 80, 78, 71, 13, 10, 26, 10]);
      expect(
        File('linux/runner/resources/sidravia.png').readAsBytesSync(),
        canonical,
      );
    },
  );
}
