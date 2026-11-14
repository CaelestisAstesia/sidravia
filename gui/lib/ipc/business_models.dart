import 'dart:convert';

import 'package:sidravia_gui/ipc/ipc_models.dart';

class AccountStatistics {
  const AccountStatistics(
    this.receivedAt,
    this.trafficBytes,
    this.balanceCents,
  );
  final DateTime receivedAt;
  final int? trafficBytes, balanceCents;
}

class ServerTextMessage {
  const ServerTextMessage(this.id, this.receivedAt, this.text);
  final String id, text;
  final DateTime receivedAt;
}

class BusinessSnapshot {
  const BusinessSnapshot({
    required this.sessionId,
    required this.runGeneration,
    required this.revision,
    required this.current,
    required this.statisticsSupported,
    required this.textSupported,
    required this.statistics,
    required this.messages,
    required this.updatedAt,
  });
  final String sessionId;
  final BigInt runGeneration, revision;
  final bool current, statisticsSupported, textSupported;
  final AccountStatistics? statistics;
  final List<ServerTextMessage> messages;
  final DateTime updatedAt;
}

Map<String, dynamic> _object(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      !raw.keys.toSet().containsAll(keys)) {
    throw const IpcProtocolException();
  }
  return raw;
}

DateTime _time(Object? raw) {
  if (raw is! String ||
      !RegExp(
        r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$',
      ).hasMatch(raw)) {
    throw const IpcProtocolException();
  }
  final value = DateTime.tryParse(raw);
  if (value == null) throw const IpcProtocolException();
  // DateTime accepts overflowing calendar components; reject those explicitly.
  final pieces = raw
      .substring(0, 19)
      .split(RegExp(r'[-T:]'))
      .map(int.parse)
      .toList();
  final calendar = DateTime.utc(
    pieces[0],
    pieces[1],
    pieces[2],
    pieces[3],
    pieces[4],
    pieces[5],
  );
  if (calendar.year != pieces[0] ||
      calendar.month != pieces[1] ||
      calendar.day != pieces[2] ||
      pieces[3] > 23 ||
      pieces[4] > 59 ||
      pieces[5] > 59) {
    throw const IpcProtocolException();
  }
  if (!raw.endsWith('Z')) {
    final offset = raw
        .substring(raw.length - 5)
        .split(':')
        .map(int.parse)
        .toList();
    if (offset[0] > 23 || offset[1] > 59) throw const IpcProtocolException();
  }
  return value;
}

BigInt _uint(Object? value, {bool positive = false}) {
  final n = value is BigInt
      ? value
      : value is int
      ? BigInt.from(value)
      : null;
  if (n == null ||
      n < BigInt.zero ||
      n > BigInt.parse('18446744073709551615') ||
      positive && n == BigInt.zero) {
    throw const IpcProtocolException();
  }
  return n;
}

int? _optionalUint(Object? v, int max) {
  if (v == null) return null;
  if (v is! int || v < 0 || v > max) throw const IpcProtocolException();
  return v;
}

bool _bool(Object? v) {
  if (v is! bool) throw const IpcProtocolException();
  return v;
}

bool validBusinessText(String text) =>
    text.isNotEmpty &&
    text.trim() == text &&
    utf8.encode(text).length <= 512 &&
    !RegExp(
      r'[\x00-\x09\x0b-\x1f\x7f-\x9f\u00ad\u0600-\u0605\u061c\u06dd\u070f\u0890\u0891\u08e2\u180e\u200b-\u200f\u202a-\u202e\u2060-\u2064\u2066-\u206f\ufeff\ufff9-\ufffb]',
      unicode: true,
    ).hasMatch(text) &&
    !text.runes.any(
      (r) =>
          r >= 0xd800 && r <= 0xdfff ||
          r == 0x110bd ||
          r == 0x110cd ||
          r >= 0x13430 && r <= 0x1343f ||
          r >= 0x1bca0 && r <= 0x1bca3 ||
          r >= 0x1d173 && r <= 0x1d17a ||
          r == 0xe0001 ||
          r >= 0xe0020 && r <= 0xe007f,
    );
BusinessSnapshot decodeBusiness(String source) {
  if (utf8.encode(source).length > 60 * 1024) {
    throw const IpcProtocolException();
  }
  return decodeBusinessValue(
    decodeBindingCheckedJson(
      source,
      preserveUnsignedRevision: true,
      preserveRunGeneration: true,
    ),
  );
}

BusinessSnapshot decodeBusinessValue(Object? raw) {
  final v = _object(raw, {
    'sessionId',
    'runGeneration',
    'revision',
    'current',
    'statisticsSupported',
    'textSupported',
    'statistics',
    'messages',
    'updatedAt',
  });
  final id = v['sessionId'];
  if (id is! String || id.isEmpty) throw const IpcProtocolException();
  final generation = _uint(v['runGeneration']),
      revision = _uint(v['revision'], positive: true);
  final current = _bool(v['current']),
      statsSupported = _bool(v['statisticsSupported']),
      textSupported = _bool(v['textSupported']);
  AccountStatistics? statistics;
  if (v['statistics'] != null) {
    final s = _object(v['statistics'], {
      'receivedAt',
      'source',
      'scope',
      'trafficBytes',
      'balanceCents',
    });
    if (s['source'] != 'server_login' ||
        s['scope'] != 'server_billing_history' ||
        !statsSupported) {
      throw const IpcProtocolException();
    }
    statistics = AccountStatistics(
      _time(s['receivedAt']),
      _optionalUint(s['trafficBytes'], 4398046510080),
      _optionalUint(s['balanceCents'], 2147483647),
    );
  }
  final rawMessages = v['messages'];
  if (rawMessages is! List ||
      rawMessages.length > 16 ||
      !textSupported && rawMessages.isNotEmpty) {
    throw const IpcProtocolException();
  }
  final messages = <ServerTextMessage>[];
  BigInt previous = BigInt.zero;
  for (final raw in rawMessages) {
    final m = _object(raw, {'id', 'receivedAt', 'text'});
    final mid = m['id'], text = m['text'];
    final number = mid is String ? BigInt.tryParse(mid) : null;
    if (number == null ||
        number <= previous ||
        number.toString() != mid ||
        number > BigInt.parse('18446744073709551615') ||
        text is! String ||
        !validBusinessText(text)) {
      throw const IpcProtocolException();
    }
    previous = number;
    messages.add(
      ServerTextMessage(mid as String, _time(m['receivedAt']), text),
    );
  }
  if (generation == BigInt.zero &&
      (current || statistics != null || messages.isNotEmpty)) {
    throw const IpcProtocolException();
  }
  return BusinessSnapshot(
    sessionId: id,
    runGeneration: generation,
    revision: revision,
    current: current,
    statisticsSupported: statsSupported,
    textSupported: textSupported,
    statistics: statistics,
    messages: List.unmodifiable(messages),
    updatedAt: _time(v['updatedAt']),
  );
}
