part of 'interactive_demo_main.dart';

/// In-memory visual preview; never invokes system integration tools.
class _OfflineInstallationManager implements InstallationManager {
  bool _pathEnabled = false;
  String _autostart = 'disabled';

  @override
  Future<IntegrationStatus> status() async => IntegrationStatus(
    pathEnabled: _pathEnabled,
    autostart: _autostart,
    guiAvailable: true,
  );

  @override
  Future<void> change(IntegrationChange change) async {
    switch (change) {
      case IntegrationChange.pathEnable:
        _pathEnabled = true;
      case IntegrationChange.pathDisable:
        _pathEnabled = false;
      case IntegrationChange.startGui:
        _autostart = 'gui';
      case IntegrationChange.startDaemon:
        _autostart = 'daemon';
      case IntegrationChange.startDisable:
        _autostart = 'disabled';
    }
  }

  @override
  Future<void> close() async {}
}

class _OfflineAnnouncementWindow extends SchoolAnnouncementWindow {
  _OfflineAnnouncementWindow(this.navigator) : super.disabled();
  final GlobalKey<NavigatorState> navigator;
  @override
  Future<SchoolAnnouncementOpenResult> open(
    AnnouncementMetadata metadata, {
    String namespace = '',
  }) async {
    final context = navigator.currentContext;
    if (context == null) return SchoolAnnouncementOpenResult.failed;
    unawaited(
      showDialog<void>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('离线公告占位预览'),
          content: const Text('这是离线演示占位；真实学校公告将在独立应用内窗口显示。未访问学校网页。'),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('关闭'),
            ),
          ],
        ),
      ),
    );
    return SchoolAnnouncementOpenResult.opened;
  }
}
