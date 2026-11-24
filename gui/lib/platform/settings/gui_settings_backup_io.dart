import 'dart:ffi';
import 'dart:io';

// Only GUI settings migration uses this platform boundary. No process, installer
// or general file-maintenance command is exposed.
final class _SecurityAttributes extends Struct {
  @Uint32()
  external int length;
  external Pointer<Void> descriptor;
  @Int32()
  external int inherit;
}

Future<void> preserveGuiSettingsFile(File source, File destination) async {
  if (!Platform.isWindows) {
    // File.copy preserves the source mode on Unix. Reject a widening rather
    // than treating inherited parent permissions as proof of equal protection.
    final originalMode = (await source.stat()).mode & 0x1ff;
    await source.copy(destination.path);
    final copiedMode = (await destination.stat()).mode & 0x1ff;
    if ((copiedMode & ~originalMode) != 0) {
      await destination.delete();
      throw const FileSystemException('GUI backup permissions differ');
    }
    return;
  }
  _preserveWindowsSettings(source.path, destination.path);
  final copied = await destination.open(mode: FileMode.append);
  try {
    await copied.flush();
  } finally {
    await copied.close();
  }
}

void _preserveWindowsSettings(String source, String destination) {
  final kernel = DynamicLibrary.open('kernel32.dll');
  final security = DynamicLibrary.open('advapi32.dll');
  final heap = kernel
      .lookupFunction<Pointer<Void> Function(), Pointer<Void> Function()>(
        'GetProcessHeap',
      )();
  final allocate = kernel
      .lookupFunction<
        Pointer<Void> Function(Pointer<Void>, Uint32, IntPtr),
        Pointer<Void> Function(Pointer<Void>, int, int)
      >('HeapAlloc');
  final free = kernel
      .lookupFunction<
        Int32 Function(Pointer<Void>, Uint32, Pointer<Void>),
        int Function(Pointer<Void>, int, Pointer<Void>)
      >('HeapFree');
  final localFree = kernel
      .lookupFunction<
        Pointer<Void> Function(Pointer<Void>),
        Pointer<Void> Function(Pointer<Void>)
      >('LocalFree');
  final memory = <Pointer<Void>>[];
  Pointer<Void> reserve(int count) {
    final pointer = allocate(heap, 8, count);
    if (pointer == nullptr) {
      throw const FileSystemException('GUI backup memory');
    }
    memory.add(pointer);
    return pointer;
  }

  Pointer<Uint16> wide(String value) {
    final pointer = reserve((value.length + 1) * 2).cast<Uint16>();
    pointer.asTypedList(value.length + 1).setAll(0, value.codeUnits);
    return pointer;
  }

  final readSecurity = security
      .lookupFunction<
        Uint32 Function(
          Pointer<Uint16>,
          Uint32,
          Uint32,
          Pointer<Void>,
          Pointer<Void>,
          Pointer<Void>,
          Pointer<Void>,
          Pointer<Pointer<Void>>,
        ),
        int Function(
          Pointer<Uint16>,
          int,
          int,
          Pointer<Void>,
          Pointer<Void>,
          Pointer<Void>,
          Pointer<Void>,
          Pointer<Pointer<Void>>,
        )
      >('GetNamedSecurityInfoW');
  final protect = security
      .lookupFunction<
        Int32 Function(Pointer<Void>, Uint16, Uint16),
        int Function(Pointer<Void>, int, int)
      >('SetSecurityDescriptorControl');
  final create = kernel
      .lookupFunction<
        IntPtr Function(
          Pointer<Uint16>,
          Uint32,
          Uint32,
          Pointer<_SecurityAttributes>,
          Uint32,
          Uint32,
          IntPtr,
        ),
        int Function(
          Pointer<Uint16>,
          int,
          int,
          Pointer<_SecurityAttributes>,
          int,
          int,
          int,
        )
      >('CreateFileW');
  final close = kernel
      .lookupFunction<Int32 Function(IntPtr), int Function(int)>('CloseHandle');
  final copy = kernel
      .lookupFunction<
        Int32 Function(Pointer<Uint16>, Pointer<Uint16>, Int32),
        int Function(Pointer<Uint16>, Pointer<Uint16>, int)
      >('CopyFileW');
  Pointer<Void> descriptor = nullptr;
  var handle = -1;
  try {
    final original = wide(source);
    final backup = wide(destination);
    final result = reserve(sizeOf<Pointer<Void>>()).cast<Pointer<Void>>();
    if (readSecurity(
          original,
          1,
          4,
          nullptr,
          nullptr,
          nullptr,
          nullptr,
          result,
        ) !=
        0) {
      throw const FileSystemException('Cannot read GUI settings protection');
    }
    descriptor = result.value;
    // Prevent inheritance from adding readers while keeping the source DACL.
    if (protect(descriptor, 0x1000, 0x1000) == 0) {
      throw const FileSystemException('Cannot protect GUI settings backup');
    }
    final attributes = reserve(sizeOf<_SecurityAttributes>())
        .cast<_SecurityAttributes>();
    attributes.ref
      ..length = sizeOf<_SecurityAttributes>()
      ..descriptor = descriptor
      ..inherit = 0;
    handle = create(backup, 0x40000000, 0, attributes, 1, 0x80, 0);
    if (handle == -1) {
      throw const FileSystemException('Cannot create private GUI backup');
    }
    if (close(handle) == 0) {
      throw const FileSystemException('Cannot close GUI backup');
    }
    handle = -1;
    if (copy(original, backup, 0) == 0) {
      throw const FileSystemException('Cannot copy GUI settings backup');
    }
  } finally {
    if (handle != -1) close(handle);
    if (descriptor != nullptr) localFree(descriptor);
    for (final pointer in memory.reversed) {
      free(heap, 0, pointer);
    }
  }
}
