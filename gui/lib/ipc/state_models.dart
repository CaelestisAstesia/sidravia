import 'dart:convert';

import 'package:sidravia_gui/ipc/ipc_models.dart';

const stateResourceCapacity = 256;
const stateFrameLimit = 64 * 1024;

class StateBootstrap {
  StateBootstrap({
    required List<SessionSummary> sessions,
    required this.network,
  }) : sessions = List.unmodifiable(sessions);
  final List<SessionSummary> sessions;
  final NetworkInterfacesSnapshot network;
}

sealed class StateEvent {
  const StateEvent();
}

final class SessionChanged extends StateEvent {
  const SessionChanged(this.session);
  final SessionSummary session;
  bool get cleanupRequired => session.cleanupRequired;
}

final class SessionRemoved extends StateEvent {
  const SessionRemoved({required this.sessionId, required this.revision});
  final String sessionId;
  final BigInt revision;
}

final class NetworkChanged extends StateEvent {
  const NetworkChanged(this.network);
  final NetworkInterfacesSnapshot network;
}

Object? _raw(String source) {
  if (utf8.encode(source).length > stateFrameLimit) {
    throw const IpcProtocolException();
  }
  return decodeBindingCheckedJson(
    source,
    preserveUnsignedRevision: true,
    preserveRunGeneration: true,
  );
}

Map<String, dynamic> _object(Object? value, Set<String> keys) {
  if (value is! Map<String, dynamic> ||
      value.length != keys.length ||
      !value.keys.toSet().containsAll(keys)) {
    throw const IpcProtocolException();
  }
  return value;
}

StateBootstrap decodeStateBootstrap(String source) =>
    decodeStateBootstrapValue(_raw(source));
StateBootstrap decodeStateBootstrapValue(Object? raw) {
  final root = _object(raw, const {'sessions', 'network'});
  final sessions = decodeSessionsValue(root['sessions']);
  if (sessions.length + 1 > stateResourceCapacity) {
    throw const IpcProtocolException();
  }
  return StateBootstrap(
    sessions: sessions,
    network: decodeNetworkInterfacesValue(root['network']),
  );
}

StateEvent decodeStateEvent(String source) =>
    decodeStateEventValue(_raw(source));
StateEvent decodeStateEventValue(Object? raw) {
  final root = _object(raw, const {'kind', 'method', 'payload'});
  if (root['kind'] != 'event') throw const IpcProtocolException();
  switch (root['method']) {
    case 'session.changed':
      final payload = _object(root['payload'], const {
        'session',
        'cleanupRequired',
      });
      if (payload['cleanupRequired'] is! bool) {
        throw const IpcProtocolException();
      }
      final session = payload['session'];
      if (session is! Map<String, dynamic>) throw const IpcProtocolException();
      // Reuse the one complete Session/list grammar, including cleanup membership.
      return SessionChanged(
        decodeSessionsValue({
          'sessions': [session],
          'cleanupRequiredSessionIds': payload['cleanupRequired'] == true
              ? [session['sessionId']]
              : <String>[],
        }).single,
      );
    case 'session.removed':
      final payload = _object(root['payload'], const {'sessionId', 'revision'});
      final id = payload['sessionId'];
      final revision = payload['revision'];
      if (id is! String ||
          id.isEmpty ||
          revision is! BigInt ||
          revision <= BigInt.zero ||
          revision > BigInt.parse('18446744073709551615')) {
        throw const IpcProtocolException();
      }
      return SessionRemoved(sessionId: id, revision: revision);
    case 'network.changed':
      final network = decodeNetworkInterfacesValue(root['payload']);
      if (!network.available) throw const IpcProtocolException();
      return NetworkChanged(network);
    default:
      throw const IpcProtocolException();
  }
}

void decodeStateUnsubscribe(String source) =>
    decodeStateUnsubscribeValue(_raw(source));
void decodeStateUnsubscribeValue(Object? raw) {
  if (_object(raw, const {'status'})['status'] != 'unsubscribed') {
    throw const IpcProtocolException();
  }
}
