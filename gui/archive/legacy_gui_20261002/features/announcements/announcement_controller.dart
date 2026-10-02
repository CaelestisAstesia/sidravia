import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/features/announcements/announcement_feed.dart';
import 'package:sidravia_gui/features/announcements/announcement_model.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';

final class AnnouncementController extends ChangeNotifier {
  AnnouncementController({
    required this.endpoint,
    required this.store,
    this.fetcher,
    DateTime Function()? nowUtc,
    this.refreshInterval = const Duration(hours: 6),
  }) : _nowUtc = nowUtc ?? _defaultNowUtc;

  factory AnnouncementController.disabled() =>
      AnnouncementController(endpoint: null, store: MemoryAnnouncementStore());

  static const maxTrackedKeys = 128;

  static DateTime _defaultNowUtc() => DateTime.now().toUtc();

  final Uri? endpoint;
  final AnnouncementStore store;
  final AnnouncementFetcher? fetcher;
  final Duration refreshInterval;
  final DateTime Function() _nowUtc;

  bool _disposed = false;
  bool _started = false;
  int _generation = 0;
  Future<void>? _refreshInFlight;

  AnnouncementDocument? _feed;
  String? _feedJson;
  String? _etag;
  String? _lastModified;
  DateTime? _lastSuccessUtc;
  List<String> _readKeys = const <String>[];
  List<String> _dismissedKeys = const <String>[];

  bool get enabled => endpoint != null;

  List<Announcement> get announcements =>
      _feed?.activeAt(_nowUtc()) ?? const <Announcement>[];

  int get unreadCount =>
      announcements.where((item) => !_readKeys.contains(item.key)).length;

  Announcement? get inlineNotice {
    for (final item in announcements) {
      if (item.level == AnnouncementLevel.info) continue;
      if (_readKeys.contains(item.key)) continue;
      if (_dismissedKeys.contains(item.key)) continue;
      return item;
    }
    return null;
  }

  Future<void> start() {
    if (!enabled || _started) return Future<void>.value();
    _started = true;
    final generation = _generation;
    return _refreshInFlight = _startFlow(generation)
        .whenComplete(() => _refreshInFlight = null);
  }

  Future<void> refreshIfDue() {
    if (!enabled || !_started || _disposed) return Future<void>.value();
    final inFlight = _refreshInFlight;
    if (inFlight != null) return inFlight;
    final lastSuccess = _lastSuccessUtc;
    if (lastSuccess != null &&
        _nowUtc().difference(lastSuccess) < refreshInterval) {
      return Future<void>.value();
    }
    final generation = _generation;
    return _refreshInFlight = _revalidate(generation)
        .whenComplete(() => _refreshInFlight = null);
  }

  void markCurrentRead() {
    if (!enabled || _disposed) return;
    final current = announcements;
    if (current.isEmpty) return;
    final keys = List<String>.of(_readKeys);
    var changed = false;
    for (final item in current) {
      changed = _trackKey(keys, item.key) || changed;
    }
    if (!changed) return;
    _readKeys = List.unmodifiable(keys);
    _persist();
    _notify();
  }

  void dismissInline(String key) {
    if (!enabled || _disposed) return;
    final keys = List<String>.of(_dismissedKeys);
    if (!_trackKey(keys, key)) return;
    _dismissedKeys = List.unmodifiable(keys);
    _persist();
    _notify();
  }

  static bool _trackKey(List<String> keys, String key) {
    if (keys.isNotEmpty && keys.first == key) return false;
    keys.remove(key);
    keys.insert(0, key);
    if (keys.length > maxTrackedKeys) {
      keys.removeRange(maxTrackedKeys, keys.length);
    }
    return true;
  }

  Future<void> _startFlow(int generation) async {
    await _restore(generation);
    await _revalidate(generation);
  }

  Future<void> _restore(int generation) async {
    AnnouncementCache? cache;
    try {
      cache = await store.read();
    } on Object {
      cache = null;
    }
    if (!_current(generation) || cache == null) return;
    try {
      final feed = AnnouncementDocument.decode(
        cache.feedJson,
        endpoint: endpoint!,
      );
      _feed = feed;
      _feedJson = cache.feedJson;
      _etag = cache.etag;
      _lastModified = cache.lastModified;
      _lastSuccessUtc = cache.lastSuccessUtc;
      _readKeys = List.unmodifiable(cache.readKeys.take(maxTrackedKeys));
      _dismissedKeys = List.unmodifiable(
        cache.dismissedKeys.take(maxTrackedKeys),
      );
      _notify();
    } on FormatException {
      // A corrupted cache is dropped; revalidation proceeds without validators.
    }
  }

  Future<void> _revalidate(int generation) async {
    final fetcher = this.fetcher;
    if (fetcher == null || !_current(generation)) return;
    try {
      final result = await fetcher.fetch(
        etag: _etag,
        lastModified: _lastModified,
      );
      if (!_current(generation)) return;
      switch (result.outcome) {
        case AnnouncementFetchOutcome.updated:
          _feed = result.feed;
          _feedJson = result.rawBody;
          _etag = result.etag;
          _lastModified = result.lastModified;
          _lastSuccessUtc = _nowUtc();
          _persist();
          _notify();
        case AnnouncementFetchOutcome.notModified:
          // A 304 without a usable cache is a non-fatal fetch failure.
          if (_feed == null) return;
          _etag = result.etag ?? _etag;
          _lastModified = result.lastModified ?? _lastModified;
          _lastSuccessUtc = _nowUtc();
          _persist();
          _notify();
      }
    } on Object {
      // A fetch failure is non-fatal: the unexpired cache projection stays.
    }
  }

  void _persist() {
    final feedJson = _feedJson;
    final lastSuccess = _lastSuccessUtc;
    if (feedJson == null || lastSuccess == null) return;
    unawaited(_persistSafely(feedJson, lastSuccess));
  }

  Future<void> _persistSafely(String feedJson, DateTime lastSuccess) async {
    try {
      await store.write(
        AnnouncementCache(
          feedJson: feedJson,
          lastSuccessUtc: lastSuccess,
          etag: _etag,
          lastModified: _lastModified,
          readKeys: _readKeys,
          dismissedKeys: _dismissedKeys,
        ),
      );
    } on Object {
      // A store failure only means this state does not survive the process.
    }
  }

  bool _current(int generation) => !_disposed && generation == _generation;

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    fetcher?.close();
    super.dispose();
  }
}
