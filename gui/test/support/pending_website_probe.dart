import 'dart:async';

import 'package:sidravia_gui/features/diagnostics/website_latency.dart';

class PendingProbe implements WebsiteLatencyProbe {
  final done = Completer<WebsiteLatencyResult>();
  @override
  Future<WebsiteLatencyResult> get result => done.future;
  @override
  void cancel() =>
      done.complete(const WebsiteLatencyResult(WebsiteLatencyStatus.cancelled));
}
