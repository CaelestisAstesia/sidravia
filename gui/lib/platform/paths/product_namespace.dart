/// Validates the runtime namespace used by product paths and desktop channels.
/// Null and empty input select the production namespace.
String? validateProductNamespace(String? value) {
  if (value == null || value.isEmpty) return '';
  if (value.length > 64 ||
      !RegExp(r'^[A-Za-z0-9._-]+$').hasMatch(value) ||
      value.startsWith('.') ||
      value.endsWith('.') ||
      value.contains('..') ||
      const {'production', 'default', 'prod'}.contains(value.toLowerCase())) {
    return null;
  }
  return value;
}
