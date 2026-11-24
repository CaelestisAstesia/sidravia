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
  return (state: state, version: version);
}
