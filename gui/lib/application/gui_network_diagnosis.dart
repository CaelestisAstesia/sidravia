import 'package:sidravia_gui/ipc/ipc_models.dart';

String diagnosisErrorMessage(String code) => switch (code) {
  'configuration_not_found' => '连接配置已不存在，请刷新配置',
  'session_not_found' => '会话已不存在，请刷新',
  'profile_not_found' => '学校配置不可用，请检查连接配置',
  'unsupported_platform' => '当前平台不支持网络诊断',
  _ => '请检查配置或稍后刷新',
};

String _code(String code) => switch (code) {
  'available' => '已取得路由观察',
  'unsupported' => '暂不支持',
  'platform' => '当前平台不支持路由诊断',
  'protocol' => '当前认证协议不支持此诊断',
  'destination' => '当前目标地址不支持此诊断',
  'binding_unavailable' => '指定网络绑定当前不可用',
  'route_unavailable' => '未取得可用路由',
  'session_binding' => '会话当前网络绑定',
  'configuration_explicit' => '配置中指定的网络绑定',
  'os_route_proposal' => '操作系统建议的路由（尚非实际协议端点）',
  'not_requested' => '未请求探测',
  'reachable' => '收到 IP 回显',
  'no_reply' => '未收到回显，结果不确定',
  'unreachable' => '报告不可达',
  'failed' => '探测失败',
  'not_observed' => '尚无协议 socket 观察',
  'open' => '协议 socket 已打开',
  'closed' => '协议 socket 已关闭',
  'close_failed' => '协议 socket 关闭失败',
  'close_unconfirmed' => '协议 socket 关闭尚未确认',
  _ => code,
};
String _endpoint(NetworkEndpoint endpoint) =>
    '${endpoint.address}:${endpoint.port}';

List<MapEntry<String, String>> diagnosisRows(NetworkDiagnosis d) => [
  MapEntry('观察时间', d.observedAt),
  MapEntry('诊断状态', '${_code(d.status)} (${d.status})'),
  if (d.unsupportedReason != null)
    MapEntry('不可用原因', _code(d.unsupportedReason!)),
  MapEntry('选择依据', _code(d.selectionBasis)),
  if (d.target != null) MapEntry('诊断目的端点', _endpoint(d.target!)),
  if (d.route case final r?) ...[
    MapEntry('路由源 IPv4', r.sourceIPv4),
    MapEntry('路由接口', r.interfaceId),
    MapEntry('接口索引', '${r.interfaceIndex}'),
    MapEntry('目的网段', r.destinationPrefix),
    MapEntry('下一跳', r.nextHopIPv4),
    MapEntry(
      '路由 / 接口 / 总度量',
      '${r.routeMetric} / ${r.interfaceMetric} / ${r.effectiveMetric}',
    ),
  ],
  if (d.probe case final p?)
    MapEntry(
      '有限 IP 探测',
      '${_code(p.status)}${p.roundTripTimeMs == null ? '' : ' · ${p.roundTripTimeMs} ms'}',
    ),
  MapEntry('协议 socket', _code(d.protocolSocket?.state ?? 'not_observed')),
  if (d.protocolSocket case final socket?) ...[
    MapEntry('协议运行代次', '${socket.runGeneration}'),
    if (socket.updatedAt != null) MapEntry('socket 观察时间', socket.updatedAt!),
    if (socket.localEndpoint != null)
      MapEntry('实际本地端点', _endpoint(socket.localEndpoint!)),
    if (socket.remoteEndpoint != null)
      MapEntry('实际远端端点', _endpoint(socket.remoteEndpoint!)),
  ],
];
