import 'dart:convert';

const _bootstrapSchemaVersion = 1;
const _bootstrapFailureCode = 'bootstrap_failed';
const _bootstrapFailureMessage = 'GUI bootstrap 失败';

class GuiBootstrap {
  const GuiBootstrap({
    required this.endpoint,
    required this.token,
    required this.productVersion,
    required this.buildId,
    required this.daemonPid,
    required this.mode,
  });

  final Uri endpoint;
  final String token;
  final String productVersion;
  final String buildId;
  final int daemonPid;
  final String mode;
}

class GuiBootstrapFailure {
  const GuiBootstrapFailure(this.code, this.message);

  final String code;
  final String message;

  static const unsupportedPlatform = GuiBootstrapFailure(
    'unsupported_platform',
    '当前平台不支持 GUI bootstrap',
  );
  static const failed = GuiBootstrapFailure(
    _bootstrapFailureCode,
    _bootstrapFailureMessage,
  );
}

const guiBootstrapFailures = <String, GuiBootstrapFailure>{
  'invalid_arguments': GuiBootstrapFailure(
    'invalid_arguments',
    'GUI bootstrap 参数无效',
  ),
  'invalid_client_identity': GuiBootstrapFailure(
    'invalid_client_identity',
    'GUI bootstrap 客户端身份无效',
  ),
  'unsupported_platform': GuiBootstrapFailure(
    'unsupported_platform',
    '当前平台不支持 GUI bootstrap',
  ),
  'mode_conflict': GuiBootstrapFailure('mode_conflict', '另一运行模式正在使用 daemon'),
  'incompatible_build': GuiBootstrapFailure(
    'incompatible_build',
    '客户端与 daemon 构建不兼容',
  ),
  'startup_unconfirmed': GuiBootstrapFailure(
    'startup_unconfirmed',
    'daemon 启动状态尚未确认',
  ),
  'bootstrap_failed': GuiBootstrapFailure(
    'bootstrap_failed',
    'GUI bootstrap 失败',
  ),
  'output_failed': GuiBootstrapFailure('output_failed', 'GUI bootstrap 结果输出失败'),
};

class GuiBootstrapResult {
  const GuiBootstrapResult.success(this.value) : failure = null;
  const GuiBootstrapResult.failure(this.failure) : value = null;

  final GuiBootstrap? value;
  final GuiBootstrapFailure? failure;

  bool get isSuccess => value != null;
}

abstract interface class GuiBootstrapper {
  Future<GuiBootstrapResult> bootstrap();
}

/// Explicit platform boundary for targets that do not yet have a local
/// daemon-launch adapter. Keeping this as a typed bootstrapper lets the
/// application and feature layers remain platform-neutral while a future
/// adapter is added.
class UnsupportedGuiBootstrapper implements GuiBootstrapper {
  const UnsupportedGuiBootstrapper();

  @override
  Future<GuiBootstrapResult> bootstrap() async =>
      const GuiBootstrapResult.failure(GuiBootstrapFailure.unsupportedPlatform);
}

GuiBootstrapResult decodeGuiBootstrap(String source) {
  try {
    final decoded = jsonDecode(source);
    if (decoded is! Map<String, dynamic> ||
        !_hasExactKeys(decoded, const {
          'schemaVersion',
          'endpoint',
          'token',
          'productVersion',
          'buildId',
          'daemonPid',
          'mode',
        })) {
      return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
    }
    final schemaVersion = decoded['schemaVersion'];
    final endpointValue = decoded['endpoint'];
    final token = decoded['token'];
    final productVersion = decoded['productVersion'];
    final buildId = decoded['buildId'];
    final daemonPid = decoded['daemonPid'];
    final mode = decoded['mode'];
    if (schemaVersion != _bootstrapSchemaVersion ||
        endpointValue is! String ||
        token is! String ||
        productVersion is! String ||
        buildId is! String ||
        daemonPid is! int ||
        mode is! String ||
        productVersion.isEmpty ||
        buildId.isEmpty ||
        daemonPid <= 0 ||
        mode != 'desktop' ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(token)) {
      return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
    }
    final endpoint = Uri.tryParse(endpointValue);
    if (endpoint == null ||
        endpoint.scheme != 'ws' ||
        endpoint.path != '/ipc' ||
        endpoint.hasQuery ||
        endpoint.hasFragment ||
        endpoint.userInfo.isNotEmpty ||
        endpoint.port <= 0 ||
        endpoint.port > 65535 ||
        !_isLoopback(endpoint.host)) {
      return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
    }
    return GuiBootstrapResult.success(
      GuiBootstrap(
        endpoint: endpoint,
        token: token,
        productVersion: productVersion,
        buildId: buildId,
        daemonPid: daemonPid,
        mode: mode,
      ),
    );
  } on FormatException {
    return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
  }
}

GuiBootstrapFailure decodeGuiBootstrapFailure(String source, int exitCode) {
  try {
    final value = jsonDecode(source);
    if (value is! Map<String, dynamic> ||
        !_hasExactKeys(value, const {'schemaVersion', 'error'}) ||
        value['schemaVersion'] != 1 ||
        value['error'] is! Map<String, dynamic>) {
      return GuiBootstrapFailure.failed;
    }
    final error = value['error'] as Map<String, dynamic>;
    if (!_hasExactKeys(error, const {'code', 'message'}) ||
        error['code'] is! String ||
        error['message'] is! String) {
      return GuiBootstrapFailure.failed;
    }
    final mapped = guiBootstrapFailures[error['code']];
    if (mapped == null ||
        mapped.message != error['message'] ||
        _exitFor(mapped.code) != exitCode) {
      return GuiBootstrapFailure.failed;
    }
    return mapped;
  } on FormatException {
    return GuiBootstrapFailure.failed;
  }
}

int _exitFor(String code) =>
    const {
      'invalid_arguments': 2,
      'invalid_client_identity': 10,
      'unsupported_platform': 11,
      'mode_conflict': 12,
      'incompatible_build': 13,
      'startup_unconfirmed': 14,
      'bootstrap_failed': 15,
      'output_failed': 16,
    }[code] ??
    -1;

bool _hasExactKeys(Map<String, dynamic> value, Set<String> expected) =>
    value.length == expected.length && value.keys.toSet().containsAll(expected);

bool _isLoopback(String host) => host == '127.0.0.1' || host == '::1';
