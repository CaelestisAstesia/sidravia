part of 'ipc_models.dart';

// Scan raw JSON before maps erase duplicates. Object keys are decoded so
// escaped and literal aliases coincide; each object owns its own key set.
Object? decodeBindingCheckedJson(
  String source, {
  bool preserveUnsignedRevision = false,
  bool preserveRunGeneration = false,
  bool strictUnsignedNumbers = false,
}) {
  try {
    var i = 0;
    final strictUnicode =
        preserveUnsignedRevision ||
        preserveRunGeneration ||
        strictUnsignedNumbers;
    void whitespace() {
      while (i < source.length &&
          const [9, 10, 13, 32].contains(source.codeUnitAt(i))) {
        i++;
      }
    }

    String string() {
      final start = i;
      if (i >= source.length || source[i] != '"') {
        throw const IpcProtocolException();
      }
      i++;
      while (i < source.length) {
        if (source[i] == '\\') {
          i += 2;
          continue;
        }
        if (source[i++] == '"') {
          final text = jsonDecode(source.substring(start, i)) as String;
          if (strictUnicode &&
              text.runes.any((r) => r >= 0xd800 && r <= 0xdfff)) {
            throw const IpcProtocolException();
          }
          return text;
        }
      }
      throw const IpcProtocolException();
    }

    late Object? Function([String?]) value;
    value = ([String? key]) {
      whitespace();
      if (i >= source.length) {
        throw const IpcProtocolException();
      }
      final exact =
          preserveUnsignedRevision && key == 'revision' ||
          preserveRunGeneration && key == 'runGeneration';
      if (source[i] == '{') {
        if (exact) {
          throw const IpcProtocolException();
        }
        i++;
        whitespace();
        final result = <String, dynamic>{};
        if (i < source.length && source[i] == '}') {
          i++;
          return result;
        }
        while (true) {
          whitespace();
          final name = string();
          if (result.containsKey(name)) {
            throw const IpcProtocolException();
          }
          whitespace();
          if (i >= source.length || source[i++] != ':') {
            throw const IpcProtocolException();
          }
          whitespace();
          final valueStart = i;
          result[name] = value(name);
          if (strictUnsignedNumbers &&
              name == 'result' &&
              utf8.encode(source.substring(valueStart, i)).length >
                  maximumDiagnosticExportBytes) {
            throw const IpcProtocolException();
          }
          whitespace();
          if (i >= source.length) {
            throw const IpcProtocolException();
          }
          final next = source[i++];
          if (next == '}') {
            return result;
          }
          if (next != ',') {
            throw const IpcProtocolException();
          }
        }
      }
      if (source[i] == '[') {
        if (exact) {
          throw const IpcProtocolException();
        }
        i++;
        whitespace();
        final result = <dynamic>[];
        if (i < source.length && source[i] == ']') {
          i++;
          return result;
        }
        while (true) {
          result.add(value());
          whitespace();
          if (i >= source.length) {
            throw const IpcProtocolException();
          }
          final next = source[i++];
          if (next == ']') {
            return result;
          }
          if (next != ',') {
            throw const IpcProtocolException();
          }
        }
      }
      if (source[i] == '"') {
        if (exact) {
          throw const IpcProtocolException();
        }
        return string();
      }
      final start = i;
      while (i < source.length &&
          !const [',', '}', ']', ' ', '\t', '\n', '\r'].contains(source[i])) {
        i++;
      }
      if (start == i) {
        throw const IpcProtocolException();
      }
      final token = source.substring(start, i);
      if (exact) {
        if (!RegExp(r'^(0|[1-9][0-9]*)$').hasMatch(token)) {
          throw const IpcProtocolException();
        }
        final n = BigInt.parse(token);
        if (n > BigInt.parse('18446744073709551615')) {
          throw const IpcProtocolException();
        }
        return n;
      }
      if (strictUnsignedNumbers &&
          RegExp(r'^[-0-9]').hasMatch(token) &&
          !RegExp(r'^(0|[1-9][0-9]*)$').hasMatch(token)) {
        throw const IpcProtocolException();
      }
      if (strictUnicode && token == '-0') {
        throw const IpcProtocolException();
      }
      return jsonDecode(token);
    };
    final decoded = value();
    whitespace();
    if (i != source.length) {
      throw const IpcProtocolException();
    }
    return decoded;
  } on FormatException {
    throw const IpcProtocolException();
  }
}

class IpcProtocolException implements Exception {
  const IpcProtocolException();
}

/// Stable transport diagnostics; never contains socket text, endpoints or tokens.
class IpcTransportException implements Exception {
  const IpcTransportException(this.code);
  final String code;
}

class IpcRequestFailure implements Exception {
  const IpcRequestFailure(this.code);
  final String code;
}

Map<String, dynamic> decodeObject(String source, Set<String> keys) =>
    _object(decodeBindingCheckedJson(source), keys);

Map<String, dynamic> _object(Object? value, Set<String> keys) {
  if (value is! Map<String, dynamic> ||
      value.length != keys.length ||
      !value.keys.toSet().containsAll(keys)) {
    throw const IpcProtocolException();
  }
  return value;
}

List<dynamic> _list(Object? value) {
  if (value is! List<dynamic>) throw const IpcProtocolException();
  return value;
}

String _text(Object? value) {
  if (value is! String || value.isEmpty) throw const IpcProtocolException();
  return value;
}

String _optionalText(Object? value) {
  if (value is! String) throw const IpcProtocolException();
  return value;
}

bool _bool(Object? value) {
  if (value is! bool) throw const IpcProtocolException();
  return value;
}

int _positive(Object? value) {
  if (value is! int || value <= 0) throw const IpcProtocolException();
  return value;
}

DateTime _time(Object? value) {
  final text = _text(value);
  final match = RegExp(
    r'^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?(?:Z|[+-](\d\d):(\d\d))$',
  ).firstMatch(text);
  if (match == null) {
    throw const IpcProtocolException();
  }
  final year = int.parse(match[1]!);
  final month = int.parse(match[2]!);
  final day = int.parse(match[3]!);
  final hour = int.parse(match[4]!);
  final minute = int.parse(match[5]!);
  final second = int.parse(match[6]!);
  final offsetHour = match[7] == null ? null : int.parse(match[7]!);
  final offsetMinute = match[8] == null ? null : int.parse(match[8]!);
  final daysInMonth = switch (month) {
    1 || 3 || 5 || 7 || 8 || 10 || 12 => 31,
    4 || 6 || 9 || 11 => 30,
    2 when year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) => 29,
    2 => 28,
    _ => 0,
  };
  if (day < 1 ||
      day > daysInMonth ||
      hour > 23 ||
      minute > 59 ||
      second > 59 ||
      (offsetHour != null && offsetHour > 23) ||
      (offsetMinute != null && offsetMinute > 59)) {
    throw const IpcProtocolException();
  }
  final parsed = DateTime.tryParse(text);
  if (parsed == null) throw const IpcProtocolException();
  return parsed;
}
