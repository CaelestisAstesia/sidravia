import 'dart:convert';

import 'installation_manager_stub.dart'
    if (dart.library.io) 'installation_manager_io.dart'
    as platform;

enum IntegrationChange {
  pathEnable,
  pathDisable,
  startGui,
  startDaemon,
  startDisable,
}

class IntegrationStatus {
  const IntegrationStatus({
    required this.pathEnabled,
    required this.autostart,
    required this.guiAvailable,
  });
  final bool pathEnabled;
  final String autostart;
  final bool guiAvailable;

  factory IntegrationStatus.decode(String text) {
    final result = decodeManagerResponse(text);
    if (result['pathEnabled'] is! bool ||
        result['guiAvailable'] is! bool ||
        !const [
          'disabled',
          'gui',
          'daemon',
          'conflict',
        ].contains(result['autostart'])) {
      throw const IntegrationFailure('维护工具返回了无效状态。');
    }
    return IntegrationStatus(
      pathEnabled: result['pathEnabled'] as bool,
      autostart: result['autostart'] as String,
      guiAvailable: result['guiAvailable'] as bool,
    );
  }
}

class IntegrationFailure implements Exception {
  const IntegrationFailure(this.message);
  final String message;
}

Map<String, dynamic> decodeManagerResponse(String text) {
  final dynamic raw;
  try {
    raw = jsonDecode(text);
  } on Object {
    throw const IntegrationFailure('维护工具返回了无效结果。');
  }
  if (raw is! Map<String, dynamic> ||
      raw['version'] != 1 ||
      raw['ok'] is! bool ||
      raw['code'] is! String) {
    throw const IntegrationFailure('维护工具版本不兼容或结果无效。');
  }
  if (raw['ok'] != true) {
    final message = switch (raw['code']) {
      'scheduled_task_conflict' => '开机自启任务属于其他安装或定义不匹配，请先处理冲突。',
      'autostart_target_missing' => '所选启动程序不存在，请检查安装是否完整。',
      'unsupported' => '当前平台尚不支持系统集成管理。',
      _ => '系统集成操作失败，请刷新状态后重试。',
    };
    throw IntegrationFailure(message);
  }
  final capabilities = raw['capabilities'];
  if (raw['code'] != 'ok' ||
      capabilities is! List ||
      !capabilities.contains('user_path') ||
      !capabilities.contains('user_autostart')) {
    throw const IntegrationFailure('维护工具版本不兼容。');
  }
  return raw;
}

abstract interface class InstallationManager {
  Future<IntegrationStatus> status();
  Future<void> change(IntegrationChange change);
  Future<void> close();
}

InstallationManager createInstallationManager() => platform.createManager();
