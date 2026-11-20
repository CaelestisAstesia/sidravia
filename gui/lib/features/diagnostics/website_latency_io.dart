import 'dart:async';
import 'dart:io';

import 'package:http/http.dart' as http;
import 'package:http/io_client.dart';

import 'website_latency.dart';

WebsiteLatencyProbeFactory get websiteLatencyProbeFactory =>
    (uri) => HttpWebsiteLatencyProbe(uri);

class HttpWebsiteLatencyProbe implements WebsiteLatencyProbe {
  HttpWebsiteLatencyProbe(
    Uri target, {
    Duration timeout = const Duration(seconds: 5),
  }) : _client = IOClient(
         HttpClient()
           ..findProxy = ((_) => 'DIRECT')
           ..userAgent = 'Sidravia',
       ) {
    _timer = Timer(
      timeout,
      () => _finish(const WebsiteLatencyResult(WebsiteLatencyStatus.timeout)),
    );
    unawaited(_send(target));
  }

  final IOClient _client;
  final _abort = Completer<void>();
  final _done = Completer<WebsiteLatencyResult>();
  late final Timer _timer;
  @override
  Future<WebsiteLatencyResult> get result => _done.future;

  Future<void> _send(Uri target) async {
    final watch = Stopwatch()..start();
    try {
      final request =
          http.AbortableRequest('GET', target, abortTrigger: _abort.future)
            ..followRedirects = false
            ..persistentConnection = false;
      final response = await _client.send(request);
      final elapsed = watch.elapsedMilliseconds;
      // Never buffer the homepage. Cancellation also releases a late response.
      await response.stream.listen(null, onError: (Object _) {}).cancel();
      final status = response.statusCode;
      _finish(
        status >= 200 && status < 300
            ? WebsiteLatencyResult(WebsiteLatencyStatus.success, elapsed)
            : WebsiteLatencyResult(
                const [401, 403, 429].contains(status)
                    ? WebsiteLatencyStatus.restricted
                    : WebsiteLatencyStatus.unexpected,
              ),
      );
    } on http.ClientException {
      _finish(const WebsiteLatencyResult(WebsiteLatencyStatus.failed));
    } on IOException {
      _finish(const WebsiteLatencyResult(WebsiteLatencyStatus.failed));
    }
  }

  void _finish(WebsiteLatencyResult value) {
    if (_done.isCompleted) return;
    _timer.cancel();
    _abort.complete();
    _client.close(); // IOClient force-closes its dedicated HttpClient.
    _done.complete(value);
  }

  @override
  void cancel() =>
      _finish(const WebsiteLatencyResult(WebsiteLatencyStatus.cancelled));
}
