// Package d520 implements the deterministic, side-effect-free wire codec for
// the Dr.COM 5.2.0(D) authentication protocol.
//
// The codec owns no runtime state, time, randomness, retries, sockets,
// contexts, logging or goroutines. Every value that varies at runtime — the
// Challenge seed, the KA1 timestamp, the Login auth-extension tail, the
// Challenge salt, the Login Auth Info, and the KA2 serial and Tail — enters
// the appropriate helper explicitly. The future protocol Run owns
// salt/auth_info/tail/serial and supplies them to these helpers; they are not
// IPC, Profile or context-override fields.
//
// The protocol-text encoder uses strict GBK. Invalid UTF-8, embedded NUL and
// unrepresentable runes are rejected so wire text cannot be silently replaced
// or confused with NUL padding.
package d520

import (
	protocol "sidravia/internal/daemon/authentication/protocol"
)

// ProtocolID is the stable identifier under which the future Factory will
// register this protocol. It is the only public declaration in this package;
// every other declaration is package-private. This slice does not create a
// Factory or register the protocol.
const ProtocolID protocol.AuthenticationProtocolID = "drcom-5.2.0-d"
