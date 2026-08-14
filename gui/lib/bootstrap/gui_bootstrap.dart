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

bool _hasExactKeys(Map<String, dynamic> value, Set<String> expected) =>
    value.length == expected.length && value.keys.toSet().containsAll(expected);

bool _isLoopback(String host) => host == '127.0.0.1' || host == '::1';
