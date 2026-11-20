import 'dart:async';

import 'package:flutter/foundation.dart';

/// A single HTTPS request to response headers, including connection setup.
enum WebsiteLatencyStatus {
  untested,
  waiting,
  testing,
  success,
  timeout,
  failed,
  restricted,
  unexpected,
  cancelled,
}

@immutable
class WebsiteLatencyResult {
  const WebsiteLatencyResult(this.status, [this.milliseconds]);
  final WebsiteLatencyStatus status;
  final int? milliseconds;
}

const websiteLatencyTargets = <String, String>{
  '百度': 'https://www.baidu.com/',
  '哔哩哔哩': 'https://www.bilibili.com/',
  '腾讯网': 'https://www.qq.com/',
  '抖音': 'https://www.douyin.com/',
  '淘宝': 'https://www.taobao.com/',
};

/// Platform boundary. Cancellation closes owned network resources; [result]
/// settles even when an OS DNS lookup itself cannot be interrupted.
abstract interface class WebsiteLatencyProbe {
  Future<WebsiteLatencyResult> get result;
  void cancel();
}

typedef WebsiteLatencyProbeFactory = WebsiteLatencyProbe Function(Uri target);

class WebsiteLatencyController extends ChangeNotifier {
  WebsiteLatencyController({required this.createProbe});
  final WebsiteLatencyProbeFactory? createProbe;
  final _results = <String, WebsiteLatencyResult>{};
  final _active = <WebsiteLatencyProbe>{};
  Future<void>? _running;
  bool _cancelled = false;
  bool _disposed = false;
  DateTime? _testedAt;
  DateTime? get testedAt => _testedAt;

  bool get available => createProbe != null;
  bool get running => _running != null;
  WebsiteLatencyResult resultFor(String name) =>
      _results[name] ??
      const WebsiteLatencyResult(WebsiteLatencyStatus.untested);

  Future<void> test() {
    if (_disposed || !available) return Future.value();
    if (_running != null) return _running!;
    _cancelled = false;
    _testedAt = DateTime.now();
    for (final name in websiteLatencyTargets.keys) {
      _results[name] = const WebsiteLatencyResult(WebsiteLatencyStatus.waiting);
    }
    // Schedule workers after publishing the single in-flight future.
    final done = Completer<void>();
    _running = done.future;
    notifyListeners();
    unawaited(_run(done));
    return done.future;
  }

  Future<void> _run(Completer<void> done) async {
    final queue = websiteLatencyTargets.entries.iterator;
    Future<void> worker() async {
      while (!_cancelled && queue.moveNext()) {
        final target = queue.current;
        _results[target.key] = const WebsiteLatencyResult(
          WebsiteLatencyStatus.testing,
        );
        final probe = createProbe!(Uri.parse(target.value));
        _active.add(probe);
        if (!_disposed) notifyListeners();
        final result = await probe.result;
        _active.remove(probe);
        if (!_cancelled && !_disposed) {
          _results[target.key] = result;
          notifyListeners();
        }
      }
    }

    try {
      await Future.wait([worker(), worker()]);
    } finally {
      _running = null;
      if (!_disposed) notifyListeners();
      done.complete();
    }
  }

  Future<void> cancel() {
    final pending = _running;
    if (pending == null) return Future.value();
    _cancelled = true;
    for (final probe in _active.toList()) {
      probe.cancel();
    }
    for (final entry in _results.entries.toList()) {
      if (entry.value.status == WebsiteLatencyStatus.testing ||
          entry.value.status == WebsiteLatencyStatus.waiting) {
        _results[entry.key] = const WebsiteLatencyResult(
          WebsiteLatencyStatus.cancelled,
        );
      }
    }
    if (!_disposed) notifyListeners();
    return pending;
  }

  @override
  void dispose() {
    _disposed = true;
    unawaited(cancel());
    super.dispose();
  }
}
