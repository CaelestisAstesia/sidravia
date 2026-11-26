"""dpkg callbacks, embedded together with the shared Linux maintenance actions."""
import contextlib
import hashlib
import json
import shutil
import sys
import tempfile

APP = Path('/usr/lib/sidravia')
MANAGER = 'deb'
NAMES = ('sidravia_gui', 'sidraviad', 'sidraviactl')
STATE = Path('/var/lib/sidravia/deb-maintenance')
GATE = STATE / 'lock'
BLOCK = STATE / 'block'
JOURNAL = STATE / 'transaction.json'
ERASE = STATE / 'erase' / 'sidraviactl'


def trusted(path, kind):
    info = path.lstat()
    if not kind(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
        raise RuntimeError('untrusted ownership or file type in the managed installation')
    return info


def owned_directory(path, mode):
    path.mkdir(mode=mode, exist_ok=True)
    return trusted(path, stat.S_ISDIR)


def sync_directory(path):
    fd = os.open(path, os.O_DIRECTORY | os.O_RDONLY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def atomic_write(path, content, mode):
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix=f'.{path.name}-', delete=False) as stream:
        temporary = Path(stream.name)
        try:
            os.fchmod(stream.fileno(), mode)
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
            temporary.replace(path)
            sync_directory(path.parent)
        finally:
            temporary.unlink(missing_ok=True)


def remove_state_file(path):
    try:
        path.unlink()
    except FileNotFoundError:
        return
    sync_directory(path.parent)


@contextlib.contextmanager
def transaction_gate():
    owned_directory(STATE.parent, 0o755)
    directory = owned_directory(STATE, 0o711)
    if directory.st_mode & 0o711 != 0o711:
        raise RuntimeError('maintenance directory must allow application startup checks')
    fd = os.open(GATE, os.O_RDONLY | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o644)
    try:
        info = trusted(GATE, stat.S_ISREG)
        if info.st_size or info.st_mode & 0o444 != 0o444 or identity(GATE) != (
                os.fstat(fd).st_dev, os.fstat(fd).st_ino):
            raise RuntimeError('invalid Debian startup lock')
        deadline = time.monotonic() + 5
        while True:
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() >= deadline:
                    raise RuntimeError('another package operation or startup is active')
                time.sleep(.05)
        for directory, names in ((STATE, (BLOCK.name, JOURNAL.name)), (ERASE.parent, (ERASE.name,))):
            if directory.exists():
                trusted(directory, stat.S_ISDIR)
                for name in names:
                    for leftover in directory.glob(f'.{name}-*'):
                        trusted(leftover, stat.S_ISREG)
                        remove_state_file(leftover)
        yield
    finally:
        os.close(fd)


def verify_payload():
    # This script's binary package supplies the manifest. During rollback, the
    # old package runs its own script and verifies its own restored payload.
    trusted(APP, stat.S_ISDIR)
    for filename, expected in DEB_FILES.items():
        path = APP / filename
        for part in path.relative_to(APP).parents:
            trusted(APP / part, stat.S_ISDIR)
        info = trusted(path, stat.S_ISREG)
        with path.open('rb') as installed:
            if (stat.S_IMODE(info.st_mode) != expected[0]
                    or hashlib.file_digest(installed, 'sha256').hexdigest() != expected[1]):
                raise RuntimeError('installed Debian payload differs from its package manifest')
    if not marker_matches() or not marker_matches('-lifecycle'):
        raise RuntimeError('installed Debian package identity unavailable')


def verify_retirement():
    # A damaged resource must not prevent repair or erasure. Authenticate the
    # installation and limit process discovery to this package's executable paths.
    trusted(APP, stat.S_ISDIR)
    for suffix in ('', '-lifecycle'):
        trusted(APP / ('sidravia.deb' + suffix), stat.S_ISREG)
        if not marker_matches(suffix):
            raise RuntimeError('installed Debian package identity unavailable')
    executables = [APP / name for name in (*NAMES, 'gui-runtime/sidravia_gui') if name in DEB_FILES]
    for path in executables:
        with contextlib.suppress(FileNotFoundError):
            trusted(path.parent, stat.S_ISDIR)
            trusted(path, stat.S_ISREG)
    return executables


def load_state():
    try:
        trusted(JOURNAL, stat.S_ISREG)
        state = json.loads(JOURNAL.read_bytes())
    except FileNotFoundError:
        return None
    if (state['package'] not in ('sidravia', 'sidravia-core')
            or state['action'] not in ('install', 'upgrade', 'remove', 'deconfigure')
            or state['phase'] not in ('blocking', 'quiesced', 'ready')
            or not isinstance(state['services'], dict)):
        raise RuntimeError('invalid Debian maintenance journal')
    return state


def save_state(state):
    atomic_write(JOURNAL, json.dumps(state, sort_keys=True).encode(), 0o600)


def create_state(action, previous=None):
    state = {
        'package': DEB_PACKAGE, 'action': action, 'phase': 'blocking',
        'services': previous['services'] if previous else {},
    }
    # Set the durable gate before recording any intention or touching processes.
    atomic_write(BLOCK, b'dpkg\n', 0o644)
    save_state(state)
    return state


def unit_token(uid, path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != uid or info.st_mode & 0o022:
        raise RuntimeError('invalid ownership of the registered user service')
    return [str(path), info.st_dev, info.st_ino, info.st_mtime_ns, info.st_ctime_ns, info.st_size]


def stage_erase_program():
    try:
        trusted(APP / 'sidraviactl', stat.S_ISREG)
        content = (APP / 'sidraviactl').read_bytes()
    except FileNotFoundError:
        content = b''
    if hashlib.sha256(content).hexdigest() != DEB_FILES['sidraviactl'][1]:
        print('Sidravia Debian maintenance: user integration kept because the verified cleanup program is unavailable.',
              file=sys.stderr)
        return
    owned_directory(ERASE.parent, 0o700)
    if ERASE.parent.stat().st_mode & 0o077:
        raise RuntimeError('invalid access mode of the Debian cleanup directory')
    atomic_write(ERASE, content, 0o755)


def quiesce_deb(action):
    with transaction_gate():
        previous = load_state()
        if previous and previous['package'] != DEB_PACKAGE:
            raise RuntimeError('another Sidravia package has an unfinished transaction')
        if (previous and previous['phase'] == 'quiesced'
                and previous['action'] == action == 'remove' and BLOCK.is_file()):
            return
        executables = verify_retirement()
        state = create_state(action, previous)
        leases = []

        def remember(uid, unit):
            state['services'][str(uid)] = unit_token(uid, unit)
            save_state(state)

        try:
            stop_managed_daemons([], leases, before_stop=remember, executables=executables)
            stop_group(True, 15, executables)
            stop_group(False, 10, executables)
            remaining = targets(executables)
            for _, fd in remaining:
                os.close(fd)
            if remaining:
                raise RuntimeError('application restarted during Debian maintenance')
            if action == 'remove':
                stage_erase_program()
            state['phase'] = 'quiesced'
            save_state(state)
        finally:
            for lease in leases:
                os.close(lease)


def prepare_deb(action):
    with transaction_gate():
        state = load_state()
        if state and state['package'] != DEB_PACKAGE:
            raise RuntimeError('another Sidravia package has unfinished maintenance')
        if action == 'upgrade':
            # dpkg can skip the old prerm when the old package was already
            # unpacked. Only allow replacement under a proven durable gate.
            if not state or state['phase'] != 'quiesced' or not BLOCK.is_file():
                raise RuntimeError('Debian upgrade lacks confirmed quiescence; finish pending configuration first')
        else:
            if APP.is_symlink() or (APP.exists() and any(APP.iterdir())):
                raise RuntimeError('the Debian installation directory already contains files')
            state = create_state('install', state)
            state['phase'] = 'quiesced'
            save_state(state)


def service_available(uid):
    runtime = Path('/run/user') / str(uid)
    return any(path.exists() for path in (runtime / 'systemd/private', runtime / 'bus'))


def restore_user(uid, token):
    try:
        expected = service_registration(uid)
        lease = acquire_service_lease(uid, expected)
    except (FileNotFoundError, KeyError):
        report_user_failure(uid, 'recovery skipped because the registration changed')
        return
    except BlockingIOError as error:
        raise RuntimeError('user service registration is busy; retry Debian configuration') from error
    try:
        directory = os.fstat(lease)
        if identity(expected.parent) != (directory.st_dev, directory.st_ino):
            raise RuntimeError('user service directory changed during Debian recovery')
        try:
            unchanged = unit_token(uid, expected) == token
        except FileNotFoundError:
            unchanged = False
        if not unchanged:
            report_user_failure(uid, 'recovery skipped because the registration changed')
            return
        if not service_available(uid):
            report_user_failure(uid, 'user manager is offline; existing login registration was preserved')
            return
        observed = user_service(uid, 'show', '-p', 'ExecStart,FragmentPath,DropInPaths')
        values = dict(line.split('=', 1) for line in observed.splitlines())
        if (not service_uses_installation(values, expected)
                or any(Path(path).parent != Path('/usr/lib/systemd/user/service.d')
                       for path in values.get('DropInPaths', '').split())):
            report_user_failure(uid, 'recovery skipped because the loaded service changed')
            return
        if not start_and_probe_user_service(uid):
            raise RuntimeError('restored Debian user service did not reach IPC readiness')
    finally:
        os.close(lease)


def complete_deb():
    with transaction_gate():
        state = load_state()
        if state and state['package'] != DEB_PACKAGE:
            raise RuntimeError('another Sidravia package has unfinished maintenance')
        verify_payload()
        # The durable gate protects interrupted unpack/configure. Once this
        # payload has been verified, release it so the daemon can start normally.
        if state:
            state['phase'] = 'ready'
            save_state(state)
        remove_state_file(BLOCK)
        remove_state_file(ERASE)
        remove_empty_directory(ERASE.parent)
    if not state:
        return
    for uid, token in list(state['services'].items()):
        restore_user(int(uid), token)
        del state['services'][uid]
        save_state(state)
    remove_state_file(JOURNAL)


def remove_empty_directory(path):
    try:
        path.rmdir()
    except FileNotFoundError:
        pass


def finish_remove(action):
    # The old binary package's postrm may survive alongside a configured
    # conflicting variant. Its purge must leave the other variant's lock intact.
    if not STATE.exists():
        return
    with transaction_gate():
        state = load_state()
        if state and state['package'] != DEB_PACKAGE:
            return
        try:
            trusted(APP / 'sidravia.deb', stat.S_ISREG)
        except FileNotFoundError:
            pass
        else:
            if action == 'purge':
                return
            raise RuntimeError('Debian payload is still present during removal cleanup')
        if state and state['action'] == 'remove' and action in ('remove', 'purge'):
            if ERASE.is_file():
                trusted(ERASE.parent, stat.S_ISDIR)
                trusted(ERASE, stat.S_ISREG)
                fd = os.open(ERASE, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
                try:
                    with os.fdopen(os.dup(fd), 'rb') as binary:
                        if hashlib.file_digest(binary, 'sha256').hexdigest() != DEB_FILES['sidraviactl'][1]:
                            raise RuntimeError('Debian cleanup program differs from its package manifest')
                    retry = state.get('cleanupUIDs')
                    pending = cleanup_erased_registration(fd, set(retry) if retry is not None else None)
                    if pending:
                        state['cleanupUIDs'] = sorted(pending)
                        save_state(state)
                        raise RuntimeError('user integration cleanup is pending; retry Debian removal or purge')
                finally:
                    os.close(fd)
        remove_state_file(ERASE)
        remove_empty_directory(ERASE.parent)
        remove_state_file(JOURNAL)
        remove_state_file(BLOCK)
        remove_state_file(GATE)
    remove_empty_directory(STATE)
    # The parent can contain state belonging to a different package manager.
    try:
        STATE.parent.rmdir()
    except OSError:
        pass


def deb_main(script, args):
    try:
        if (os.geteuid() != 0 or os.environ.get('DPKG_ROOT', '') not in ('', '/')
                or os.environ.get('DPKG_MAINTSCRIPT_PACKAGE') != DEB_PACKAGE
                or os.environ.get('DPKG_MAINTSCRIPT_NAME') != script):
            raise RuntimeError('Debian maintenance requires its native dpkg transaction')
        os.umask(0o022)
        action = args[0]
        if script == 'prerm' and action in ('upgrade', 'remove', 'deconfigure'):
            quiesce_deb(action)
        elif script == 'preinst' and action in ('install', 'upgrade'):
            prepare_deb(action)
        elif script == 'postinst' and action in ('configure', 'abort-upgrade', 'abort-remove', 'abort-deconfigure'):
            complete_deb()
        elif script == 'postrm' and action in ('remove', 'purge', 'abort-install'):
            finish_remove(action)
        elif (script, action) in (('preinst', 'abort-upgrade'), ('postrm', 'abort-upgrade'),
                                  ('postrm', 'upgrade'), ('postrm', 'failed-upgrade')):
            # The old postinst authenticates the installation before releasing
            # a startup gate after an unwind. Removal is a separate operation.
            pass
        elif script == 'prerm' and action == 'failed-upgrade':
            raise RuntimeError('the old installation did not quiesce; upgrade cancelled')
        else:
            raise RuntimeError('unsupported Debian maintainer script operation')
        return 0
    except (RuntimeError, OSError, subprocess.SubprocessError, ValueError, KeyError) as error:
        if isinstance(error, RuntimeError):
            print('Sidravia Debian maintenance: ' + str(error), file=sys.stderr)
        else:
            print('Sidravia Debian maintenance failed; retry the dpkg operation.', file=sys.stderr)
        return 1
