import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/integration_failure.dart';
import 'package:sidravia_gui/platform/update/package_update_checker.dart';
import 'package:sidravia_gui/platform/update/update_check_response.dart';

void main() {
  const success =
      '{"schemaVersion":1,"ok":true,"code":"ok",'
      '"result":{"check":{"state":"available","candidate":'
      '{"version":"1.2.3","id":"2:22","notes":"Fix things","canApply":true}}}}';

  test('only a successful process can return a successful check', () {
    final result = decodeUpdateCheckResponse(0, success);
    expect(result.state, UpdateCheckState.available);
    expect(result.version, '1.2.3');
    expect(result.notes, 'Fix things');
    expect(result.installCandidateId, '2:22');
    expect(
      decodeUpdateCheckResponse(
        0,
        success.replaceFirst('"canApply":true', '"canApply":false'),
      ).installCandidateId,
      isNull,
    );
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

  test('a maintenance engine change directs users to an offline installer', () {
    const output =
        '{"schemaVersion":1,"ok":false,'
        '"code":"maintenance_image_requires_reinstall"}';
    expect(
      () => decodeUpdateOperationResponse(1, output, installing: true),
      throwsA(
        isA<IntegrationFailure>().having(
          (failure) => failure.message,
          'message',
          '此版本需要更换维护工具，请从发行页下载完整安装包',
        ),
      ),
    );
  });

  test('malformed responses and invalid release versions are rejected', () {
    for (final output in [
      'not json',
      '{"schemaVersion":2,"ok":false,"code":"rate_limited"}',
      '{"schemaVersion":1,"ok":false,"code":null}',
      success.replaceFirst('1.2.3', '../other'),
      success.replaceFirst('2:22', '--confirm'),
    ]) {
      expect(() => decodeUpdateCheckResponse(0, output), throwsFormatException);
    }
  });
}
