import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/settings/gui_settings_storage_io.dart';
import 'package:sidravia_gui/platform/paths/linux_product_executable.dart';

void main() {
  group('Linux product paths', () {
    test(
      'packaged runtime resolves public launcher beside portable user data',
      () {
        expect(
          linuxProductExecutable(
            '/tmp/app with spaces/gui-runtime/sidravia_gui',
          ),
          '/tmp/app with spaces/sidravia_gui',
        );
        expect(
          linuxProductExecutable(
            '/usr/lib64/sidravia/gui-runtime/sidravia_gui',
          ),
          '/usr/lib64/sidravia/sidravia_gui',
        );
        expect(
          linuxProductExecutable('/tmp/bundle/sidravia_gui'),
          '/tmp/bundle/sidravia_gui',
        );
      },
    );

    test('packaged settings use root marker and existing namespace data', () {
      final inspected = <String>[];
      final location = resolveLinuxGuiSettings(
        executable: '/tmp/app/gui-runtime/sidravia_gui',
        environment: {'HOME': '/tmp/home'},
        namespace: 'fixture',
        inspectMarker: (path) {
          inspected.add(path);
          return FileSystemEntityType.file;
        },
      );
      expect(inspected, ['/tmp/app/sidravia.portable']);
      expect(location.path, '/tmp/app/namespaces/fixture/gui-settings.json');
    });
  }, skip: !Platform.isLinux);
}
