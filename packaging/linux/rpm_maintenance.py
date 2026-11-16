"""Embedded in RPM scriptlets; never imports product code or reads user data."""
import fcntl
import os
from pathlib import Path
import select
import signal
import stat
import time

APP = Path('/usr/lib64/sidravia')
GATE = Path('/run/sidravia/rpm.lock')
NAMES = ('sidravia_gui', 'sidraviad', 'sidraviactl', 'sidraviaupdater')


def identity(path):
    s = path.stat()
    return s.st_dev, s.st_ino


def alive(fd):
    p = select.poll()
    p.register(fd, select.POLLIN)
    return not p.poll(0)


def targets():
    if not APP.exists():
        return []
    if (APP / 'sidravia.rpm').read_text() != 'sidravia-rpm-v1\n':
        raise RuntimeError('installed package identity unavailable')
    directory = identity(APP)
    paths = [APP / name for name in NAMES]
    if (APP / 'gui-runtime/sidravia_gui').is_file():
        paths.append(APP / 'gui-runtime/sidravia_gui')
    expected = {str(path): identity(path) for path in paths}
    found = []
    try:
        for proc in Path('/proc').iterdir():
            if not proc.name.isdigit():
                continue
            fd = None
            candidate = False
            try:
                exe = os.readlink(proc / 'exe').removesuffix(' (deleted)')
                if exe not in expected:
                    continue
                candidate = True
                # Another mount/root may have the same pathname. Match the
                # installation directory and executable object, never the name.
                if identity(proc / 'root' / APP.relative_to('/')) != directory:
                    continue
                name = Path(exe).name
                fd = os.pidfd_open(int(proc.name))
                if not alive(fd):
                    os.close(fd)
                    fd = None
                    continue
                if identity(proc / 'exe') != expected[exe]:
                    os.close(fd)
                    fd = None
                    raise RuntimeError('an older executable generation is still active')
                found.append((name, fd))
                fd = None
            except PermissionError:
                if fd is not None:
                    os.close(fd)
                raise RuntimeError('process visibility is restricted; cannot prove installation is stopped')
            except (FileNotFoundError, ProcessLookupError):
                if fd is not None:
                    os.close(fd)
    except BaseException:
        for _, fd in found:
            os.close(fd)
        raise
    return found


def stop_group(gui, timeout):
    entries = targets()
    selected = [(name, fd) for name, fd in entries if (name == 'sidravia_gui') == gui]
    try:
        for _, fd in selected:
            try:
                signal.pidfd_send_signal(fd, signal.SIGTERM)
            except ProcessLookupError:
                pass
        deadline = time.monotonic() + timeout
        while any(alive(fd) for _, fd in selected):
            if time.monotonic() >= deadline:
                raise RuntimeError('application did not exit safely; package files were not replaced')
            time.sleep(.05)
    finally:
        for _, fd in entries:
            os.close(fd)


def require_cooperative_installation():
    capability = APP / 'sidravia.rpm-lifecycle'
    if capability.exists() and capability.read_text() == 'sidravia-rpm-lifecycle-v1\n':
        return
    entries = targets()
    for _, fd in entries:
        os.close(fd)
    if entries:
        raise RuntimeError('early prototype is running; exit that legacy installation before this one-time upgrade')


def transaction_busy(fd):
    try:
        fcntl.lockf(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        return True
    fcntl.lockf(fd, fcntl.LOCK_UN)
    return False


def hold_until_transaction_exits(lock, parent, transaction):
    """A tiny orphan owns only kernel descriptors, not a product lifecycle."""
    ready_read, ready_write = os.pipe2(os.O_CLOEXEC)
    child = os.fork()
    if child:
        os.close(ready_write)
        try:
            readable, _, _ = select.select([ready_read], [], [], 3)
            if not readable or os.read(ready_read, 1) != b'R':
                os.kill(child, signal.SIGTERM)
                os.waitpid(child, 0)
                raise RuntimeError('maintenance guard did not become ready')
        finally:
            os.close(ready_read)
        return
    os.close(ready_read)
    try:
        os.setsid()
        null = os.open('/dev/null', os.O_RDWR)
        for fd in (0, 1, 2):
            os.dup2(null, fd)
        for item in list(Path('/proc/self/fd').iterdir()):
            fd = int(item.name)
            if fd > 2 and fd not in (lock, parent, transaction, ready_write):
                try:
                    os.close(fd)
                except OSError:
                    pass
        waiter = select.poll()
        waiter.register(parent, select.POLLIN)
        os.write(ready_write, b'R')
        os.close(ready_write)
        while alive(parent):
            try:
                if not transaction_busy(transaction):
                    break
            except OSError:
                # Uncertain transaction state must fail closed until its host
                # exits, never free the startup gate while files may change.
                waiter.poll()
                break
            waiter.poll(100)
    except BaseException:
        os._exit(1)
    finally:
        os._exit(0)


def quiesce():
    lock = parent = transaction = None
    try:
        if os.geteuid() != 0:
            raise RuntimeError('RPM maintenance requires the transaction root')
        parent = os.pidfd_open(os.getppid())
        transaction = os.open('/usr/lib/sysimage/rpm/.rpm.lock', os.O_RDWR | os.O_CLOEXEC | os.O_NOFOLLOW)
        tx = os.fstat(transaction)
        if not stat.S_ISREG(tx.st_mode) or tx.st_uid != 0 or tx.st_mode & 0o022 or not transaction_busy(transaction):
            raise RuntimeError('cannot verify the active RPM transaction lock')
        GATE.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
        directory = GATE.parent.lstat()
        if not stat.S_ISDIR(directory.st_mode) or directory.st_uid != 0 or directory.st_mode & 0o022:
            raise RuntimeError('maintenance directory ownership conflict')
        lock = os.open(GATE, os.O_CREAT | os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, 0o644)
        info = os.fstat(lock)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_size or info.st_mode & 0o022:
            raise RuntimeError('maintenance lock ownership conflict')
        deadline = time.monotonic() + 5
        while True:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() >= deadline:
                    raise RuntimeError('another package operation or startup is active')
                time.sleep(.05)
        require_cooperative_installation()
        stop_group(True, 15)
        stop_group(False, 10)
        remaining = targets()
        for _, fd in remaining:
            os.close(fd)
        if remaining:
            raise RuntimeError('application restarted during maintenance')
        hold_until_transaction_exits(lock, parent, transaction)
        return 0
    except (OSError, RuntimeError) as error:
        # Static reasons only; never echo process command lines or user data.
        if isinstance(error, RuntimeError):
            print('Sidravia maintenance: ' + str(error), flush=True)
        else:
            print('Sidravia maintenance failed; package operation must stop.', flush=True)
        return 1
    finally:
        if lock is not None:
            os.close(lock)
        if parent is not None:
            os.close(parent)
        if transaction is not None:
            os.close(transaction)
