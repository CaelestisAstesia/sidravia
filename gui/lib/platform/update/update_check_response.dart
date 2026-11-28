import 'dart:convert';

import '../integration_failure.dart';
import 'package_update_checker.dart';
import 'update_release_version.dart';

PackageUpdate decodeUpdateCheckResponse(int exitCode, String output) {
  final response = jsonDecode(output);
  if (response is! Map<String, dynamic> || response['schemaVersion'] != 1) {
    throw const FormatException();
  }
  final code = response['code'];
  if (code is! String || code.isEmpty) throw const FormatException();
  if (response['ok'] == false && code != 'ok') {
    throw IntegrationFailure(switch (code) {
      'rate_limited' => 'GitHub 请求受限，请稍后重试。',
      'update_busy' => '正在执行另一项更新操作，请稍后重试。',
      'timeout' => '检查更新超时，请重试。',
      'source_unavailable' || 'incomplete_query' => '更新来源暂时不可用，请检查网络后重试。',
      'no_compatible_release' => '未找到适用于此安装的发行包',
      _ => '无法检查更新，请稍后重试。',
    });
  }
  if (exitCode != 0 || response['ok'] != true || code != 'ok') {
    throw const FormatException();
  }
  final check =
      (response['result'] as Map<String, dynamic>)['check']
          as Map<String, dynamic>;
  final state = switch (check['state']) {
    'available' => UpdateCheckState.available,
    'up_to_date' => UpdateCheckState.upToDate,
    'ahead_of_channel' => UpdateCheckState.aheadOfChannel,
    'no_compatible_release' => UpdateCheckState.noCompatibleRelease,
    _ => throw const FormatException(),
  };
  final candidate = check['candidate'] as Map<String, dynamic>?;
  final version = candidate?['version'] as String?;
  if (state == UpdateCheckState.available &&
      (version == null || !validReleaseVersion(version))) {
    throw const FormatException();
  }
  final canApply = candidate?['canApply'] as bool? ?? false;
  final id = candidate?['id'] as String?;
  if (state == UpdateCheckState.available &&
      canApply &&
      (id == null || !_candidateId.hasMatch(id))) {
    throw const FormatException();
  }
  return PackageUpdate(
    state: state,
    version: version,
    notes: candidate?['notes'] as String? ?? '',
    installCandidateId: state == UpdateCheckState.available && canApply
        ? id
        : null,
  );
}

final _candidateId = RegExp(r'^[1-9][0-9]{0,18}:[1-9][0-9]{0,18}$');

const _unknownInstall = IntegrationFailure(
  '更新结果尚未确认，请运行 sidraviactl update status 查看。',
);

UpdateInstallResult decodeUpdateOperationResponse(
  int? exitCode,
  String output, {
  required bool installing,
}) {
  final unknown = installing
      ? _unknownInstall
      : const IntegrationFailure('无法确认更新包是否下载完成。');
  final Object? response;
  try {
    response = jsonDecode(output);
  } on FormatException {
    throw unknown;
  }
  if (response is! Map<String, dynamic> || response['schemaVersion'] != 1) {
    throw unknown;
  }
  final code = response['code'];
  // The detached updater publishes a completed protocol response over stdout.
  if ((exitCode == null || exitCode == 0) && response['ok'] == true) {
    if (code == 'ok') return UpdateInstallResult.installed;
    if (installing && code == 'handed_over') {
      return UpdateInstallResult.handedOver;
    }
  }
  if (response['ok'] != false || code is! String || code == 'ok') {
    throw unknown;
  }
  throw IntegrationFailure(switch (code) {
    'candidate_changed' => '候选版本已变化，请重新检查更新。',
    'rate_limited' => 'GitHub 请求受限，请稍后重试。',
    'maintenance_image_requires_reinstall' => '此版本需要更换维护工具，请从发行页下载完整安装包',
    'source_proof_unavailable' => '无法验证当前安装，请从发行页手动更新。',
    'update_busy' ||
    'maintenance_active' => '正在执行另一项更新操作，请稍后运行 sidraviactl update status 查看。',
    'handoff_unconfirmed' => _unknownInstall.message,
    'update_rolled_back' => '升级失败，已恢复旧版文件，请检查运行状态。',
    'resume_failed' => '更新后的程序未能恢复，请运行 sidraviactl update status 查看。',
    _ =>
      installing ? '安装更新未完成，请运行 sidraviactl update status 查看。' : '下载更新未完成，请重试。',
  });
}
