import 'installation_manager.dart';

InstallationManager createManager() => _UnsupportedManager();

class _UnsupportedManager implements InstallationManager {
  @override
  Future<IntegrationStatus> status() async =>
      throw const IntegrationFailure('当前平台尚不支持系统集成管理。');
  @override
  Future<void> change(IntegrationChange change) async =>
      throw const IntegrationFailure('当前平台尚不支持系统集成管理。');
  @override
  Future<void> close() async {}
}
