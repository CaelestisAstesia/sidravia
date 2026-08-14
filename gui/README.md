# Sidravia GUI

Sidravia `0.2.0` test-line Flutter visual MVP.

Windows is the current delivery target. The MVP presents Home, Configuration
and Settings with a shared responsive navigation model. It does not yet
bootstrap or communicate with the daemon, retain credentials, control a
Session, integrate with the tray, or configure startup/PATH.

The application bundles and uses HarmonyOS Sans. Its license is included at
`assets/fonts/LICENSE.txt` and is available in Settings from Flutter's standard
license page.

The generated Android runner preserves a future shared-client path; it is not
an Android build or support claim.

```sh
flutter analyze
flutter test
```
