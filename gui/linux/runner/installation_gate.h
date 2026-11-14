#ifndef SIDRAVIA_INSTALLATION_GATE_H_
#define SIDRAVIA_INSTALLATION_GATE_H_

// Returns false only for an RPM installation with an active/untrusted gate.
bool sidravia_can_start();

#endif
