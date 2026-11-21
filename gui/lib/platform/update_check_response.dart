import 'installation_manager.dart';

final _releaseVersion = RegExp(r'^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$');

PackageUpdate decodeUpdateCheckResult(Map<String, dynamic> response) {
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
      (version == null || !_releaseVersion.hasMatch(version))) {
    throw const FormatException();
  }
  return (state: state, version: version);
}
