import 'dart:async';
import 'dart:collection';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_feed.dart';
import 'package:sidravia_gui/features/announcements/announcement_model.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';

final _endpoint = Uri.parse('https://notices.example.edu/feed.json');

Map<String, Object?> _item(
  String id,
  String level,
  DateTime now, {
  int revision = 1,
  Duration publishAge = const Duration(hours: 1),
  Duration lifetime = const Duration(days: 5),
}) => {
  'id': id,
  'revision': revision,
  'level': level,
  'title': '公告 $id',
  'body': '正文 $id',
  'publishedAt': now.subtract(publishAge).toIso8601String(),
  'startsAt': now.subtract(publishAge).toIso8601String(),
  'expiresAt': now.add(lifetime).toIso8601String(),
};

String _feedJson(DateTime now, List<Map<String, Object?>> items) => jsonEncode({
  'schema': 1,
  'generatedAt': now.toIso8601String(),
  'items': items,
});

AnnouncementCache _cache(
  DateTime now,
  List<Map<String, Object?>> items, {
  String? etag,
  String? lastModified,
  DateTime? lastSuccess,
  List<String> readKeys = const [],
  List<String> dismissedKeys = const [],
}) => AnnouncementCache(
  feedJson: _feedJson(now, items),
  lastSuccessUtc: lastSuccess ?? now,
  etag: etag,
  lastModified: lastModified,
  readKeys: readKeys,
  dismissedKeys: dismissedKeys,
);

AnnouncementFetchResult _updated(
  DateTime now,
  List<Map<String, Object?>> items, {
  String? etag,
}) {
  final raw = _feedJson(now, items);
  return AnnouncementFetchResult.updated(
    feed: AnnouncementDocument.decode(raw, endpoint: _endpoint),
    rawBody: raw,
    etag: etag,
  );
}

final class _FakeStore implements AnnouncementStore {
  _FakeStore([this.value]);

  AnnouncementCache? value;
  Object? readError;
  Object? writeError;
  var reads = 0;
  var writes = 0;
  AnnouncementCache? lastWritten;

  @override
  Future<AnnouncementCache?> read() async {
    reads++;
    if (readError case final error?) throw error;
    return value;
  }

  @override
  Future<void> write(AnnouncementCache cache) async {
    writes++;
    lastWritten = cache;
    if (writeError case final error?) throw error;
    value = cache;
  }
}

final class _FakeFetcher implements AnnouncementFetcher {
  final Queue<Object> outcomes = Queue<Object>();
  var calls = 0;
  String? lastEtag;
  String? lastModified;
  var closed = false;

  @override
  Future<AnnouncementFetchResult> fetch({String? etag, String? lastModified}) {
    calls++;
    lastEtag = etag;
    this.lastModified = lastModified;
    final outcome = outcomes.removeFirst();
    return switch (outcome) {
      AnnouncementFetchResult result => Future.value(result),
      Future<AnnouncementFetchResult> pending => pending,
      _ => Future.error(outcome),
    };
  }

  @override
  void close() => closed = true;
}

void main() {
  late DateTime now;
  DateTime nowUtc() => now;

  setUp(() => now = DateTime.utc(2026, 8, 18, 12));

  test('a disabled endpoint performs no store access and no fetch', () async {
    final store = _FakeStore(_cache(now, [_item('a', 'info', now)]));
    final fetcher = _FakeFetcher();
    final controller = AnnouncementController(
      endpoint: null,
      store: store,
      fetcher: fetcher,
      nowUtc: nowUtc,
    );
    addTearDown(controller.dispose);

    expect(controller.enabled, isFalse);
    await controller.start();
    await controller.refreshIfDue();

    expect(store.reads, 0);
    expect(fetcher.calls, 0);
    expect(controller.announcements, isEmpty);
    expect(controller.unreadCount, 0);
    expect(controller.inlineNotice, isNull);
  });

  test('publishes the strict cache before an atomic 200 replacement', () async {
    final store = _FakeStore(
      _cache(now, [_item('a-old', 'info', now)], etag: '"e1"'),
    );
    final fetcher = _FakeFetcher();
    final pending = Completer<AnnouncementFetchResult>();
    fetcher.outcomes.add(pending.future);
    final controller = AnnouncementController(
      endpoint: _endpoint,
      store: store,
      fetcher: fetcher,
      nowUtc: nowUtc,
    );
    addTearDown(controller.dispose);

    final started = controller.start();
    await pumpEventQueue();
    expect(controller.announcements.map((item) => item.id), ['a-old']);
    expect(fetcher.calls, 1);
    expect(fetcher.lastEtag, '"e1"');

    pending.complete(
      _updated(now, [_item('b-new', 'info', now)], etag: '"e2"'),
    );
    await started;
    expect(controller.announcements.map((item) => item.id), ['b-new']);
    expect(store.lastWritten?.etag, '"e2"');
    expect(store.lastWritten?.lastSuccessUtc, now);
  });

  test('keeps an unexpired cache across fetch and decode failures', () async {
    for (final failure in [
      const AnnouncementFetchException('offline'),
      const FormatException('invalid new feed'),
    ]) {
      final store = _FakeStore(_cache(now, [_item('a-old', 'info', now)]));
      final fetcher = _FakeFetcher()..outcomes.add(failure);
      final controller = AnnouncementController(
        endpoint: _endpoint,
        store: store,
        fetcher: fetcher,
        nowUtc: nowUtc,
      );
      await controller.start();
      expect(controller.announcements.map((item) => item.id), [
        'a-old',
      ], reason: '$failure');
      expect(store.writes, 0);
      controller.dispose();
    }
  });

  test('filters expired cache items even while offline', () async {
    final store = _FakeStore(
      _cache(now, [
        _item('expired', 'critical', now, lifetime: const Duration(hours: -1)),
      ]),
    );
    final fetcher = _FakeFetcher()
      ..outcomes.add(const AnnouncementFetchException('offline'));
    final controller = AnnouncementController(
      endpoint: _endpoint,
      store: store,
      fetcher: fetcher,
      nowUtc: nowUtc,
    );
    addTearDown(controller.dispose);

    await controller.start();
    expect(controller.announcements, isEmpty);
    expect(controller.unreadCount, 0);
    expect(controller.inlineNotice, isNull);
  });

  test(
    'a 304 with cache keeps the projection and updates revalidation time',
    () async {
      final store = _FakeStore(
        _cache(
          now,
          [_item('a-old', 'info', now)],
          etag: '"e1"',
          lastSuccess: now.subtract(const Duration(hours: 1)),
        ),
      );
      final fetcher = _FakeFetcher()
        ..outcomes.add(
          AnnouncementFetchResult.notModified(
            etag: '"e2"',
            lastModified: 'Tue, 18 Aug 2026 12:00:00 GMT',
          ),
        );
      final controller = AnnouncementController(
        endpoint: _endpoint,
        store: store,
        fetcher: fetcher,
        nowUtc: nowUtc,
      );
      addTearDown(controller.dispose);

      await controller.start();
      expect(controller.announcements.map((item) => item.id), ['a-old']);
      expect(store.writes, 1);
      expect(store.lastWritten?.lastSuccessUtc, now);
      expect(store.lastWritten?.etag, '"e2"');
      expect(store.lastWritten?.lastModified, 'Tue, 18 Aug 2026 12:00:00 GMT');

      await controller.refreshIfDue();
      expect(fetcher.calls, 1);
    },
  );

  test('a 304 without cache is a non-fatal fetch failure', () async {
    final store = _FakeStore();
    final fetcher = _FakeFetcher()
      ..outcomes.add(AnnouncementFetchResult.notModified(etag: '"e1"'));
    final controller = AnnouncementController(
      endpoint: _endpoint,
      store: store,
      fetcher: fetcher,
      nowUtc: nowUtc,
    );
    addTearDown(controller.dispose);

    await controller.start();
    expect(controller.announcements, isEmpty);
    expect(store.writes, 0);

    fetcher.outcomes.add(_updated(now, [_item('b-new', 'info', now)]));
    await controller.refreshIfDue();
    expect(fetcher.calls, 2);
    expect(controller.announcements.map((item) => item.id), ['b-new']);
  });

  test('a store write failure never rolls back an in-memory success', () async {
    final store = _FakeStore()..writeError = StateError('disk full');
    final fetcher = _FakeFetcher()
      ..outcomes.add(_updated(now, [_item('b-new', 'info', now)]));
    final controller = AnnouncementController(
      endpoint: _endpoint,
      store: store,
      fetcher: fetcher,
      nowUtc: nowUtc,
    );
    addTearDown(controller.dispose);

    await controller.start();
    expect(controller.announcements.map((item) => item.id), ['b-new']);
  });

  test('a store read failure is treated as an empty cache', () async {
    final store = _FakeStore()..readError = StateError('disk gone');
    final fetcher = _FakeFetcher()
      ..outcomes.add(_updated(now, [_item('b-new', 'info', now)]));
    final controller = AnnouncementController(
      endpoint: _endpoint,
      store: store,
      fetcher: fetcher,
      nowUtc: nowUtc,
    );
    addTearDown(controller.dispose);

    await controller.start();
    expect(fetcher.lastEtag, isNull);
    expect(controller.announcements.map((item) => item.id), ['b-new']);
  });

  test(
    'a corrupted cache is dropped and revalidated without validators',
    () async {
      final store = _FakeStore(
        AnnouncementCache(
          feedJson: 'not json at all',
          lastSuccessUtc: DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
          etag: '"e-stale"',
        ),
      );
      final fetcher = _FakeFetcher()
        ..outcomes.add(_updated(now, [_item('b-new', 'info', now)]));
      final controller = AnnouncementController(
        endpoint: _endpoint,
        store: store,
        fetcher: fetcher,
        nowUtc: nowUtc,
      );
      addTearDown(controller.dispose);

      await controller.start();
      expect(fetcher.lastEtag, isNull);
      expect(controller.announcements.map((item) => item.id), ['b-new']);
    },
  );

  test(
    'dispose invalidates in-flight generations and closes the fetcher',
    () async {
      final store = _FakeStore(_cache(now, [_item('a-old', 'info', now)]));
      final fetcher = _FakeFetcher();
      final pending = Completer<AnnouncementFetchResult>();
      fetcher.outcomes.add(pending.future);
      final controller = AnnouncementController(
        endpoint: _endpoint,
        store: store,
        fetcher: fetcher,
        nowUtc: nowUtc,
      );
      var notifications = 0;
      controller.addListener(() => notifications++);

      final started = controller.start();
      await pumpEventQueue();
      controller.dispose();
      expect(fetcher.closed, isTrue);

      final before = notifications;
      pending.complete(_updated(now, [_item('b-new', 'info', now)]));
      await started;
      await pumpEventQueue();
      expect(notifications, before);
      expect(store.writes, 0);
    },
  );

  test('refreshIfDue fetches at most once per refresh interval', () async {
    final store = _FakeStore(_cache(now, [_item('a-old', 'info', now)]));
    final fetcher = _FakeFetcher()
      ..outcomes.add(_updated(now, [_item('a-old', 'info', now)]))
      ..outcomes.add(_updated(now, [_item('a-old', 'info', now)]));
    final controller = AnnouncementController(
      endpoint: _endpoint,
      store: store,
      fetcher: fetcher,
      nowUtc: nowUtc,
    );
    addTearDown(controller.dispose);

    await controller.start();
    expect(fetcher.calls, 1);
    await controller.refreshIfDue();
    expect(fetcher.calls, 1);

    now = now.add(const Duration(hours: 7));
    await controller.refreshIfDue();
    expect(fetcher.calls, 2);
    await controller.refreshIfDue();
    expect(fetcher.calls, 2);
  });

  test('markCurrentRead records id@revision keys with a 128-key cap', () async {
    final store = _FakeStore(
      _cache(now, [_item('a', 'info', now), _item('b', 'maintenance', now)]),
    );
    final controller = AnnouncementController(
      endpoint: _endpoint,
      store: store,
      nowUtc: nowUtc,
    );
    addTearDown(controller.dispose);

    await controller.start();
    expect(controller.unreadCount, 2);
    controller.markCurrentRead();
    expect(controller.unreadCount, 0);
    expect(store.lastWritten?.readKeys, containsAll(<String>['a@1', 'b@1']));

    final capped = _FakeStore(
      _cache(
        now,
        [_item('new-item', 'info', now)],
        readKeys: [for (var i = 0; i < 128; i++) 'old-$i@1'],
      ),
    );
    final cappedController = AnnouncementController(
      endpoint: _endpoint,
      store: capped,
      nowUtc: nowUtc,
    );
    addTearDown(cappedController.dispose);
    await cappedController.start();
    expect(cappedController.unreadCount, 1);
    cappedController.markCurrentRead();
    final keys = capped.lastWritten?.readKeys;
    expect(keys, hasLength(128));
    expect(keys?.first, 'new-item@1');
    expect(keys, isNot(contains('old-127@1')));
  });

  test(
    'dismissInline hides only the inline notice and records the key',
    () async {
      final store = _FakeStore(_cache(now, [_item('m', 'maintenance', now)]));
      final controller = AnnouncementController(
        endpoint: _endpoint,
        store: store,
        nowUtc: nowUtc,
      );
      addTearDown(controller.dispose);

      await controller.start();
      expect(controller.inlineNotice?.id, 'm');
      controller.dismissInline('m@1');
      expect(controller.inlineNotice, isNull);
      expect(controller.announcements.map((item) => item.id), ['m']);
      expect(store.lastWritten?.dismissedKeys, contains('m@1'));
    },
  );

  test('a new revision becomes unread and loses its dismissed state', () async {
    final store = _FakeStore(
      _cache(
        now,
        [_item('n', 'maintenance', now, revision: 2)],
        readKeys: const ['n@1'],
        dismissedKeys: const ['n@1'],
      ),
    );
    final controller = AnnouncementController(
      endpoint: _endpoint,
      store: store,
      nowUtc: nowUtc,
    );
    addTearDown(controller.dispose);

    await controller.start();
    expect(controller.unreadCount, 1);
    expect(controller.inlineNotice?.key, 'n@2');
  });

  test(
    'inline notice picks the highest priority unread maintenance class',
    () async {
      final store = _FakeStore(
        _cache(
          now,
          [
            _item('i', 'info', now),
            _item('m', 'maintenance', now),
            _item('c', 'critical', now),
          ],
          readKeys: const ['c@1'],
        ),
      );
      final controller = AnnouncementController(
        endpoint: _endpoint,
        store: store,
        nowUtc: nowUtc,
      );
      addTearDown(controller.dispose);

      await controller.start();
      expect(controller.inlineNotice?.id, 'm');

      final infoOnly = AnnouncementController(
        endpoint: _endpoint,
        store: _FakeStore(_cache(now, [_item('i', 'info', now)])),
        nowUtc: nowUtc,
      );
      addTearDown(infoOnly.dispose);
      await infoOnly.start();
      expect(infoOnly.inlineNotice, isNull);
      expect(infoOnly.unreadCount, 1);
    },
  );
}
