import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

/// Owns only view/query lifetime. Session remains authoritative for account data.
class BusinessController extends ChangeNotifier {
  BusinessController({this.pollDelay = const Duration(seconds: 5)});
  final Duration pollDelay;
  BusinessSnapshot? _snapshot;
  BusinessSnapshot? get snapshot => _snapshot;
  bool get stale => _stale;
  bool get unsupported => _unsupported;
  bool _stale = false, _unsupported = false;
  bool _visible = false, _online = false, _disposed = false;
  SidraviaBusinessClient? _client;
  Object? _connection;
  String? _sessionId;
  BigInt _generation = BigInt.zero;
  Object? _epoch;
  int _requestEpoch = 0;
  Timer? _timer;
  BusinessQuery? _query;
  Future<void>? _flight;
  Future<void>? _closing;
  String? get feedback => _unsupported
      ? '当前后台不支持账户业务'
      : _stale
      ? '业务数据暂未更新'
      : null;
  void update({
    required Object? connection,
    required SidraviaBusinessClient? client,
    required SessionSummary? session,
    required bool visible,
  }) {
    if (_disposed) return;
    final generation = session?.protocolSocket.runGeneration ?? BigInt.zero;
    final epoch = (
      session?.id,
      generation,
      session?.authenticationEstablishedAt,
      session?.state,
      session?.intent,
    );
    final changed = !identical(connection, _connection) || epoch != _epoch;
    if (changed || visible != _visible) {
      ++_requestEpoch;
      _timer?.cancel();
      _timer = null;
      final previous = _query;
      if (previous != null) unawaited(previous.cancel());
      if (!identical(connection, _connection)) {
        _unsupported = false;
        _snapshot = null;
        _stale = false;
      }
      if (session?.id != _sessionId || generation != _generation) {
        _snapshot = null;
        _stale = false;
      } else if (changed && _snapshot != null) {
        _stale = true;
      }
      _connection = connection;
      _client = client;
      _sessionId = session?.id;
      _generation = generation;
      _epoch = epoch;
      _visible = visible;
      _online = session?.state == SessionState.authenticated;
      if (_canQuery && _flight == null) _start();
    }
  }

  bool get _canQuery =>
      !_disposed &&
      _visible &&
      _client != null &&
      _sessionId != null &&
      !_unsupported;
  void _start() {
    if (!_canQuery || _flight != null) return;
    final epoch = _requestEpoch;
    _flight = _refresh(epoch);
  }

  Future<void> _refresh(int epoch) async {
    final id = _sessionId!;
    BusinessQuery? query;
    try {
      query = _client!.queryBusiness(id);
      _query = query;
      final result = await query.result;
      if (epoch != _requestEpoch || !_canQuery) return;
      if (result.sessionId != id ||
          _generation != BigInt.zero && result.runGeneration != _generation ||
          _snapshot != null && result.revision < _snapshot!.revision) {
        _stale = true;
        notifyListeners();
        return;
      }
      _snapshot = result;
      _stale = false;
      notifyListeners();
    } on IpcRequestFailure catch (error) {
      if (epoch == _requestEpoch && !_disposed) {
        if (error.code == 'unknown_method') {
          _unsupported = true;
          _snapshot = null;
        } else {
          _stale = true;
        }
        notifyListeners();
      }
    } on Object {
      if (epoch == _requestEpoch && !_disposed) {
        _stale = true;
        notifyListeners();
      }
    } finally {
      await query?.cancel();
      _query = null;
      _flight = null;
      if (_canQuery) {
        if (epoch != _requestEpoch) {
          scheduleMicrotask(_start);
        } else if (_online) {
          _timer = Timer(pollDelay, _start);
        }
      }
    }
  }

  Future<void> close() {
    if (_closing != null) return _closing!;
    _disposed = true;
    ++_requestEpoch;
    _timer?.cancel();
    return _closing = () async {
      await _query?.cancel();
      await _flight;
    }();
  }

  @override
  void dispose() {
    unawaited(close());
    super.dispose();
  }
}
