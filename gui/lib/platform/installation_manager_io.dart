import 'dart:io';

import 'installation_manager.dart';
import 'linux_installation_manager.dart';
import 'installation_manager_stub.dart' as stub;
import 'update_check_process.dart';
import 'windows_installation_manager.dart';

InstallationManager createManager({UpdateCheckProcess? updateCheck}) =>
    Platform.isWindows
    ? WindowsInstallationManager(updateCheck ?? UpdateCheckProcess())
    : Platform.isLinux
    ? LinuxInstallationManager(
        executable: Platform.resolvedExecutable,
        environment: Platform.environment,
        updateCheck: updateCheck,
      )
    : stub.createManager();
