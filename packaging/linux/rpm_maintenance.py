"""Shared Linux package maintenance and RPM-specific transaction callbacks."""
import fcntl
import os
import pwd
import subprocess
from pathlib import Path
import select
import signal
import stat
import time

APP = Path('/usr/lib64/sidravia')
GATE = Path('/run/sidravia/rpm.lock')
NAMES = ('sidravia_gui', 'sidraviad', 'sidraviactl', 'sidraviaupdater')
MANAGER = 'rpm'


def marker_matches(suffix=''):
    marker = APP / f'sidravia.{MANAGER}{suffix}'
    return marker.is_file() and marker.read_text() == f'sidravia-{MANAGER}{suffix}-v1\n'


def identity(path):
    s = path.stat()
    return s.st_dev, s.st_ino


def alive(fd):
    p = select.poll()
    p.register(fd, select.POLLIN)
    return not p.poll(0)


def targets(executables=None):
    if not APP.exists():
        return []
    if not marker_matches():
        raise RuntimeError('installed package identity unavailable')
    directory = identity(APP)
    paths = executables
    if paths is None:
        paths = [APP / name for name in NAMES]
        if (APP / 'gui-runtime/sidravia_gui').is_file():
            paths.append(APP / 'gui-runtime/sidravia_gui')
    candidate_paths = {str(path) for path in paths}
    expected = {}
    for path in paths:
        try:
            expected[str(path)] = identity(path)
        except FileNotFoundError:
            if executables is not None:
                # Package-owned paths remain candidates after deletion. An
                # executable still running from an absent inode blocks removal.
                continue
            # Old RPMs carried the portable updater. New RPMs omit it, while
            # every present process still has to match its exact old inode.
            if path.name != 'sidraviaupdater' and not (path.name == 'sidravia_gui'
                    and marker_matches('-core')):
                raise
    found = []
    try:
        for proc in Path('/proc').iterdir():
            if not proc.name.isdigit():
                continue
            fd = None
            try:
                exe = os.readlink(proc / 'exe').removesuffix(' (deleted)')
                if exe not in candidate_paths:
                    continue
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
                if exe not in expected or identity(proc / 'exe') != expected[exe]:
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


def account_command(uid, command, timeout, stdout=subprocess.PIPE):
    account = pwd.getpwuid(uid)
    runtime = f'/run/user/{uid}'
    return subprocess.run(
        command, user=uid, group=account.pw_gid, extra_groups=[], cwd='/',
        env={'HOME': account.pw_dir, 'PATH': '/usr/bin', 'LC_ALL': 'C',
             'XDG_RUNTIME_DIR': runtime, 'DBUS_SESSION_BUS_ADDRESS': f'unix:path={runtime}/bus'},
        stdout=stdout, stderr=subprocess.DEVNULL,
        text=True, timeout=timeout, check=False)


def user_service(uid, *args, timeout=20):
    result = account_command(
        uid, ['/usr/bin/systemctl', '--user', '--no-ask-password',
              *args, 'sidravia.service'], timeout)
    if result.returncode != 0:
        raise RuntimeError('cannot coordinate the registered user service')
    return result.stdout.strip()


def wait_for_service_ipc(uid):
    def state():
        observed = user_service(uid, 'show', '-p', 'ActiveState,MainPID,InvocationID', timeout=2)
        return dict(line.split('=', 1) for line in observed.splitlines())

    deadline = time.monotonic() + 12
    while time.monotonic() < deadline:
        observed = state()
        pid = observed.get('MainPID', '')
        invocation = observed.get('InvocationID', '')
        if observed.get('ActiveState') in ('inactive', 'failed'):
            return False
        if observed.get('ActiveState') == 'active' and pid.isdigit() and int(pid) > 0 and invocation:
            probe = account_command(uid, [str(APP / 'sidraviactl'), f'{MANAGER}-user-service-ready', pid],
                                    2, stdout=subprocess.DEVNULL)
            if probe.returncode == 0 and state() == observed:
                return True
        time.sleep(.2)
    return False


def service_registration(uid):
    return Path(pwd.getpwuid(uid).pw_dir) / '.config/systemd/user/sidravia.service'


def service_uses_installation(values, expected):
    return (values.get('FragmentPath') == str(expected)
            and values.get('ExecStart', '').startswith(f'{{ path={APP / "sidraviad"} ;'))


def acquire_service_lease(uid, unit):
    lease = os.open(unit.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(lease)
        if info.st_uid != uid or info.st_mode & 0o022:
            raise RuntimeError('invalid user service directory ownership')
        fcntl.flock(lease, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BaseException:
        os.close(lease)
        raise
    return lease


def stop_managed_daemons(stopped, leases, before_stop=None, executables=None):
    entries = targets(executables)
    try:
        for name, fd in entries:
            if name != 'sidraviad' or not alive(fd):
                continue
            fields = dict(line.split(':', 1) for line in
                          Path(f'/proc/self/fdinfo/{fd}').read_text().splitlines())
            pid = int(fields['Pid'])
            proc = Path(f'/proc/{pid}')
            if not any(line.endswith('/sidravia.service')
                       for line in (proc / 'cgroup').read_text().splitlines()):
                continue
            uid = proc.stat().st_uid
            if uid == 0:
                raise RuntimeError('invalid Sidravia user service owner')
            expected = service_registration(uid)
            leases.append(acquire_service_lease(uid, expected))
            observed = user_service(uid, 'show', '-p', 'MainPID,FragmentPath,DropInPaths,ExecStart')
            values = dict(line.split('=', 1) for line in observed.splitlines())
            if (values.get('MainPID') != str(pid)
                    or not service_uses_installation(values, expected)
                    or any(Path(path).parent != Path('/usr/lib/systemd/user/service.d')
                           for path in values.get('DropInPaths', '').split())
                    or not alive(fd)):
                raise RuntimeError('could not establish registered service ownership')
            if before_stop is not None:
                before_stop(uid, expected)
            user_service(uid, 'stop')
            stopped.append(uid)
            if alive(fd):
                raise RuntimeError('registered user service did not stop')
            observed = user_service(uid, 'show', '-p', 'ActiveState,Result,ExecMainCode,ExecMainStatus')
            values = dict(line.split('=', 1) for line in observed.splitlines())
            if (values.get('ActiveState') != 'inactive'
                    or values.get('Result') != 'success'
                    or values.get('ExecMainCode') not in ('0', '1')
                    or values.get('ExecMainStatus') != '0'):
                raise RuntimeError('registered user service did not exit normally')
    finally:
        for _, fd in entries:
            os.close(fd)


def start_and_probe_user_service(uid):
    try:
        user_service(uid, 'start')
    except RuntimeError:
        if user_service(uid, 'show', '-p', 'Result', '--value') != 'start-limit-hit':
            raise
        user_service(uid, 'reset-failed')
        user_service(uid, 'start')
    return wait_for_service_ipc(uid)


def resume_verified_package(uids):
    if not uids:
        return
    for package in ('sidravia', 'sidravia-core'):
        verified = subprocess.run(
            ['/usr/bin/rpm', '-V', '--nosignature', package],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            timeout=30, check=False)
        if verified.returncode == 0:
            break
    else:
        for uid in uids:
            report_user_failure(uid, 'recovery skipped because the installed RPM failed verification')
        return
    for uid in uids:
        try:
            observed = user_service(uid, 'show', '-p', 'ExecStart,FragmentPath')
            values = dict(line.split('=', 1) for line in observed.splitlines())
            if not service_uses_installation(values, service_registration(uid)):
                report_user_failure(uid, 'recovery skipped because the registration changed')
                continue
            if not start_and_probe_user_service(uid):
                report_user_failure(uid, 'user service did not reach IPC readiness after the package transaction')
        except (RuntimeError, OSError, subprocess.SubprocessError):
            report_user_failure(uid, 'could not restore or verify the user service after the package transaction')


def cleanup_erased_registration(binary, retry_uids=None, *, descriptors=None):
    # Run the verified old CLI from an open descriptor under each owning UID.
    # The transaction root never reads or unlinks a user's home directory.
    accounts = [account for account in pwd.getpwall()
                if account.pw_uid != 0 and account.pw_dir.startswith('/')
                and (retry_uids is None or account.pw_uid in retry_uids)]
    pending = set()
    descriptors = descriptors or {}
    pass_fds = (binary, *descriptors.values())
    descriptor_env = {name: str(fd) for name, fd in descriptors.items()}
    deadline = time.monotonic() + 30
    for index, account in enumerate(accounts):
        runtime = f'/run/user/{account.pw_uid}'
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            report_erase_failure(account.pw_uid)
            pending.update(account.pw_uid for account in accounts[index:])
            break
        try:
            result = subprocess.run(
                [f'/proc/self/fd/{binary}', f'{MANAGER}-erase-user-registration'],
                user=account.pw_uid, group=account.pw_gid, extra_groups=[],
                cwd='/', pass_fds=pass_fds, timeout=remaining,
                env={'HOME': account.pw_dir, 'PATH': '/usr/bin', 'LC_ALL': 'C',
                     'XDG_RUNTIME_DIR': runtime, 'DBUS_SESSION_BUS_ADDRESS': f'unix:path={runtime}/bus',
                     **descriptor_env},
                check=False)
            if result.returncode:
                pending.add(account.pw_uid)
                report_erase_failure(account.pw_uid)
        except (OSError, subprocess.SubprocessError):
            pending.add(account.pw_uid)
            report_erase_failure(account.pw_uid)
    return pending


def report_erase_failure(uid):
    report_user_failure(uid, 'could not remove the user registration after erase')


def report_user_failure(uid, reason):
    try:
        subprocess.run(['/usr/bin/logger', '-t', 'sidravia-' + MANAGER,
                        f'Sidravia UID {uid}: {reason}'],
                       timeout=2, check=False)
    except (OSError, subprocess.SubprocessError):
        pass


def stop_group(gui, timeout, executables=None):
    entries = targets(executables)
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
    if marker_matches('-lifecycle'):
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


def wait_for_transaction_exit(parent, transaction, waiter):
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


def hold_until_transaction_exits(lock, parent, transaction, resume=(), leases=(), erase_cli=None):
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
            if fd > 2 and fd not in (lock, parent, transaction, ready_write, erase_cli, *leases):
                try:
                    os.close(fd)
                except OSError:
                    pass
        waiter = select.poll()
        waiter.register(parent, select.POLLIN)
        os.write(ready_write, b'R')
        os.close(ready_write)
        wait_for_transaction_exit(parent, transaction, waiter)
        if erase_cli is not None:
            held_gate = os.fstat(lock)
            held_gate_identity = (held_gate.st_dev, held_gate.st_ino)
            # Keep the startup gate through erase cleanup. A fresh Sidravia
            # pre-install already has a five-second bound on this lock and
            # fails before writing files if it races this cleanup.
            for lease in leases:
                os.close(lease)
            deadline = time.monotonic() + 30
            while True:
                try:
                    fcntl.lockf(transaction, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    if time.monotonic() >= deadline:
                        report_user_failure(0, 'erase cleanup could not obtain the RPM transaction lock')
                        return
                    time.sleep(.1)
            if not (APP / 'sidravia.rpm').exists():
                # Check and remove the gate only while the RPM database lock
                # prevents another transaction from needing or replacing it.
                try:
                    installed_gate = GATE.lstat()
                    if (installed_gate.st_dev, installed_gate.st_ino) == held_gate_identity:
                        GATE.unlink()
                except FileNotFoundError:
                    pass
                cleanup_erased_registration(erase_cli)
                return
            fcntl.lockf(transaction, fcntl.LOCK_UN)
        os.close(lock)
        if not transaction_busy(transaction):
            resume_verified_package(resume)
    except BaseException:
        os._exit(1)
    finally:
        os._exit(0)


def quiesce(remove=False):
    lock = parent = transaction = erase_cli = None
    stopped = []
    leases = []
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
        stop_managed_daemons(stopped, leases)
        stop_group(True, 15)
        stop_group(False, 10)
        remaining = targets()
        for _, fd in remaining:
            os.close(fd)
        if remaining:
            raise RuntimeError('application restarted during maintenance')
        if remove:
            erase_cli = os.open(APP / 'sidraviactl', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            info = os.fstat(erase_cli)
            if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
                raise RuntimeError('cannot establish ownership of the erase cleanup program')
        hold_until_transaction_exits(lock, parent, transaction, stopped, leases, erase_cli)
        return 0
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        if stopped and lock is not None and parent is not None and transaction is not None:
            try:
                hold_until_transaction_exits(lock, parent, transaction, stopped, leases)
            except (OSError, RuntimeError):
                pass
        # Static reasons only; never echo process command lines or user data.
        if isinstance(error, RuntimeError):
            print('Sidravia maintenance: ' + str(error), flush=True)
        else:
            print('Sidravia maintenance failed; package operation must stop.', flush=True)
        return 1
    finally:
        for lease in leases:
            os.close(lease)
        if erase_cli is not None:
            os.close(erase_cli)
        if lock is not None:
            os.close(lock)
        if parent is not None:
            os.close(parent)
        if transaction is not None:
            os.close(transaction)
