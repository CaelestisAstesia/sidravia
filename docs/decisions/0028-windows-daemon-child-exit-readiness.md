# ADR 0028: Windows daemon child-exit readiness

Process creation and typed readiness are distinct. Windows observes the exact launched child and makes one final readiness probe after child exit. The CLI output stays fixed and safe; daemon logs own private diagnostics. Linux keeps setsid plus Process.Release unchanged.
