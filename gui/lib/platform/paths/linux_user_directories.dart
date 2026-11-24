import 'dart:io';

String? linuxUserConfigOverride(Map<String, String> environment) =>
    _absoluteDirectory(environment['XDG_CONFIG_HOME']);

String? linuxUserHome(Map<String, String> environment) =>
    _absoluteDirectory(environment['HOME']);

String? _absoluteDirectory(String? value) =>
    value != null && Directory(value).isAbsolute ? value : null;
