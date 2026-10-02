import 'dart:convert';

import 'package:flutter/foundation.dart';

enum AnnouncementLevel { info, maintenance, critical }

final class AnnouncementFormatException extends FormatException {
  const AnnouncementFormatException(super.message);
}

@immutable
final class Announcement {
  const Announcement({
    required this.id,
    required this.revision,
    required this.level,
    required this.title,
    required this.body,
    required this.publishedAt,
    required this.startsAt,
    required this.expiresAt,
    this.actionLabel,
    this.actionUrl,
  });

  final String id;
  final int revision;
  final AnnouncementLevel level;
  final String title;
  final String body;
  final DateTime publishedAt;
  final DateTime startsAt;
  final DateTime expiresAt;
  final String? actionLabel;
  final Uri? actionUrl;

  String get key => '$id@$revision';

  bool isActiveAt(DateTime nowUtc) =>
      !nowUtc.isBefore(startsAt) && nowUtc.isBefore(expiresAt);
}

@immutable
final class AnnouncementDocument {
  const AnnouncementDocument._({
    required this.generatedAt,
    required this.items,
  });

  static const maxItems = 20;

  static final _idPattern = RegExp(r'^[a-z0-9][a-z0-9._-]{0,63}$');
  static final _rfc3339Pattern = RegExp(
    r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$',
  );

  final DateTime generatedAt;
  final List<Announcement> items;

  static AnnouncementDocument decode(String source, {required Uri endpoint}) {
    final Object? root;
    try {
      root = jsonDecode(source);
    } on FormatException {
      throw const AnnouncementFormatException('announcement feed is not JSON');
    }
    if (root is! Map<String, dynamic>) {
      throw const AnnouncementFormatException(
        'announcement feed root must be an object',
      );
    }
    final schema = root['schema'];
    if (schema is! int || schema != 1) {
      throw const AnnouncementFormatException(
        'unsupported announcement feed schema',
      );
    }
    final generatedAt = _decodeTime(root['generatedAt'], 'generatedAt');
    final rawItems = root['items'];
    if (rawItems is! List) {
      throw const AnnouncementFormatException(
        'announcement feed items must be a list',
      );
    }
    if (rawItems.length > maxItems) {
      throw const AnnouncementFormatException(
        'announcement feed carries too many items',
      );
    }
    final seenIds = <String>{};
    final items = <Announcement>[];
    for (final raw in rawItems) {
      if (raw is! Map<String, dynamic>) {
        throw const AnnouncementFormatException(
          'announcement item must be an object',
        );
      }
      final item = _decodeItem(raw, endpoint);
      if (!seenIds.add(item.id)) {
        throw const AnnouncementFormatException(
          'announcement feed repeats an item id',
        );
      }
      items.add(item);
    }
    return AnnouncementDocument._(
      generatedAt: generatedAt,
      items: List.unmodifiable(items),
    );
  }

  List<Announcement> activeAt(DateTime nowUtc) {
    final active = items.where((item) => item.isActiveAt(nowUtc)).toList();
    active.sort((a, b) {
      final rank = _levelRank(a.level).compareTo(_levelRank(b.level));
      if (rank != 0) return rank;
      final published = b.publishedAt.compareTo(a.publishedAt);
      if (published != 0) return published;
      return a.id.compareTo(b.id);
    });
    return List.unmodifiable(active);
  }

  static int _levelRank(AnnouncementLevel level) => switch (level) {
    AnnouncementLevel.critical => 0,
    AnnouncementLevel.maintenance => 1,
    AnnouncementLevel.info => 2,
  };

  static Announcement _decodeItem(Map<String, dynamic> raw, Uri endpoint) {
    final id = raw['id'];
    if (id is! String || !_idPattern.hasMatch(id)) {
      throw const AnnouncementFormatException('announcement id is invalid');
    }
    final revision = raw['revision'];
    if (revision is! int || revision < 1) {
      throw const AnnouncementFormatException(
        'announcement revision is invalid',
      );
    }
    final level = switch (raw['level']) {
      'info' => AnnouncementLevel.info,
      'maintenance' => AnnouncementLevel.maintenance,
      'critical' => AnnouncementLevel.critical,
      _ => throw const AnnouncementFormatException(
        'announcement level is invalid',
      ),
    };
    final title = _decodeText(raw['title'], 'title', 80);
    final body = _decodeText(raw['body'], 'body', 1000);
    final publishedAt = _decodeTime(raw['publishedAt'], 'publishedAt');
    final startsAt = _decodeTime(raw['startsAt'], 'startsAt');
    final expiresAt = _decodeTime(raw['expiresAt'], 'expiresAt');
    if (publishedAt.isAfter(startsAt) || !expiresAt.isAfter(startsAt)) {
      throw const AnnouncementFormatException(
        'announcement time order is invalid',
      );
    }
    final rawLabel = raw['actionLabel'];
    final rawUrl = raw['actionUrl'];
    if ((rawLabel == null) != (rawUrl == null)) {
      throw const AnnouncementFormatException(
        'announcement action fields must appear together',
      );
    }
    String? actionLabel;
    Uri? actionUrl;
    if (rawLabel != null) {
      actionLabel = _decodeText(rawLabel, 'actionLabel', 32);
      actionUrl = _decodeActionUrl(rawUrl, endpoint);
    }
    return Announcement(
      id: id,
      revision: revision,
      level: level,
      title: title,
      body: body,
      publishedAt: publishedAt,
      startsAt: startsAt,
      expiresAt: expiresAt,
      actionLabel: actionLabel,
      actionUrl: actionUrl,
    );
  }

  static String _decodeText(Object? value, String field, int maxRunes) {
    if (value is! String) {
      throw AnnouncementFormatException('announcement $field is invalid');
    }
    final trimmed = value.trim();
    final length = trimmed.runes.length;
    if (length < 1 || length > maxRunes) {
      throw AnnouncementFormatException('announcement $field is invalid');
    }
    return trimmed;
  }

  static DateTime _decodeTime(Object? value, String field) {
    if (value is! String || !_rfc3339Pattern.hasMatch(value)) {
      throw AnnouncementFormatException('announcement $field is invalid');
    }
    final parsed = DateTime.tryParse(value);
    if (parsed == null) {
      throw AnnouncementFormatException('announcement $field is invalid');
    }
    return parsed.toUtc();
  }

  static Uri _decodeActionUrl(Object? value, Uri endpoint) {
    final uri = value is String ? Uri.tryParse(value) : null;
    if (uri == null ||
        uri.scheme != 'https' ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.fragment.isNotEmpty ||
        uri.scheme != endpoint.scheme ||
        uri.host != endpoint.host ||
        uri.port != endpoint.port) {
      throw const AnnouncementFormatException(
        'announcement action URL is invalid',
      );
    }
    return uri;
  }
}
