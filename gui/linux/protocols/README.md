# Wayland protocol inputs

These unmodified MIT-licensed protocol definitions come from wayland-protocols,
commit `b9edf66597586c914b5b1ff3d964d7ed201adab9` (upstream mirror: https://github.com/wayland-mirror/wayland-protocols).
Original paths: `stable/xdg-shell/xdg-shell.xml` and
`staging/xdg-toplevel-icon/xdg-toplevel-icon-v1.xml`. Copyright and licenses stay
in each XML file. CMake generates private client bindings using wayland-scanner;
only the icon extension creates application-owned protocol objects. GDK continues
to own the display event dispatch, surface and xdg_toplevel. The optional icon
extension supplies immutable ARGB pixel buffers without installing a desktop file.
No synchronous Wayland roundtrip or competing display dispatcher is introduced.
