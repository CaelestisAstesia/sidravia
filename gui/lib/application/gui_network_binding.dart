import 'package:sidravia_gui/ipc/ipc_models.dart';

/// Filters the daemon's observations; does not choose an OS interface or route.
List<({NetworkBindingPolicy policy, String label})> networkBindingChoices(
  NetworkInterfacesSnapshot? network,
) => [
  if (network?.available == true)
    for (final interface in network!.interfaces)
      if (interface.operationalState == 'up')
        for (final address in interface.ipv4Assignments)
          if (address.explicitBindable)
            (
              policy: NetworkBindingPolicy.explicit(
                interface.interfaceId,
                address.address,
              ),
              label:
                  '${interface.displayName.isEmpty ? interface.interfaceId : interface.displayName} · ${interface.interfaceId} · ${address.address}/${address.prefixLength}',
            ),
];

bool networkBindingEligible(
  NetworkInterfacesSnapshot? network,
  NetworkBindingPolicy policy,
) =>
    network?.available == true &&
    (policy.interfaceId == null ||
        networkBindingChoices(network)
            .any((choice) => choice.policy == policy));
