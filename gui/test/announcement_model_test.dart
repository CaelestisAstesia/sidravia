import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/announcements/announcement_model.dart';

final _endpoint = Uri.parse('https://notices.example.edu/feed.json');

String _feed({Object? schema = 1, List<Map<String, Object?>>? items}) =>
    jsonEncode({
      'schema': schema,
      'generatedAt': '2026-08-18T08:00:00Z',
      'items': items ?? [_item()],
    });

Map<String, Object?> _item({
  Object? id = 'notice-1',
  Object? revision = 1,
  Object? level = 'info',
  Object? title = '校园网公告',
  Object? body = '公告正文内容。',
  Object? publishedAt = '2026-08-18T08:00:00Z',
  Object? startsAt = '2026-08-18T08:00:00Z',
  Object? expiresAt = '2026-08-20T18:00:00Z',
  Object? actionLabel,
  Object? actionUrl,
  Map<String, Object?> extra = const {},
}) {
  final map = <String, Object?>{};
  void put(String key, Object? value) {
    if (value != null) map[key] = value;
  }

  put('id', id);
  put('revision', revision);
  put('level', level);
  put('title', title);
  put('body', body);
  put('publishedAt', publishedAt);
  put('startsAt', startsAt);
  put('expiresAt', expiresAt);
  put('actionLabel', actionLabel);
  put('actionUrl', actionUrl);
  map.addAll(extra);
  return map;
}

void main() {
  test('decodes a strict schema-1 feed and normalizes values', () {
    final document = AnnouncementDocument.decode(
      _feed(
        items: [
          _item(
            title: '  校园网维护  ',
            body: '  8 月 20 日凌晨维护。  ',
            publishedAt: '2026-08-18T16:00:00+08:00',
            startsAt: '2026-08-18T16:00:00+08:00',
            expiresAt: '2026-08-21T02:00:00+08:00',
            actionLabel: ' 查看详情 ',
            actionUrl: 'https://notices.example.edu/detail',
            extra: const {'futureField': true},
          ),
        ],
      ),
      endpoint: _endpoint,
    );

    expect(document.generatedAt, DateTime.utc(2026, 8, 18, 8));
    final item = document.items.single;
    expect(item.id, 'notice-1');
    expect(item.revision, 1);
    expect(item.level, AnnouncementLevel.info);
    expect(item.title, '校园网维护');
    expect(item.body, '8 月 20 日凌晨维护。');
    expect(item.publishedAt, DateTime.utc(2026, 8, 18, 8));
    expect(item.publishedAt.isUtc, isTrue);
    expect(item.expiresAt, DateTime.utc(2026, 8, 20, 18));
    expect(item.actionLabel, '查看详情');
    expect(item.actionUrl, Uri.parse('https://notices.example.edu/detail'));
    expect(item.key, 'notice-1@1');
  });

  test('rejects a missing, unknown or wrongly typed schema', () {
    for (final source in [
      _feed(schema: 0),
      _feed(schema: 2),
      _feed(schema: '1'),
      _feed(schema: 1.0),
      _feed(schema: null),
      '[]',
      'not json',
    ]) {
      expect(
        () => AnnouncementDocument.decode(source, endpoint: _endpoint),
        throwsA(isA<AnnouncementFormatException>()),
        reason: source,
      );
    }
  });

  test('accepts exactly 20 items and rejects 21', () {
    final items = [for (var i = 0; i < 20; i++) _item(id: 'notice-$i')];
    expect(
      AnnouncementDocument.decode(
        _feed(items: items),
        endpoint: _endpoint,
      ).items,
      hasLength(20),
    );
    expect(
      () => AnnouncementDocument.decode(
        _feed(
          items: [
            ...items,
            _item(id: 'notice-20'),
          ],
        ),
        endpoint: _endpoint,
      ),
      throwsA(isA<AnnouncementFormatException>()),
    );
  });

  test('validates id shape and uniqueness within one feed', () {
    for (final id in ['Notice-1', '-notice', '_notice', '', 'a' * 65]) {
      expect(
        () => AnnouncementDocument.decode(
          _feed(items: [_item(id: id)]),
          endpoint: _endpoint,
        ),
        throwsA(isA<AnnouncementFormatException>()),
        reason: id,
      );
    }
    expect(
      AnnouncementDocument.decode(
        _feed(items: [_item(id: 'a.${'b' * 60}_-')]),
        endpoint: _endpoint,
      ).items.single.id,
      'a.${'b' * 60}_-',
    );
    expect(
      () => AnnouncementDocument.decode(
        _feed(
          items: [
            _item(id: 'dup'),
            _item(id: 'dup', revision: 2),
          ],
        ),
        endpoint: _endpoint,
      ),
      throwsA(isA<AnnouncementFormatException>()),
    );
  });

  test('validates revision as a positive integer', () {
    for (final revision in [0, -1, '1', 1.5, null]) {
      expect(
        () => AnnouncementDocument.decode(
          _feed(items: [_item(revision: revision)]),
          endpoint: _endpoint,
        ),
        throwsA(isA<AnnouncementFormatException>()),
        reason: '$revision',
      );
    }
  });

  test('validates the level enum', () {
    for (final level in ['INFO', 'notice', 1, null]) {
      expect(
        () => AnnouncementDocument.decode(
          _feed(items: [_item(level: level)]),
          endpoint: _endpoint,
        ),
        throwsA(isA<AnnouncementFormatException>()),
        reason: '$level',
      );
    }
    for (final level in ['info', 'maintenance', 'critical']) {
      expect(
        AnnouncementDocument.decode(
          _feed(items: [_item(level: level)]),
          endpoint: _endpoint,
        ).items.single.level,
        AnnouncementLevel.values.byName(level),
      );
    }
  });

  test('enforces trimmed rune limits on title, body and actionLabel', () {
    expect(
      () => AnnouncementDocument.decode(
        _feed(items: [_item(title: '   ')]),
        endpoint: _endpoint,
      ),
      throwsA(isA<AnnouncementFormatException>()),
    );
    expect(
      AnnouncementDocument.decode(
        _feed(items: [_item(title: '公' * 80)]),
        endpoint: _endpoint,
      ).items.single.title,
      '公' * 80,
    );
    expect(
      () => AnnouncementDocument.decode(
        _feed(items: [_item(title: '公' * 81)]),
        endpoint: _endpoint,
      ),
      throwsA(isA<AnnouncementFormatException>()),
    );
    expect(
      AnnouncementDocument.decode(
        _feed(items: [_item(body: 'a' * 1000)]),
        endpoint: _endpoint,
      ).items.single.body,
      'a' * 1000,
    );
    expect(
      () => AnnouncementDocument.decode(
        _feed(items: [_item(body: 'a' * 1001)]),
        endpoint: _endpoint,
      ),
      throwsA(isA<AnnouncementFormatException>()),
    );
    expect(
      AnnouncementDocument.decode(
        _feed(
          items: [
            _item(
              actionLabel: 'b' * 32,
              actionUrl: 'https://notices.example.edu/x',
            ),
          ],
        ),
        endpoint: _endpoint,
      ).items.single.actionLabel,
      'b' * 32,
    );
    expect(
      () => AnnouncementDocument.decode(
        _feed(
          items: [
            _item(
              actionLabel: 'b' * 33,
              actionUrl: 'https://notices.example.edu/x',
            ),
          ],
        ),
        endpoint: _endpoint,
      ),
      throwsA(isA<AnnouncementFormatException>()),
    );
  });

  test('requires timezone-aware RFC 3339 times in a valid order', () {
    for (final patch in [
      _item(startsAt: '2026-08-18T08:00:00'),
      _item(startsAt: '2026-08-18 08:00:00Z'),
      _item(publishedAt: '2026-08-18T09:00:00Z'),
      _item(
        startsAt: '2026-08-18T08:00:00Z',
        expiresAt: '2026-08-18T08:00:00Z',
      ),
      _item(
        startsAt: '2026-08-19T08:00:00Z',
        expiresAt: '2026-08-18T08:00:00Z',
      ),
    ]) {
      expect(
        () => AnnouncementDocument.decode(
          _feed(items: [patch]),
          endpoint: _endpoint,
        ),
        throwsA(isA<AnnouncementFormatException>()),
        reason: jsonEncode(patch),
      );
    }
  });

  test('requires actionLabel and actionUrl to appear together', () {
    expect(
      () => AnnouncementDocument.decode(
        _feed(items: [_item(actionLabel: '查看详情')]),
        endpoint: _endpoint,
      ),
      throwsA(isA<AnnouncementFormatException>()),
    );
    expect(
      () => AnnouncementDocument.decode(
        _feed(items: [_item(actionUrl: 'https://notices.example.edu/x')]),
        endpoint: _endpoint,
      ),
      throwsA(isA<AnnouncementFormatException>()),
    );
  });

  test('accepts only same-origin HTTPS action URLs', () {
    for (final url in [
      'https://notices.example.edu/x',
      'https://notices.example.edu:443/x',
    ]) {
      expect(
        AnnouncementDocument.decode(
          _feed(
            items: [_item(actionLabel: '查看详情', actionUrl: url)],
          ),
          endpoint: _endpoint,
        ).items.single.actionUrl,
        isNotNull,
        reason: url,
      );
    }
    for (final url in [
      'http://notices.example.edu/x',
      'https://user@notices.example.edu/x',
      'https://notices.example.edu/x#section',
      'https://other.example.edu/x',
      'https://notices.example.edu:8443/x',
      'not a url',
    ]) {
      expect(
        () => AnnouncementDocument.decode(
          _feed(
            items: [_item(actionLabel: '查看详情', actionUrl: url)],
          ),
          endpoint: _endpoint,
        ),
        throwsA(isA<AnnouncementFormatException>()),
        reason: url,
      );
    }
  });

  test('activity window includes startsAt and excludes expiresAt', () {
    final item = AnnouncementDocument.decode(
      _feed(),
      endpoint: _endpoint,
    ).items.single;
    expect(item.isActiveAt(DateTime.utc(2026, 8, 18, 8)), isTrue);
    expect(item.isActiveAt(DateTime.utc(2026, 8, 18, 7, 59, 59)), isFalse);
    expect(item.isActiveAt(DateTime.utc(2026, 8, 20, 17, 59, 59)), isTrue);
    expect(item.isActiveAt(DateTime.utc(2026, 8, 20, 18)), isFalse);
  });

  test('activeAt filters inactive items and sorts by priority', () {
    final document = AnnouncementDocument.decode(
      _feed(
        items: [
          _item(id: 'b-info', publishedAt: '2026-08-18T06:00:00Z'),
          _item(id: 'a-info', publishedAt: '2026-08-18T06:00:00Z'),
          _item(
            id: 'critical-old',
            level: 'critical',
            publishedAt: '2026-08-18T05:00:00Z',
          ),
          _item(
            id: 'maintenance-new',
            level: 'maintenance',
            publishedAt: '2026-08-18T07:00:00Z',
          ),
          _item(
            id: 'critical-new',
            level: 'critical',
            publishedAt: '2026-08-18T08:00:00Z',
          ),
          _item(
            id: 'future',
            startsAt: '2027-01-01T00:00:00Z',
            expiresAt: '2027-01-02T00:00:00Z',
          ),
          _item(id: 'expired', expiresAt: '2026-08-18T11:00:00Z'),
        ],
      ),
      endpoint: _endpoint,
    );

    final active = document.activeAt(DateTime.utc(2026, 8, 18, 12));
    expect(active.map((item) => item.id), [
      'critical-new',
      'critical-old',
      'maintenance-new',
      'a-info',
      'b-info',
    ]);
  });
}
