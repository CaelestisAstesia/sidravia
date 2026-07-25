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
// The protocol-text encoder covers the single-byte ASCII subset (0x01-0x7F),
// which is the range the tracked client-vector fixture exercises and the
// subset every converged source treats as plain bytes. Full GBK multibyte
// encoding is deliberately deferred until a real non-ASCII credential
// requires it; any rune outside the ASCII subset, and any embedded NUL, is
// rejected so the boundary stays observable rather than silently replaced or
// confused with NUL padding.
package d520

import (
	protocol "sidravia/internal/daemon/authentication/protocol"
)

// ProtocolID is the stable identifier under which the future Factory will
// register this protocol. It is the only public declaration in this package;
// every other declaration is package-private. This slice does not create a
// Factory or register the protocol.
const ProtocolID protocol.AuthenticationProtocolID = "drcom-5.2.0-d"
