import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_connection_state.dart';
import 'package:sidravia_gui/application/gui_network_diagnosis.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

/// Facts and dispatch permission from the sole IPC owner, never a client handle.
@immutable
class NetworkDiagnosisContext {
  const NetworkDiagnosisContext({
    required this.generation,
    required this.capabilities,
    required this.supported,
    required this.active,
    required this.canDispatch,
  });
  final int generation;
  final GuiCapabilities capabilities;
  final bool supported, active, canDispatch;

  (String?, String?)? get target {
    if (capabilities.capability != GuiCapabilityState.manageable) return null;
    final session = capabilities.retainedSession;
    return session != null
        ? (null, session.id)
        : (capabilities.configuration!.id, null);
  }

  String? get unavailable {
    if (!active || capabilities.state != GuiConnectionState.ready) {
      return '通信尚未就绪，网络诊断暂不可用。';
    }
    if (!supported) {
      return '当前客户端不提供网络诊断；离线预览没有真实网络观察。';
    }
    if (target == null) {
      return '需要一份连接配置及明确的会话关系；请先检查连接配置。';
    }
    return null;
  }
}

/// Null means the owner's serial gate did not dispatch; pending intent stays local.
typedef DiagnosisRequest = Future<NetworkDiagnosis?>? Function(
  NetworkDiagnosisContext context,
  bool probe,
);

class NetworkDiagnosisController extends ChangeNotifier {
  NetworkDiagnosisController({required this.request});
  final DiagnosisRequest request;
  NetworkDiagnosisContext? _context;
  NetworkDiagnosis? _result;
  String? _targetLabel, _error;
  bool _visible = false, _busy = false, _stale = true;
  bool _pending = false, _probe = false, _scheduled = false;
  bool _closed = false, _disposed = false;
  int _revision = 0;
  Future<void>? _work, _closing;

  NetworkDiagnosis? get result => _result;
  String? get targetLabel => _targetLabel;
  String? get error => _error;
  bool get busy => _busy;
  bool get stale => _stale || _context?.capabilities.isReady != true;
  String? get unavailable => _context == null
      ? '通信尚未就绪，网络诊断暂不可用。'
      : _context!.unavailable;
  bool get canRequest =>
      !_closed &&
      _visible &&
      unavailable == null &&
      _context?.capabilities.busy == false &&
      !_busy;

  void updateContext(NetworkDiagnosisContext context) {
    if (_closed) return;
    final previous = _context;
    final changed =
        previous == null ||
        previous.generation != context.generation ||
        !identical(
          previous.capabilities.snapshot,
          context.capabilities.snapshot,
        ) ||
        previous.capabilities.state != context.capabilities.state ||
        previous.active != context.active ||
        previous.supported != context.supported ||
        !previous.capabilities.busy && context.capabilities.busy;
    _context = context;
    if (changed) {
      ++_revision;
      _stale = true;
      _pending = _visible;
      // New facts and reconnects never repeat an explicitly requested probe.
      _probe = false;
      _notify();
    }
    _schedule();
  }

  void setVisible(bool visible) {
    if (_closed || _visible == visible) return;
    _visible = visible;
    ++_revision;
    _stale = true;
    _pending = visible;
    _probe = false;
    if (visible) _notify();
    _schedule();
  }

  void refresh({bool probe = false}) {
    if (!canRequest) return;
    _pending = true;
    _probe = probe;
    _stale = true;
    ++_revision;
    _notify();
    _schedule();
  }

  void _schedule() {
    if (_closed ||
        _scheduled ||
        !_pending ||
        !_visible ||
        _work != null ||
        _context?.canDispatch != true ||
        unavailable != null) {
      return;
    }
    _scheduled = true;
    scheduleMicrotask(() {
      _scheduled = false;
      final context = _context;
      if (_closed ||
          !_pending ||
          !_visible ||
          _work != null ||
          context == null ||
          !context.canDispatch ||
          unavailable != null) {
        return;
      }
      final future = request(context, _probe);
      if (future == null) return;
      final revision = _revision;
      _pending = false;
      _probe = false;
      _busy = true;
      _error = null;
      _notify();
      _work = _run(future, context, revision).whenComplete(() {
        _work = null;
        _schedule();
      });
    });
  }

  bool _current(NetworkDiagnosisContext context, int revision) =>
      !_closed &&
      _visible &&
      revision == _revision &&
      context.generation == _context?.generation &&
      context.target == _context?.target;

  Future<void> _run(
    Future<NetworkDiagnosis?> future,
    NetworkDiagnosisContext context,
    int revision,
  ) async {
    try {
      final result = await future;
      if (result != null && _current(context, revision)) {
        _result = result;
        _targetLabel = context.target!.$2 != null
            ? '会话 ${context.target!.$2}'
            : '配置 ${context.target!.$1}';
        _stale = false;
      }
    } on IpcRequestFailure catch (failure) {
      if (_current(context, revision)) {
        _error =
            '网络诊断失败：${diagnosisErrorMessage(failure.code)} (${failure.code})';
      }
    } finally {
      _busy = false;
      _notify();
    }
  }

  void _notify() {
    if (!_closed && !_disposed) notifyListeners();
  }

  Future<void> close() => _closing ??= _close();
  Future<void> _close() async {
    _closed = true;
    ++_revision;
    _pending = false;
    _probe = false;
    await _work;
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    unawaited(close());
    super.dispose();
  }
}
