import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

const _completeConfigurationWire =
    r'{"configurationId":"c","displayName":"","institutionProfileId":"i","institutionDisplayName":"Institution","authenticationProtocolId":"p","username":"u","credentialStored":true,"storageProtection":"protected","autoLogin":false,"autoReconnect":false,"networkBindingPolicy":{"mode":"automatically_select_latest_available"},"runtimeAvailability":"available"}';

void main() {
  test('school announcement optional metadata and strict payload', () {
    const base = {
      'institutionProfileId': 'jlu',
      'displayName': '吉林大学',
      'authenticationProtocolId': 'drcom-5.2.0-d',
    };
    String wire(Object? announcement) => jsonEncode({
      'profiles': [
        {...base, 'announcement': announcement},
      ],
    });
    expect(
      decodeProfiles(
        jsonEncode({
          'profiles': [base],
        }),
      ).single.announcement,
      isNull,
    );
    expect(decodeProfiles(wire(null)).single.announcement, isNull);
    for (final url in [
      'http://login.jlu.edu.cn/notice_win.php',
      'https://school.example/notice?q=1',
    ]) {
      final profile = decodeProfiles(
        wire({'url': url, 'clientWidth': 600, 'clientHeight': 460}),
      ).single;
      expect(profile.id, 'jlu');
      expect(profile.announcement!.url, url);
      expect(profile.announcement!.clientWidth, 600);
      expect(profile.announcement!.clientHeight, 460);
    }
    for (final url in [
      '',
      'notice.php',
      '//school.example/notice',
      'ftp://school.example',
      'http://',
      'http://:80',
      'http://user:password@school.example',
      'http://@school.example',
      'http://school.example/#',
      'http://school.example/#notice',
      'http://school.example/a b',
      'http://school.example/\n',
      'http://school.example/\u0085',
      'http://school.example/\u2003',
      'http://school.example/\u007f',
      'http://school.example/%zz',
    ]) {
      expect(
        () => decodeProfiles(
          wire({'url': url, 'clientWidth': 600, 'clientHeight': 460}),
        ),
        throwsA(isA<IpcProtocolException>()),
        reason: url,
      );
    }
    for (final metadata in [
      {},
      [],
      true,
      'notice',
      {'URL': 'http://school.example', 'clientWidth': 600, 'clientHeight': 460},
      {'url': 'http://school.example', 'ClientWidth': 600, 'clientHeight': 460},
      {'url': 'http://school.example', 'clientWidth': 600, 'ClientHeight': 460},
      {
        'url': 'http://school.example',
        'URL': 'http://other.example',
        'clientWidth': 600,
        'clientHeight': 460,
      },
      {'clientWidth': 600, 'clientHeight': 460},
      {'url': 'http://school.example', 'clientHeight': 460},
      {'url': 'http://school.example', 'clientWidth': 600},
      {'url': null, 'clientWidth': 600, 'clientHeight': 460},
      {'url': true, 'clientWidth': 600, 'clientHeight': 460},
      for (final width in [null, '600', 600.5, 0, -1, 4097])
        {
          'url': 'http://school.example',
          'clientWidth': width,
          'clientHeight': 460,
        },
      for (final height in [false, 0, -1, 4097])
        {
          'url': 'http://school.example',
          'clientWidth': 600,
          'clientHeight': height,
        },
      {
        'url': 'http://school.example',
        'clientWidth': 600,
        'clientHeight': 460,
        'extra': true,
      },
    ]) {
      expect(
        () => decodeProfiles(wire(metadata)),
        throwsA(isA<IpcProtocolException>()),
        reason: '$metadata',
      );
    }
    const valid = {
      'url': 'http://school.example',
      'clientWidth': 1,
      'clientHeight': 4096,
    };
    expect(decodeProfiles(wire(valid)).single.announcement!.clientHeight, 4096);
    final source = wire(valid);
    for (final bad in [
      source.replaceFirst('"clientWidth":1', '"clientWidth":1,"clientWidth":2'),
      source.replaceFirst(
        '"url":"http://school.example"',
        '"url":"http://school.example","url":"http://other.example"',
      ),
    ]) {
      expect(() => decodeProfiles(bad), throwsA(isA<IpcProtocolException>()));
    }
  });

  test(
    'configuration availability keeps unknown Profile metadata truthful',
    () {
      const wire = _completeConfigurationWire;
      for (final status in [
        'available',
        'protocol_unavailable',
        'override_invalid',
        'profile_unavailable',
      ]) {
        var source = wire.replaceFirst(
          '"runtimeAvailability":"available"',
          '"runtimeAvailability":"$status"',
        );
        if (status == 'profile_unavailable') {
          source = source
              .replaceFirst(
                '"institutionDisplayName":"Institution"',
                '"institutionDisplayName":""',
              )
              .replaceFirst(
                '"authenticationProtocolId":"p"',
                '"authenticationProtocolId":""',
              );
        }
        final row = decodeConfiguration(source);
        expect(row.runtimeAvailability.wireValue, status);
        expect(
          decodeConfigurationPage(
            '{"storageProtection":"protected","configurations":[$source]}',
          ).configurations.single.runtimeAvailability,
          row.runtimeAvailability,
        );
        final bad = status == 'profile_unavailable'
            ? source.replaceFirst(
                '"institutionDisplayName":""',
                '"institutionDisplayName":"invented"',
              )
            : source.replaceFirst(
                '"institutionDisplayName":"Institution"',
                '"institutionDisplayName":""',
              );
        expect(
          () => decodeConfiguration(bad),
          throwsA(isA<IpcProtocolException>()),
        );
      }
      for (final source in [
        wire.replaceFirst('"runtimeAvailability":', '"unknown":'),
        wire.replaceFirst(
          '"runtimeAvailability":"available"',
          '"runtimeAvailability":null',
        ),
        wire.replaceFirst(
          '"runtimeAvailability":"available"',
          '"runtimeAvailability":"future"',
        ),
        wire.replaceFirst('"username":"u"', r'"username":"\ud800"'),
        wire.replaceFirst(
          '"username":"u"',
          r'"username":"u","\u0075sername":"u"',
        ),
        '$wire{}',
      ]) {
        expect(
          () => decodeConfiguration(source),
          throwsA(isA<IpcProtocolException>()),
        );
      }
    },
  );
}
