part of 'ipc_models.dart';

class AnnouncementMetadata {
  const AnnouncementMetadata({
    required this.url,
    required this.clientWidth,
    required this.clientHeight,
  });
  final String url;
  final int clientWidth, clientHeight;
}

AnnouncementMetadata? _announcement(Object? raw) {
  if (raw == null) return null;
  final value = _object(raw, const {'url', 'clientWidth', 'clientHeight'});
  final url = _text(value['url']);
  final width = _positive(value['clientWidth']);
  final height = _positive(value['clientHeight']);
  final invalidRune = url.runes.any(
    (r) =>
        r <= 0x20 ||
        (r >= 0x7f && r <= 0x9f) ||
        const {
          0xa0,
          0x1680,
          0x2028,
          0x2029,
          0x202f,
          0x205f,
          0x3000,
        }.contains(r) ||
        (r >= 0x2000 && r <= 0x200a),
  );
  final uri = Uri.tryParse(url);
  if (invalidRune ||
      RegExp(r'%(?![0-9a-fA-F]{2})').hasMatch(url) ||
      uri == null ||
      !uri.isAbsolute ||
      !const {'http', 'https'}.contains(uri.scheme) ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      RegExp(r'^https?://[^/?#]*@', caseSensitive: false).hasMatch(url) ||
      uri.hasFragment ||
      width > 4096 ||
      height > 4096) {
    throw const IpcProtocolException();
  }
  return AnnouncementMetadata(
    url: url,
    clientWidth: width,
    clientHeight: height,
  );
}

class InstitutionProfile {
  const InstitutionProfile({
    required this.id,
    required this.displayName,
    required this.protocolId,
    this.announcement,
    this.overrideSchemaVersion = 0,
  });
  final String id, displayName, protocolId;
  final int overrideSchemaVersion;
  final AnnouncementMetadata? announcement;
}

List<InstitutionProfile> decodeProfiles(String source) =>
    _list(decodeObject(source, const {'profiles'})['profiles'])
        .map((raw) {
          if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
          final v = _object(raw, {
            'institutionProfileId',
            'displayName',
            'authenticationProtocolId',
            if (raw.containsKey('announcement')) 'announcement',
            if (raw.containsKey('overrideSchemaVersion'))
              'overrideSchemaVersion',
          });
          return InstitutionProfile(
            id: _text(v['institutionProfileId']),
            displayName: _text(v['displayName']),
            protocolId: _text(v['authenticationProtocolId']),
            announcement: _announcement(v['announcement']),
            overrideSchemaVersion: v.containsKey('overrideSchemaVersion')
                ? _positive(v['overrideSchemaVersion'])
                : 0,
          );
        })
        .toList(growable: false);
