// Package d520 implements the Dr.COM 5.2.0(D) wire codec and cancellable Run.
//
// The codec owns no runtime state, time, randomness, retries, sockets,
// contexts, logging or goroutines. Every value that varies at runtime — the
// Challenge seed, the KA1 timestamp, the Login auth-extension tail, the
// Challenge salt, the Login Auth Info, and the KA2 serial and Tail — enters
// the appropriate helper explicitly. The protocol Run owns
// salt/auth_info/tail/serial and supplies them to these helpers; they are not
// IPC, Profile or context-override fields.
//
// The protocol-text encoder uses strict GBK. Invalid UTF-8, embedded NUL and
// unrepresentable runes are rejected so wire text cannot be silently replaced
// or confused with NUL padding.
package driver
