part of 'ipc_models.dart';

class MaintenanceStatus {
  const MaintenanceStatus(this.operationId, this.phase, this.interactive);
  final String? operationId;
  final String phase;
  final bool interactive;
}

MaintenanceStatus decodeMaintenanceStatus(String source) {
  final raw = decodeBindingCheckedJson(source);
  if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
  final value = decodeObjectValue(raw, {
    'schemaVersion',
    'phase',
    'interactive',
    'intent',
    if (raw.containsKey('operationId')) 'operationId',
  });
  final phase = _text(value['phase']);
  if (value['schemaVersion'] != 1 ||
      value['interactive'] is! bool ||
      !const {
        'idle',
        'requested',
        'prepared',
        'rejected',
        'committed',
      }.contains(phase) ||
      value['intent'] is! Map<String, dynamic>) {
    throw const IpcProtocolException();
  }
  final id = value['operationId'];
  if (phase != 'idle' &&
      (id is! String || !RegExp(r'^[0-9a-f]{32}$').hasMatch(id))) {
    throw const IpcProtocolException();
  }
  return MaintenanceStatus(id as String?, phase, value['interactive'] as bool);
}
