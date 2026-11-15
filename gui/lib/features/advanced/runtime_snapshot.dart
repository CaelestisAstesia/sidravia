import 'dart:convert';

import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

/// Explicit projection of public UI DTOs, not a dump of the client/bootstrap.
/// Never reads credentials, IPC authorization, protocol keys or raw packets.
/// Non-secret public values are preserved; uint64 revisions use decimal strings.
String captureRuntimeSnapshot(
  GuiController controller, {
  DateTime? capturedAt,
}) {
  final snapshot = controller.snapshot;
  final daemon = snapshot?.daemon;
  final network = snapshot?.network;
  final business = controller.business.snapshot;
  Object? endpoint(NetworkEndpoint? e) =>
      e == null ? null : {'address': e.address, 'port': e.port};
  return const JsonEncoder.withIndent('  ').convert({
    'capturedAt': (capturedAt ?? DateTime.now()).toUtc().toIso8601String(),
    'frontend': {
      'connectionState': controller.state.name,
      'busy': controller.busy,
      'notice': controller.notice,
      'failure': controller.failure == null
          ? null
          : {
              'code': controller.failure!.code,
              'message': controller.failure!.message,
            },
    },
    'backend': daemon == null
        ? null
        : {
            'productVersion': daemon.productVersion,
            'buildId': daemon.buildId,
            'pid': daemon.pid,
            'desktopOwnerPid': daemon.desktopOwnerPid,
            'status': daemon.status,
            'mode': daemon.mode,
            'logLevel': daemon.logLevel,
          },
    'configurations': snapshot?.configurations
        .map(
          (c) => {
            'id': c.id,
            'displayName': c.displayName,
            'institutionProfileId': c.institutionProfileId,
            'institutionDisplayName': c.institutionDisplayName,
            'authenticationProtocolId': c.authenticationProtocolId,
            'username': c.username,
            'credentialStored': c.credentialStored,
            'storageProtection': c.storageProtection,
            'networkBindingPolicy': c.networkBindingPolicy.toJson(),
            'autoLogin': c.autoLogin,
            'autoReconnect': c.autoReconnect,
            'runtimeAvailability': c.runtimeAvailability.wireValue,
          },
        )
        .toList(),
    'sessions': snapshot?.sessions
        .map(
          (s) => {
            'id': s.id,
            'accountName': s.accountName,
            'configurationId': s.configurationId,
            'institutionProfileId': s.institutionProfileId,
            'authenticationProtocolId': s.authenticationProtocolId,
            'state': s.state.wireValue,
            'intent': s.intent.wireValue,
            'revision': s.revision.toString(),
            'cleanupRequired': s.cleanupRequired,
            'updatedAt': s.updatedAt?.toIso8601String(),
            'authenticationEstablishedAt': s.authenticationEstablishedAt
                ?.toIso8601String(),
            'nextRetryAt': s.nextRetryAt?.toIso8601String(),
            'stateReason': s.stateReason == null
                ? null
                : {
                    'code': s.stateReason!.code,
                    'description': s.stateReason!.description,
                  },
            'selectedNetworkBinding': s.selectedNetworkBinding == null
                ? null
                : {
                    'interfaceId': s.selectedNetworkBinding!.interfaceId,
                    'displayName': s.selectedNetworkBinding!.displayName,
                    'localIpv4Address':
                        s.selectedNetworkBinding!.localIpv4Address,
                  },
            'lastAuthenticationFailure': s.lastAuthenticationFailure == null
                ? null
                : {
                    'code': s.lastAuthenticationFailure!.code,
                    'description': s.lastAuthenticationFailure!.description,
                    'handlingRecommendation':
                        s.lastAuthenticationFailure!.handlingRecommendation,
                  },
            'protocolSocket': {
              'state': s.protocolSocket.state,
              'runGeneration': s.protocolSocket.runGeneration.toString(),
              'updatedAt': s.protocolSocket.updatedAt,
              'localEndpoint': endpoint(s.protocolSocket.localEndpoint),
              'remoteEndpoint': endpoint(s.protocolSocket.remoteEndpoint),
            },
          },
        )
        .toList(),
    'network': network == null
        ? null
        : {
            'available': network.available,
            'revision': network.revision.toString(),
            'observedAt': network.observedAt,
            'interfaces': network.interfaces
                .map(
                  (i) => {
                    'interfaceId': i.interfaceId,
                    'displayName': i.displayName,
                    'description': i.description,
                    'operationalState': i.operationalState,
                    'ipv4Assignments': i.ipv4Assignments
                        .map(
                          (a) => {
                            'address': a.address,
                            'prefixLength': a.prefixLength,
                          },
                        )
                        .toList(),
                  },
                )
                .toList(),
          },
    'business': business == null
        ? null
        : {
            'sessionId': business.sessionId,
            'runGeneration': business.runGeneration.toString(),
            'revision': business.revision.toString(),
            'current': business.current,
            'statisticsSupported': business.statisticsSupported,
            'textSupported': business.textSupported,
            'updatedAt': business.updatedAt.toIso8601String(),
            'statistics': business.statistics == null
                ? null
                : {
                    'receivedAt': business.statistics!.receivedAt
                        .toIso8601String(),
                    'trafficBytes': business.statistics!.trafficBytes,
                    'balanceCents': business.statistics!.balanceCents,
                  },
            'messageCount': business.messages.length,
            'latestMessage': business.messages.isEmpty
                ? null
                : {
                    'id': business.messages.last.id,
                    'receivedAt': business.messages.last.receivedAt
                        .toIso8601String(),
                    'text': business.messages.last.text,
                  },
          },
  });
}
