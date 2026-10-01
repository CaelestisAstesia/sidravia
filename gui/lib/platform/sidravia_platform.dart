import 'dart:io';

import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';

enum SidraviaPlatformKind { windows, linux, android, ios, other }

/// The small composition boundary between shared Flutter application code and
/// operating-system behavior.
///
/// The first version keeps Windows behavior intact. Other targets receive
/// explicit adapters so adding mobile or Linux support later does not require
/// changing GuiController, IPC models, or feature pages.
class SidraviaPlatform {
  const SidraviaPlatform({
    required this.kind,
    required this.bootstrapper,
    required this.desktopPresence,
  });

  final SidraviaPlatformKind kind;
  final GuiBootstrapper bootstrapper;
  final DesktopPresence desktopPresence;

  bool get supportsDesktopPresence => kind == SidraviaPlatformKind.windows;

  bool get supportsLocalBootstrap =>
      kind == SidraviaPlatformKind.windows &&
      bootstrapper is ProcessGuiBootstrap;

  factory SidraviaPlatform.detect({String namespace = ''}) {
    if (Platform.isWindows) {
      return SidraviaPlatform(
        kind: SidraviaPlatformKind.windows,
        bootstrapper: ProcessGuiBootstrap(),
        desktopPresence: DesktopPresence(namespace: namespace),
      );
    }
    if (Platform.isLinux) {
      return SidraviaPlatform(
        kind: SidraviaPlatformKind.linux,
        bootstrapper: const UnsupportedGuiBootstrapper(),
        desktopPresence: DesktopPresence.disabled(),
      );
    }
    if (Platform.isAndroid) {
      return SidraviaPlatform(
        kind: SidraviaPlatformKind.android,
        bootstrapper: const UnsupportedGuiBootstrapper(),
        desktopPresence: DesktopPresence.disabled(),
      );
    }
    if (Platform.isIOS) {
      return SidraviaPlatform(
        kind: SidraviaPlatformKind.ios,
        bootstrapper: const UnsupportedGuiBootstrapper(),
        desktopPresence: DesktopPresence.disabled(),
      );
    }
    return SidraviaPlatform(
      kind: SidraviaPlatformKind.other,
      bootstrapper: const UnsupportedGuiBootstrapper(),
      desktopPresence: DesktopPresence.disabled(),
    );
  }

  /// Creates deterministic platform bundles for unit tests without changing
  /// the host platform detected by dart:io.
  factory SidraviaPlatform.forKind(SidraviaPlatformKind kind) {
    if (kind == SidraviaPlatformKind.windows) {
      return SidraviaPlatform(
        kind: SidraviaPlatformKind.windows,
        bootstrapper: const UnsupportedGuiBootstrapper(),
        desktopPresence: DesktopPresence.disabled(),
      );
    }
    return SidraviaPlatform(
      kind: kind,
      bootstrapper: const UnsupportedGuiBootstrapper(),
      desktopPresence: DesktopPresence.disabled(),
    );
  }
}
