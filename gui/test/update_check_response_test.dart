import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/installation_manager.dart';
import 'package:sidravia_gui/platform/update_check_response.dart';

void main() {
  const success =
      '{"schemaVersion":1,"ok":true,"code":"ok",'
      '"result":{"check":{"state":"available","candidate":{"version":"1.2.3"}}}}';

  test('only a successful process can return a successful check', () {
    expect(decodeUpdateCheckResponse(0, success), (
      state: UpdateCheckState.available,
      version: '1.2.3',
    ));
    expect(() => decodeUpdateCheckResponse(1, success), throwsFormatException);
  });

  test('a structured failure is reported safely with either exit code', () {
    const output =
        '{"schemaVersion":1,"ok":false,"code":"rate_limited",'
        '"message":"untrusted child output"}';
    for (final exitCode in [0, 1]) {
      expect(
        () => decodeUpdateCheckResponse(exitCode, output),
        throwsA(
          isA<IntegrationFailure>().having(
            (failure) => failure.message,
            'message',
            'GitHub 请求受限，请稍后重试。',
          ),
        ),
      );
    }
  });

  test('malformed responses and invalid release versions are rejected', () {
    for (final output in [
      'not json',
      '{"schemaVersion":2,"ok":false,"code":"rate_limited"}',
      '{"schemaVersion":1,"ok":false,"code":null}',
      success.replaceFirst('1.2.3', '../other'),
    ]) {
      expect(() => decodeUpdateCheckResponse(0, output), throwsFormatException);
    }
  });
}
