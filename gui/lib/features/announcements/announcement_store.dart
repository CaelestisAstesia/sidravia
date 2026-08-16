import 'package:shared_preferences/shared_preferences.dart';

final class AnnouncementCache {
  const AnnouncementCache({
    required this.feedJson,
    required this.lastSuccessUtc,
    this.etag,
    this.lastModified,
    this.readKeys = const <String>[],
    this.dismissedKeys = const <String>[],
  });

  final String feedJson;
  final DateTime lastSuccessUtc;
  final String? etag;
  final String? lastModified;
  final List<String> readKeys;
  final List<String> dismissedKeys;
}

abstract interface class AnnouncementStore {
  Future<AnnouncementCache?> read();
  Future<void> write(AnnouncementCache cache);
}

final class MemoryAnnouncementStore implements AnnouncementStore {
  MemoryAnnouncementStore([this._value]);

  AnnouncementCache? _value;

  @override
  Future<AnnouncementCache?> read() async => _value;

  @override
  Future<void> write(AnnouncementCache cache) async => _value = cache;
}

final class SharedPreferencesAnnouncementStore implements AnnouncementStore {
  SharedPreferencesAnnouncementStore(this._preferences);

  static const _feedJsonKey = 'announcements.feed_json';
  static const _etagKey = 'announcements.etag';
  static const _lastModifiedKey = 'announcements.last_modified';
  static const _lastSuccessKey = 'announcements.last_success_utc';
  static const _readKeysKey = 'announcements.read_keys';
  static const _dismissedKeysKey = 'announcements.dismissed_keys';

  final SharedPreferencesAsync _preferences;

  @override
  Future<AnnouncementCache?> read() async {
    final feedJson = await _preferences.getString(_feedJsonKey);
    if (feedJson == null) return null;
    final lastSuccessMillis = await _preferences.getInt(_lastSuccessKey) ?? 0;
    return AnnouncementCache(
      feedJson: feedJson,
      lastSuccessUtc: DateTime.fromMillisecondsSinceEpoch(
        lastSuccessMillis,
        isUtc: true,
      ),
      etag: await _preferences.getString(_etagKey),
      lastModified: await _preferences.getString(_lastModifiedKey),
      readKeys:
          await _preferences.getStringList(_readKeysKey) ?? const <String>[],
      dismissedKeys:
          await _preferences.getStringList(_dismissedKeysKey) ??
          const <String>[],
    );
  }

  @override
  Future<void> write(AnnouncementCache cache) async {
    await _preferences.setString(_feedJsonKey, cache.feedJson);
    await _preferences.setInt(
      _lastSuccessKey,
      cache.lastSuccessUtc.millisecondsSinceEpoch,
    );
    await _setNullableString(_etagKey, cache.etag);
    await _setNullableString(_lastModifiedKey, cache.lastModified);
    await _preferences.setStringList(_readKeysKey, cache.readKeys);
    await _preferences.setStringList(_dismissedKeysKey, cache.dismissedKeys);
  }

  Future<void> _setNullableString(String key, String? value) => value == null
      ? _preferences.remove(key)
      : _preferences.setString(key, value);
}
