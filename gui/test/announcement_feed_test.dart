import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:sidravia_gui/features/announcements/announcement_feed.dart';

final _endpoint = Uri.parse('https://notices.example.edu/feed.json');

String _feedBody() => jsonEncode({
  'schema': 1,
  'generatedAt': '2026-08-18T08:00:00Z',
  'items': [
    {
      'id': 'notice-1',
      'revision': 1,
      'level': 'maintenance',
      'title': '校园网维护',
      'body': '8 月 20 日凌晨维护。',
      'publishedAt': '2026-08-18T08:00:00Z',
      'startsAt': '2026-08-18T08:00:00Z',
      'expiresAt': '2026-08-20T18:00:00Z',
    },
  ],
});

String _feedBodyOfSize(int size) {
  final base = _feedBody();
  final padding = size - utf8.encode(base).length;
  expect(padding, greaterThanOrEqualTo(0));
  return '$base${' ' * padding}';
}

HttpAnnouncementFetcher _fetcher(
  http.Client client, {
  Duration timeout = const Duration(seconds: 5),
}) => HttpAnnouncementFetcher(
  endpoint: _endpoint,
  client: client,
  timeout: timeout,
);

void main() {
  test('parseAnnouncementEndpoint accepts only clean absolute HTTPS URLs', () {
    expect(parseAnnouncementEndpoint(''), isNull);
    expect(parseAnnouncementEndpoint('   '), isNull);
    expect(parseAnnouncementEndpoint('notices.example.edu/feed.json'), isNull);
    expect(parseAnnouncementEndpoint('http://notices.example.edu/f'), isNull);
    expect(
      parseAnnouncementEndpoint('https://user@notices.example.edu/f'),
      isNull,
    );
    expect(
      parseAnnouncementEndpoint('https://notices.example.edu/f#frag'),
      isNull,
    );
    expect(
      parseAnnouncementEndpoint(' https://notices.example.edu/feed.json '),
      Uri.parse('https://notices.example.edu/feed.json'),
    );
  });

  test(
    'sends strict headers and validators without following redirects',
    () async {
      http.BaseRequest? captured;
      final client = MockClient((request) async {
        captured = request;
        return http.Response(
          _feedBody(),
          200,
          headers: {
            'content-type': 'application/json',
            'etag': '"v1"',
            'last-modified': 'Tue, 18 Aug 2026 08:00:00 GMT',
          },
        );
      });

      final result = await _fetcher(client)
          .fetch(etag: '"v0"', lastModified: 'Mon, 17 Aug 2026 08:00:00 GMT');

      expect(result.outcome, AnnouncementFetchOutcome.updated);
      expect(result.feed?.items.single.id, 'notice-1');
      expect(result.rawBody, isNotEmpty);
      expect(result.etag, '"v1"');
      expect(result.lastModified, contains('2026'));

      final request = captured!;
      expect(request.method, 'GET');
      expect(request.url, _endpoint);
      expect(request.followRedirects, isFalse);
      expect(request.headers['accept'], 'application/json');
      expect(request.headers['accept-encoding'], 'identity');
      expect(request.headers['if-none-match'], '"v0"');
      expect(request.headers['if-modified-since'], contains('2026'));
      expect(request.headers.containsKey('cookie'), isFalse);
      expect(request.headers.containsKey('authorization'), isFalse);
    },
  );

  test('omits validators when no cache exists', () async {
    http.BaseRequest? captured;
    final client = MockClient((request) async {
      captured = request;
      return http.Response(
        _feedBody(),
        200,
        headers: {'content-type': 'application/json'},
      );
    });

    await _fetcher(client).fetch();
    expect(captured!.headers.containsKey('if-none-match'), isFalse);
    expect(captured!.headers.containsKey('if-modified-since'), isFalse);
  });

  test('reports a 304 as notModified with response validators', () async {
    final client = MockClient((request) async {
      return http.Response('', 304, headers: {'etag': '"v1"'});
    });

    final result = await _fetcher(client).fetch(etag: '"v1"');
    expect(result.outcome, AnnouncementFetchOutcome.notModified);
    expect(result.feed, isNull);
    expect(result.etag, '"v1"');
  });

  test('rejects redirects and other unexpected statuses', () async {
    for (final status in [302, 404, 500]) {
      final client = MockClient((request) async {
        return http.Response(
          '',
          status,
          headers: {'location': 'https://notices.example.edu/moved'},
        );
      });
      await expectLater(
        _fetcher(client).fetch(),
        throwsA(isA<AnnouncementFetchException>()),
        reason: '$status',
      );
    }
  });

  test('requires an application/json content type', () async {
    final wrongType = MockClient((request) async {
      return http.Response(
        _feedBody(),
        200,
        headers: {'content-type': 'text/plain'},
      );
    });
    await expectLater(
      _fetcher(wrongType).fetch(),
      throwsA(isA<AnnouncementFetchException>()),
    );

    final missingType = MockClient.streaming((request, _) async {
      return http.StreamedResponse(Stream.value(utf8.encode(_feedBody())), 200);
    });
    await expectLater(
      _fetcher(missingType).fetch(),
      throwsA(isA<AnnouncementFetchException>()),
    );

    final withCharset = MockClient((request) async {
      return http.Response(
        _feedBody(),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    final result = await _fetcher(withCharset).fetch();
    expect(result.outcome, AnnouncementFetchOutcome.updated);
  });

  test('rejects compressed responses', () async {
    final client = MockClient((request) async {
      return http.Response(
        _feedBody(),
        200,
        headers: {
          'content-type': 'application/json',
          'content-encoding': 'gzip',
        },
      );
    });
    await expectLater(
      _fetcher(client).fetch(),
      throwsA(isA<AnnouncementFetchException>()),
    );
  });

  test('accepts exactly 65536 bytes and rejects one byte more', () async {
    final accepted = MockClient((request) async {
      return http.Response(
        _feedBodyOfSize(HttpAnnouncementFetcher.maxBodyBytes),
        200,
        headers: {'content-type': 'application/json'},
      );
    });
    final result = await _fetcher(accepted).fetch();
    expect(result.feed?.items.single.id, 'notice-1');

    final tooLarge = MockClient((request) async {
      return http.Response(
        _feedBodyOfSize(HttpAnnouncementFetcher.maxBodyBytes + 1),
        200,
        headers: {'content-type': 'application/json'},
      );
    });
    await expectLater(
      _fetcher(tooLarge).fetch(),
      throwsA(isA<AnnouncementFetchException>()),
    );
  });

  test('rejects malformed JSON and schema violations', () async {
    for (final body in [
      'this is not json',
      _feedBody().replaceFirst('"schema":1', '"schema":2'),
    ]) {
      final client = MockClient((request) async {
        return http.Response(
          body,
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      await expectLater(
        _fetcher(client).fetch(),
        throwsA(isA<FormatException>()),
        reason: body,
      );
    }
  });

  test('wraps transport failures', () async {
    final client = MockClient((request) async {
      throw StateError('network down');
    });
    await expectLater(
      _fetcher(client).fetch(),
      throwsA(isA<AnnouncementFetchException>()),
    );
  });

  test('times out a stalled request', () async {
    final completer = Completer<http.Response>();
    final client = MockClient((request) => completer.future);
    await expectLater(
      _fetcher(client, timeout: const Duration(milliseconds: 50)).fetch(),
      throwsA(isA<AnnouncementFetchException>()),
    );
    completer.complete(
      http.Response(
        _feedBody(),
        200,
        headers: {'content-type': 'application/json'},
      ),
    );
  });

  test('close closes the underlying client', () {
    final client = _CloseTrackingClient();
    _fetcher(client).close();
    expect(client.closed, isTrue);
  });
}

final class _CloseTrackingClient extends http.BaseClient {
  var closed = false;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) =>
      Future.error(StateError('unused'));

  @override
  void close() {
    closed = true;
    super.close();
  }
}
