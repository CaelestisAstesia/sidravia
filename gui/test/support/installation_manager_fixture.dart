import 'dart:async';

import 'package:sidravia_gui/platform/installation/installation_manager.dart';

class FakeManager implements InstallationManager {
  int reads = 0;
  bool closed = false, path = false;
  IntegrationChange? lastChange;
  Completer<IntegrationStatus>? pending;
  bool fail = false, guiAvailable = true;
  @override
  Future<IntegrationStatus> status() async {
    reads++;
    if (fail) throw const IntegrationFailure('读取失败');
    if (pending != null) return pending!.future;
    return IntegrationStatus(
      pathEnabled: path,
      autostart: 'disabled',
      guiAvailable: guiAvailable,
    );
  }

  @override
  Future<void> change(IntegrationChange change) async {
    lastChange = change;
    path = change == IntegrationChange.pathEnable;
  }

  @override
  Future<void> close() async {
    closed = true;
  }
}
