part of 'ipc_models.dart';

const daemonModes = {'headless', 'desktop', 'systemd'};

class DaemonStatus {
  const DaemonStatus({
    required this.productVersion,
    required this.buildId,
    required this.pid,
    required this.status,
    required this.mode,
    this.desktopOwnerPid,
    this.logLevel,
  });
  final String productVersion, buildId, status, mode;
  final int pid;
  final int? desktopOwnerPid;
  final String? logLevel;
}

class DaemonStopResult {
  const DaemonStopResult({required this.status});
  final String status;
}

DaemonStatus decodeDaemonStatus(String source) {
  final raw = decodeBindingCheckedJson(source);
  if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
  final mode = _text(raw['mode']);
  final expected = mode == 'desktop'
      ? const {
          'productVersion',
          'buildId',
          'pid',
          'status',
          'mode',
          'desktopOwnerPid',
        }
      : const {'productVersion', 'buildId', 'pid', 'status', 'mode'};
  final value = decodeObjectValue(raw, {
    ...expected,
    if (raw.containsKey("logLevel")) "logLevel",
  });
  final logLevel = raw["logLevel"];
  if (logLevel != null &&
      !const {"info", "debug", "trace"}.contains(logLevel)) {
    throw const IpcProtocolException();
  }
  if (!daemonModes.contains(mode)) throw const IpcProtocolException();
  return DaemonStatus(
    logLevel: logLevel as String?,
    productVersion: _text(value['productVersion']),
    buildId: _text(value['buildId']),
    pid: _positive(value['pid']),
    status: _text(value['status']),
    mode: mode,
    desktopOwnerPid: mode == 'desktop'
        ? _positive(value['desktopOwnerPid'])
        : null,
  );
}

DaemonStopResult decodeDaemonStop(String source) {
  final value = decodeObject(source, const {'status'});
  final status = _text(value['status']);
  if (status != 'stopping') throw const IpcProtocolException();
  return DaemonStopResult(status: status);
}
