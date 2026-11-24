import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/diagnostics/website_latency.dart';
import 'package:sidravia_gui/features/diagnostics/website_latency_io.dart';

import '../../support/pending_website_probe.dart';

void main() {
  test(
    'single flight, two workers, cancellation and retry isolation',
    () async {
      final probes = <PendingProbe>[];
      final controller = WebsiteLatencyController(
        createProbe: (_) {
          final probe = PendingProbe();
          probes.add(probe);
          return probe;
        },
      );
      final first = controller.test();
      expect(identical(first, controller.test()), isTrue);
      expect(probes.length, 2);
      probes.first.done.complete(
        const WebsiteLatencyResult(WebsiteLatencyStatus.success, 12),
      );
      await Future<void>.delayed(Duration.zero);
      expect(probes.length, 3);
      await controller.cancel();
      await first;
      expect(controller.running, isFalse);
      expect(controller.resultFor('百度').milliseconds, 12);
      expect(controller.resultFor('淘宝').status, WebsiteLatencyStatus.cancelled);
      final second = controller.test();
      expect(controller.resultFor('百度').status, WebsiteLatencyStatus.testing);
      controller.dispose();
      await second;
    },
  );

  test('unavailable capability starts no work', () async {
    final controller = WebsiteLatencyController(createProbe: null);
    await controller.test();
    expect(controller.running, isFalse);
    expect(controller.testedAt, isNull);
    controller.dispose();
  });

  test(
    'real HTTP status contract, headers only, no redirect or cookies',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      var calls = 0;
      server.listen((request) {
        calls++;
        expect(request.method, 'GET');
        expect(request.headers.value('cookie'), isNull);
        final status = int.parse(request.uri.path.substring(1));
        request.response.statusCode = status;
        request.response.headers.set('location', '/200');
        request.response.contentLength = 100000;
        request.response.write('x');
        unawaited(request.response.flush()); // Intentionally never finish body.
      });
      try {
        for (final (status, expected) in [
          (200, WebsiteLatencyStatus.success),
          (302, WebsiteLatencyStatus.unexpected),
          (403, WebsiteLatencyStatus.restricted),
          (429, WebsiteLatencyStatus.restricted),
          (503, WebsiteLatencyStatus.unexpected),
        ]) {
          final probe = HttpWebsiteLatencyProbe(
            Uri.parse('http://127.0.0.1:${server.port}/$status'),
          );
          final result = await probe.result;
          expect(result.status, expected);
          expect(result.milliseconds != null, status == 200);
        }
        expect(calls, 5);
      } finally {
        await server.close(force: true);
      }
    },
  );

  test(
    'timeout and cancel settle before headers; late data cannot replace result',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final seen = StreamController<HttpRequest>();
      server.listen(seen.add);
      final requests = StreamIterator(seen.stream);
      try {
        final probe = HttpWebsiteLatencyProbe(
          Uri.parse('http://127.0.0.1:${server.port}/'),
          timeout: const Duration(milliseconds: 100),
        );
        await requests.moveNext();
        final request = requests.current;
        expect((await probe.result).status, WebsiteLatencyStatus.timeout);
        await request.response.close();
        expect((await probe.result).status, WebsiteLatencyStatus.timeout);
        final cancelled = HttpWebsiteLatencyProbe(
          Uri.parse('http://127.0.0.1:${server.port}/'),
        );
        await requests.moveNext();
        cancelled.cancel();
        cancelled.cancel();
        expect((await cancelled.result).status, WebsiteLatencyStatus.cancelled);
        // Cancel immediately, including before openUrl has yielded a request.
        final early = HttpWebsiteLatencyProbe(
          Uri.parse('https://example.invalid/'),
        );
        early.cancel();
        expect((await early.result).status, WebsiteLatencyStatus.cancelled);
      } finally {
        await requests.cancel();
        await seen.close();
        await server.close(force: true);
      }
    },
  );

  test('direct connection bypasses process proxy settings', () async {
    // Run with HTTP_PROXY/http_proxy pointing at a dead loopback port.
    final server = await HttpServer.bind(InternetAddress('127.0.0.2'), 0);
    server.listen((request) => request.response.close());
    try {
      final probe = HttpWebsiteLatencyProbe(
        Uri.parse('http://127.0.0.2:${server.port}/'),
      );
      expect((await probe.result).status, WebsiteLatencyStatus.success);
    } finally {
      await server.close(force: true);
    }
  });

  if (Platform.environment['SIDRAVIA_WEBSITE_PROBE'] == '1') {
    test('bounded live HTTPS target observations', () async {
      for (final target in websiteLatencyTargets.entries) {
        final result = await HttpWebsiteLatencyProbe(Uri.parse(target.value))
            .result;
        // Only safe status and elapsed time, no body or raw exception.
        // ignore: avoid_print
        print(
          '${target.key}: ${result.status.name} ${result.milliseconds ?? ""}',
        );
      }
    });
  }
}
