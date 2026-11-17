import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import urllib.error
import urllib.request
import zipfile

from probe_codex_original import elevation, run_limited


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--inner', action='store_true')
    parser.add_argument('--root', required=True)
    args = parser.parse_args()
    if not args.inner and elevation():
        return run_limited([sys.executable, str(Path(__file__).resolve()), '--inner', '--root', args.root])
    assert not elevation()
    security_checks = subprocess.run([sys.executable, '-m', 'unittest', 'discover', '-s', str(Path(__file__).parent), '-p', 'test_windows_lifecycle.py', '-v'], timeout=90)
    root = Path(args.root).resolve()
    root.mkdir(parents=True, exist_ok=True)
    source = Path(__file__).resolve().parents[1]
    hook_checks = subprocess.run(['go', 'test', './internal/app', '-run', '^TestWindowsHook', '-count=1', '-v'], cwd=source / 'cli', timeout=120)
    release = root / 'release'
    result = subprocess.check_output([sys.executable, str(source / 'tools/build_release.py'), '--output', str(release)], text=True)
    archive = Path(json.loads(result)['archive'])
    unpacked = root / 'unpacked'
    with zipfile.ZipFile(archive) as stream:
        stream.extractall(unpacked)
    package = next(unpacked.iterdir())
    install = root / 'installed'
    service = root / 'service'
    client = root / 'client'
    env = {**os.environ, 'REMOTE_AGENT_SESSION_INSTALL_DIR': str(install), 'REMOTE_AGENT_SESSION_RUNTIME_DIR': str(root / 'runtime'), 'REMOTE_AGENT_SESSION_STATE_DIR': str(client), 'REMOTE_AGENT_SESSION_SERVICE_STATE_DIR': str(service)}
    subprocess.run(['pwsh', '-NoProfile', '-File', str(package / 'install/install.ps1')], env=env, check=True, timeout=300)
    binary = install / 'bin/remote-agent-session.exe'
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', 0))
        port = probe.getsockname()[1]
    def lifecycle(action, expected=0, state=service):
        result = subprocess.run([str(binary), '--json', action, '--service-state-dir', str(state), '--mode', 'offline', '--port', str(port)], env=env, capture_output=True, text=True, timeout=120)
        assert result.returncode == expected, (action, result.stdout, result.stderr)
        return json.loads(result.stdout)
    try:
        lifecycle('start')
        lifecycle('start')
        assert lifecycle('status')['running']
        try:
            urllib.request.urlopen(f'http://127.0.0.1:{port}/v1/me', timeout=5)
        except urllib.error.HTTPError as error:
            assert error.code == 401
        else:
            raise AssertionError('API authentication boundary missing')
        lifecycle('restart')
        assert lifecycle('status')['running']
        lifecycle('start', expected=1, state=client / 'forbidden')
        assert not (client / 'forbidden').exists()
    finally:
        lifecycle('stop')
    assert not lifecycle('status')['running']
    subprocess.run(['pwsh', '-NoProfile', '-File', str(install / 'install/uninstall.ps1'), '--service-state-dir', str(service)], env=env, check=True, timeout=60)
    assert install.exists() and service.exists()
    subprocess.run([str(install / '.venv/Scripts/python.exe'), '-m', 'pip', 'install', 'pytest', 'httpx', 'ruff'], check=True, timeout=120)
    test_env = {**env, 'PATH': str(install / '.venv/Scripts') + os.pathsep + env['PATH']}
    subprocess.run([str(install / '.venv/Scripts/python.exe'), '-m', 'pytest', 'tests'], cwd=source / 'server', env=test_env, check=True, timeout=300)
    assert security_checks.returncode == 0 and hook_checks.returncode == 0, 'Native security or hook checks failed'
    print(json.dumps({'windows_installed_lifecycle': 'passed', 'backend_persistence_tests': 'passed'}))
    return 0


if __name__ == '__main__':
    sys.exit(main())
