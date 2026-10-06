import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';

class BlockingStore implements AnnouncementStore {
  final firstGate = Completer<void>();
  final writes = <AnnouncementCache>[];
  AnnouncementCache? value;
  bool failFirst = false;
  @override
  Future<AnnouncementCache?> read() async => null;
  @override
  Future<void> write(AnnouncementCache cache) async {
    writes.add(cache);
    if (writes.length == 1) {
      await firstGate.future;
      if (failFirst) throw StateError('fixture storage error');
    }
    value = cache;
  }
}

Future<void> drain() async {
  for (var i = 0; i < 5; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

void main() {
  for (final failFirst in [false, true]) {
    test(
      'announcement latest cache survives blocked old write failFirst=$failFirst',
      () async {
        final now = DateTime.utc(2026, 10, 6);
        final store = BlockingStore()..failFirst = failFirst;
        final c = AnnouncementController(
          endpoint: OfflineAnnouncementFetcher.endpoint,
          store: store,
          fetcher: OfflineAnnouncementFetcher(now),
          nowUtc: () => now,
        );
        await c.start();
        await drain();
        expect(store.writes, hasLength(1));
        expect(store.writes.first.readKeys, isEmpty);
        c.markCurrentRead();
        await drain();
        expect(c.unreadCount, 0);
        store.firstGate.complete();
        await drain();
        expect(store.value!.readKeys, ['offline-maintenance@1']);
        expect(store.writes, hasLength(2));
        c.dispose();
      },
    );
  }
}
