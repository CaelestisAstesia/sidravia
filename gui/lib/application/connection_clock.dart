import 'dart:async';

import 'package:flutter/foundation.dart';

/// A display-only clock. Ticks never change domain snapshots or request IPC.
class ConnectionClock extends ChangeNotifier {
  ConnectionClock({DateTime Function()? now}) : _now = now ?? DateTime.now;
  final DateTime Function() _now;
  Timer? _timer;
  DateTime? _establishedAt;
  bool _active = true;
  DateTime get now => _now();

  void update(DateTime? establishedAt, {bool? active}) {
    final visible = active ?? _active;
    if (_establishedAt == establishedAt && _active == visible) return;
    final resumed = !_active && visible;
    _establishedAt = establishedAt;
    _active = visible;
    _schedule();
    if (resumed) notifyListeners();
  }

  void _schedule() {
    _timer?.cancel();
    _timer = null;
    final at = _establishedAt;
    if (!_active || at == null) return;
    final time = now;
    final elapsed = time.difference(at);
    final next = elapsed.isNegative
        ? at.add(const Duration(minutes: 1))
        : at.add(Duration(minutes: elapsed.inMinutes + 1));
    _timer = Timer(next.difference(time), () {
      _timer = null;
      _schedule();
      notifyListeners();
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    _timer = null;
    super.dispose();
  }
}
