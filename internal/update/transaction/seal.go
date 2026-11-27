package transaction

// StartupSeal stays with a directory moved after a partial write. Execution
// records remain outside product files; this conservative presence-only seal
// prevents moving the target or clearing the cache from bypassing crash safety.
const StartupSeal = "sidravia.update"
