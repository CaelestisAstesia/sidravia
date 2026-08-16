import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;
import 'package:sidravia_gui/features/announcements/announcement_model.dart';

final class AnnouncementFetchException implements Exception {
  const AnnouncementFetchException(this.message, [this.cause]);

  final String message;
  final Object? cause;

  @override
  String toString() => 'AnnouncementFetchException: $message';
}

enum AnnouncementFetchOutcome { updated, notModified }

final class AnnouncementFetchResult {
  const AnnouncementFetchResult._({
    required this.outcome,
    this.feed,
    this.rawBody,
    this.etag,
    this.lastModified,
  });

  factory AnnouncementFetchResult.updated({
    required AnnouncementDocument feed,
    required String rawBody,
    String? etag,
    String? lastModified,
  }) => AnnouncementFetchResult._(
    outcome: AnnouncementFetchOutcome.updated,
    feed: feed,
    rawBody: rawBody,
    etag: etag,
    lastModified: lastModified,
  );

  factory AnnouncementFetchResult.notModified({
    String? etag,
    String? lastModified,
  }) => AnnouncementFetchResult._(
    outcome: AnnouncementFetchOutcome.notModified,
    etag: etag,
    lastModified: lastModified,
  );

  final AnnouncementFetchOutcome outcome;
  final AnnouncementDocument? feed;
  final String? rawBody;
  final String? etag;
  final String? lastModified;
}

abstract interface class AnnouncementFetcher {
  Future<AnnouncementFetchResult> fetch({String? etag, String? lastModified});
  void close();
}

Uri? parseAnnouncementEndpoint(String raw) {
  final trimmed = raw.trim();
  if (trimmed.isEmpty) return null;
  final uri = Uri.tryParse(trimmed);
  if (uri == null ||
      uri.scheme != 'https' ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.fragment.isNotEmpty) {
    return null;
  }
  return uri;
}

final class HttpAnnouncementFetcher implements AnnouncementFetcher {
  HttpAnnouncementFetcher({
    required this.endpoint,
    required this.client,
    this.timeout = const Duration(seconds: 5),
  });

  static const maxBodyBytes = 65536;

  final Uri endpoint;
  final http.Client client;
  final Duration timeout;

  @override
  Future<AnnouncementFetchResult> fetch({String? etag, String? lastModified}) {
    return _fetch(etag: etag, lastModified: lastModified).timeout(
      timeout,
      onTimeout: () => throw const AnnouncementFetchException(
        'announcement fetch timed out',
      ),
    );
  }

  Future<AnnouncementFetchResult> _fetch({
    String? etag,
    String? lastModified,
  }) async {
    final request = http.Request('GET', endpoint)
      ..followRedirects = false
      ..headers['accept'] = 'application/json'
      ..headers['accept-encoding'] = 'identity';
    if (etag != null) request.headers['if-none-match'] = etag;
    if (lastModified != null) {
      request.headers['if-modified-since'] = lastModified;
    }
    final http.StreamedResponse response;
    try {
      response = await client.send(request);
    } on Object catch (error) {
      throw AnnouncementFetchException('announcement request failed', error);
    }
    if (response.statusCode == 304) {
      await response.stream.drain<void>();
      return AnnouncementFetchResult.notModified(
        etag: response.headers['etag'],
        lastModified: response.headers['last-modified'],
      );
    }
    if (response.statusCode != 200) {
      await response.stream.drain<void>();
      throw AnnouncementFetchException(
        'unexpected announcement status ${response.statusCode}',
      );
    }
    final mediaType = response.headers['content-type']
        ?.split(';')
        .first
        .trim()
        .toLowerCase();
    if (mediaType != 'application/json') {
      await response.stream.drain<void>();
      throw const AnnouncementFetchException(
        'unexpected announcement content type',
      );
    }
    final contentEncoding = response.headers['content-encoding']
        ?.trim()
        .toLowerCase();
    if (contentEncoding != null && contentEncoding != 'identity') {
      await response.stream.drain<void>();
      throw const AnnouncementFetchException(
        'compressed announcements are not accepted',
      );
    }
    final builder = BytesBuilder(copy: false);
    await for (final chunk in response.stream) {
      builder.add(chunk);
      if (builder.length > maxBodyBytes) {
        throw const AnnouncementFetchException(
          'announcement response is too large',
        );
      }
    }
    final body = utf8.decode(builder.takeBytes());
    final feed = AnnouncementDocument.decode(body, endpoint: endpoint);
    return AnnouncementFetchResult.updated(
      feed: feed,
      rawBody: body,
      etag: response.headers['etag'],
      lastModified: response.headers['last-modified'],
    );
  }

  @override
  void close() => client.close();
}
