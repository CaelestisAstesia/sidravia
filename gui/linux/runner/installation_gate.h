#ifndef SIDRAVIA_INSTALLATION_GATE_H_
#define SIDRAVIA_INSTALLATION_GATE_H_

// Packaged portable runtime must inherit its pre-loader lease from the static
// public launcher. RPM retains its transaction gate; developer bundles are local.
bool sidravia_can_start();

#endif
