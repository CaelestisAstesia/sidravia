part of 'ipc_models.dart';

class NetworkBindingPolicy {
  const NetworkBindingPolicy.automatic()
    : mode = 'automatically_select_latest_available',
      interfaceId = null,
      localIpv4Address = null;
  const NetworkBindingPolicy._(
    this.mode,
    this.interfaceId,
    this.localIpv4Address,
  );
  factory NetworkBindingPolicy.explicit(String id, String address) {
    final runes = id.runes.toList();
    bool space(int r) =>
        const {
          0x9,
          0xa,
          0xb,
          0xc,
          0xd,
          0x20,
          0x85,
          0xa0,
          0x1680,
          0x2028,
          0x2029,
          0x202f,
          0x205f,
          0x3000,
        }.contains(r) ||
        (r >= 0x2000 && r <= 0x200a);
    if (runes.isEmpty ||
        utf8.encode(id).length > 256 ||
        space(runes.first) ||
        space(runes.last) ||
        runes.any(
          (r) =>
              r <= 0x1f ||
              (r >= 0x7f && r <= 0x9f) ||
              (r >= 0xd800 && r <= 0xdfff),
        )) {
      throw const IpcProtocolException();
    }
    _networkIPv4(address, broadcast: true);
    return NetworkBindingPolicy._(
      'explicit_interface_and_local_ipv4',
      id,
      address,
    );
  }
  final String mode;
  final String? interfaceId, localIpv4Address;
  Map<String, Object> toJson() => {
    'mode': mode,
    'interfaceId': ?interfaceId,
    'localIpv4Address': ?localIpv4Address,
  };
  String get summary => interfaceId == null
      ? '自动选择可用网卡'
      : '指定网卡：$interfaceId\n本地 IPv4：$localIpv4Address';
  static NetworkBindingPolicy decode(Object? raw) {
    if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
    if (raw['mode'] == 'automatically_select_latest_available') {
      decodeObjectValue(raw, const {'mode'});
      return const NetworkBindingPolicy.automatic();
    }
    final v = decodeObjectValue(raw, const {
      'mode',
      'interfaceId',
      'localIpv4Address',
    });
    if (v['mode'] != 'explicit_interface_and_local_ipv4') {
      throw const IpcProtocolException();
    }
    return NetworkBindingPolicy.explicit(
      _text(v['interfaceId']),
      _text(v['localIpv4Address']),
    );
  }

  @override
  bool operator ==(Object other) =>
      other is NetworkBindingPolicy &&
      mode == other.mode &&
      interfaceId == other.interfaceId &&
      localIpv4Address == other.localIpv4Address;
  @override
  int get hashCode => Object.hash(mode, interfaceId, localIpv4Address);
}

// These immutable wire models are an optional query capability; the GUI does
// not fetch them or own another network cache.
class NetworkInterfacesSnapshot {
  NetworkInterfacesSnapshot({
    required this.available,
    required this.revision,
    required List<NetworkInterfaceRow> interfaces,
    this.observedAt,
  }) : interfaces = List.unmodifiable(interfaces);
  final bool available;
  final BigInt revision;
  final String? observedAt;
  final List<NetworkInterfaceRow> interfaces;
}

class NetworkInterfaceRow {
  NetworkInterfaceRow({
    required this.interfaceId,
    required this.displayName,
    this.description = '',
    required this.operationalState,
    required this.physicalMedium,
    required this.hardwareBacked,
    required this.physicalConnectorPresent,
    required this.filterInterface,
    required this.endpointInterface,
    required this.addressAssignmentMethod,
    required List<NetworkIPv4Assignment> ipv4Assignments,
  }) : ipv4Assignments = List.unmodifiable(ipv4Assignments);
  final String interfaceId,
      displayName,
      description,
      operationalState,
      physicalMedium,
      addressAssignmentMethod;
  final bool hardwareBacked,
      physicalConnectorPresent,
      filterInterface,
      endpointInterface;
  final List<NetworkIPv4Assignment> ipv4Assignments;
}

class NetworkIPv4Assignment {
  const NetworkIPv4Assignment({
    required this.address,
    required this.prefixLength,
    required this.automaticCandidate,
    required this.explicitBindable,
  });
  final String address;
  final int prefixLength;
  final bool automaticCandidate, explicitBindable;
}

NetworkInterfacesSnapshot decodeNetworkInterfaces(String source) =>
    decodeNetworkInterfacesValue(
      decodeBindingCheckedJson(source, preserveUnsignedRevision: true),
    );

NetworkInterfacesSnapshot decodeNetworkInterfacesValue(Object? raw) {
  if (raw is! Map<String, dynamic> || raw['available'] is! bool) {
    throw const IpcProtocolException();
  }
  final available = raw['available'] as bool;
  final root = decodeObjectValue(raw, {
    'available',
    'revision',
    'interfaces',
    if (available) 'observedAt',
  });
  final revision = root['revision'];
  if (revision is! BigInt ||
      (available ? revision <= BigInt.zero : revision != BigInt.zero)) {
    throw const IpcProtocolException();
  }
  final rows = _list(root['interfaces']).map((raw) {
    final row = decodeObjectValue(raw, {
      'interfaceId',
      'displayName',
      if (raw is Map<String, dynamic> && raw.containsKey('description'))
        'description',
      'operationalState',
      'physicalMedium',
      'hardwareBacked',
      'physicalConnectorPresent',
      'filterInterface',
      'endpointInterface',
      'addressAssignmentMethod',
      'ipv4Assignments',
    });
    String enumValue(String key, Set<String> allowed) {
      final value = _text(row[key]);
      if (!allowed.contains(value)) throw const IpcProtocolException();
      return value;
    }

    final id = _text(row['interfaceId']);
    final name = _optionalText(row['displayName']);
    final description = row.containsKey('description')
        ? _optionalText(row['description'])
        : '';
    if ([
      id,
      name,
      description,
    ].any((text) => text.runes.any((r) => r >= 0xd800 && r <= 0xdfff))) {
      throw const IpcProtocolException();
    }
    return NetworkInterfaceRow(
      interfaceId: id,
      displayName: name,
      description: description,
      operationalState: enumValue('operationalState', const {'up', 'down'}),
      physicalMedium: enumValue('physicalMedium', const {
        'wired',
        'wireless',
        'unknown',
      }),
      addressAssignmentMethod: enumValue('addressAssignmentMethod', const {
        'unknown',
        'static',
        'dhcp',
      }),
      hardwareBacked: _bool(row['hardwareBacked']),
      physicalConnectorPresent: _bool(row['physicalConnectorPresent']),
      filterInterface: _bool(row['filterInterface']),
      endpointInterface: _bool(row['endpointInterface']),
      ipv4Assignments: _list(row['ipv4Assignments']).map((raw) {
        final address = decodeObjectValue(raw, const {
          'address',
          'prefixLength',
          'automaticCandidate',
          'explicitBindable',
        });
        final text = _text(address['address']);
        final parts = text.split('.');
        if (parts.length != 4 ||
            parts.any((p) {
              final n = int.tryParse(p);
              return n == null || n < 0 || n > 255 || n.toString() != p;
            })) {
          throw const IpcProtocolException();
        }
        final prefix = address['prefixLength'];
        if (prefix is! int || prefix < 0 || prefix > 32) {
          throw const IpcProtocolException();
        }
        return NetworkIPv4Assignment(
          address: text,
          prefixLength: prefix,
          automaticCandidate: _bool(address['automaticCandidate']),
          explicitBindable: _bool(address['explicitBindable']),
        );
      }).toList(),
    );
  }).toList();
  String? observedAt;
  if (available) {
    observedAt = _nonzeroWireTime(root['observedAt']);
  } else if (rows.isNotEmpty) {
    throw const IpcProtocolException();
  }
  return NetworkInterfacesSnapshot(
    available: available,
    revision: revision,
    interfaces: rows,
    observedAt: observedAt,
  );
}

class NetworkEndpoint {
  const NetworkEndpoint(this.address, this.port);
  final String address;
  final int port;
}

class NetworkProtocolSocket {
  const NetworkProtocolSocket({
    required this.state,
    required this.runGeneration,
    this.updatedAt,
    this.localEndpoint,
    this.remoteEndpoint,
  });
  final String state;
  final BigInt runGeneration;
  final String? updatedAt;
  final NetworkEndpoint? localEndpoint, remoteEndpoint;
}

const networkSocketStates = {
  'not_observed',
  'open',
  'closed',
  'close_failed',
  'close_unconfirmed',
};
int _networkUint(Object? value, int max, {bool positive = false}) {
  if (value is! int || value < (positive ? 1 : 0) || value > max) {
    throw const IpcProtocolException();
  }
  return value;
}

String _networkEnum(Object? value, Set<String> allowed) {
  final text = _text(value);
  if (!allowed.contains(text)) {
    throw const IpcProtocolException();
  }
  return text;
}

int _networkIPv4(
  String text, {
  bool zero = false,
  bool broadcast = false,
  bool multicast = false,
}) {
  final parts = text.split('.');
  if (parts.length != 4) {
    throw const IpcProtocolException();
  }
  var n = 0;
  for (final part in parts) {
    final v = int.tryParse(part);
    if (v == null || v < 0 || v > 255 || v.toString() != part) {
      throw const IpcProtocolException();
    }
    n = n * 256 + v;
  }
  if ((!zero && n == 0) ||
      (!broadcast && n == 4294967295) ||
      (!multicast && n >= 3758096384 && n <= 4026531839)) {
    throw const IpcProtocolException();
  }
  return n;
}

NetworkEndpoint _networkEndpoint(Object? raw, {bool broadcast = false}) {
  final v = decodeObjectValue(raw, const {'address', 'port'});
  final address = _text(v['address']);
  _networkIPv4(address, broadcast: broadcast);
  return NetworkEndpoint(
    address,
    _networkUint(v['port'], 65535, positive: true),
  );
}

Map<String, dynamic> _networkObject(
  Object? raw,
  Set<String> required,
  Set<String> optional,
) {
  if (raw is! Map<String, dynamic> ||
      !raw.keys.toSet().containsAll(required) ||
      raw.keys.any((k) => !required.contains(k) && !optional.contains(k)) ||
      raw.values.any((v) => v == null)) {
    throw const IpcProtocolException();
  }
  return raw;
}

NetworkProtocolSocket _decodeProtocolSocket(Object? raw) {
  final s = _networkObject(
    raw,
    const {'state', 'runGeneration'},
    const {'updatedAt', 'localEndpoint', 'remoteEndpoint'},
  );
  final state = _networkEnum(s['state'], networkSocketStates);
  final generation = s['runGeneration'];
  if (generation is! BigInt ||
      generation < BigInt.zero ||
      generation > BigInt.parse('18446744073709551615')) {
    throw const IpcProtocolException();
  }
  final updated = s.containsKey('updatedAt')
      ? _nonzeroWireTime(s['updatedAt'])
      : null;
  NetworkEndpoint? local, remote;
  if (state == 'not_observed') {
    if (s.containsKey('localEndpoint') ||
        s.containsKey('remoteEndpoint') ||
        (generation == BigInt.zero) != (updated == null)) {
      throw const IpcProtocolException();
    }
  } else {
    if (generation == BigInt.zero || updated == null) {
      throw const IpcProtocolException();
    }
    local = _networkEndpoint(s['localEndpoint']);
    remote = _networkEndpoint(s['remoteEndpoint'], broadcast: true);
  }
  return NetworkProtocolSocket(
    state: state,
    runGeneration: generation,
    updatedAt: updated,
    localEndpoint: local,
    remoteEndpoint: remote,
  );
}
