#!/usr/bin/env python3
"""Qualify live logs, events, metrics, native shells and localhost forwarding."""
import argparse
import fcntl
import os
from pathlib import Path
import re
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time
import urllib.request

from terminal_screen import screen

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--binary', type=Path, default=Path('bin/kqt'))
parser.add_argument('--kubeconfig', type=Path)
args = parser.parse_args()

def setup_child():
    signal.pthread_sigmask(signal.SIG_SETMASK, [])
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)

def closed(url):
    try:
        with urllib.request.urlopen(url, timeout=0.5):
            return False
    except OSError:
        return True

def run(root, bad_shell=False):
    width, height = 80, 24
    config = root / ('bad-shell.yaml' if bad_shell else 'config.yaml')
    config.write_text('version: 1\nlayout: classic\ntheme: catppuccin-mocha\n'
                      + ('shell: /kqt-missing-shell\n' if bad_shell else ''))
    launch = [str(args.binary.resolve()), '--config', str(config), '--view', 'pods', '-n', 'kqt-test' if args.kubeconfig else 'default']
    launch += ['--kubeconfig', str(args.kubeconfig.resolve())] if args.kubeconfig else ['--demo']
    wrapper = "import subprocess,sys; rc=subprocess.call(sys.argv[1:]); print('__kqt_exit__'+str(rc),flush=True); sys.stdin.readline(); sys.exit(rc)"
    master, slave = os.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', height, width, 0, 0))
    original = termios.tcgetattr(slave)
    env = dict(os.environ, TERM='xterm-256color', COLORTERM='truecolor')
    process = subprocess.Popen([sys.executable, '-c', wrapper, *launch], stdin=slave, stdout=slave, stderr=slave, env=env, preexec_fn=setup_child)
    raw = bytearray()
    def pump(duration=0.2):
        deadline = time.monotonic() + duration
        while time.monotonic() < deadline:
            ready, _, _ = select.select([master], [], [], max(0, deadline-time.monotonic()))
            if ready:
                try:
                    data = os.read(master, 65536)
                except OSError:
                    break
                if not data:
                    break
                raw.extend(data)
    def send(data):
        os.write(master, data)
        pump(0.3)
    def expect(text, timeout=15):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            pump()
            view = screen(raw, width, height)
            if text in view:
                return view
        raise AssertionError(text + ' not on screen:\n' + screen(raw, width, height))
    def expect_port(number):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            pump()
            view = screen(raw, width, height)
            match = re.search(r'Forward #' + str(number) + r'.*127\.0\.0\.1:(\d+)', view)
            if match:
                return 'http://127.0.0.1:' + match[1] + '/'
        raise AssertionError('forward did not start:\n' + view)
    try:
        expect('pods [5]' if args.kubeconfig else 'pods [12]')
        selected = 'kqt-log-fixture' if args.kubeconfig else 'web0000'
        send(('/' + selected + '\r').encode()); expect('pods [1]')
        if bad_shell:
            send(b's'); expect('Shell failed:'); send(b'\x1b'); expect('pods [1]')
        else:
            send(b'E'); expect('Events'); expect('kqt-event-marker' if args.kubeconfig else 'Back-off restarting'); send(b'\x1b')
            send(b'm'); expect('CPU'); expect('MEMORY'); send(b'm')
            send(b'L')
            if not args.kubeconfig:
                expect('Containers'); send(b'\r')
            marker = 'kqt-live-log-marker' if args.kubeconfig else 'live heartbeat'
            expect(marker); send(b' '); expect('paused')
            before = [line for line in screen(raw, width, height).splitlines() if marker in line]
            pump(2)
            after = [line for line in screen(raw, width, height).splitlines() if marker in line]
            assert before == after, 'paused logs moved'
            send(b'/no-such-log-line\r'); expect('No log lines match')
            send(('/\x01\x0b' + marker + '\r').encode()); expect(marker)
            send(b' '); expect(marker)
            destination = root / 'saved.log'
            send(b'o'); send(b'\x01\x0b' + str(destination).encode() + b'\r')
            deadline = time.monotonic() + 5
            while not destination.exists() and time.monotonic() < deadline:
                pump()
            assert marker in destination.read_text(), 'log export lost displayed lines'
            assert destination.stat().st_mode & 0o777 == 0o600, 'export is not private'
            send(b'o'); send(b'\x01\x0b' + str(destination).encode() + b'\r'); expect('Unable to save logs')
            send(b'f'); expect('snapshot'); send(b'f'); expect('live'); send(b'\x1b')
            if args.kubeconfig:
                send(b's'); expect('kqt shell')
                send(b'printf "kqt-shell-%s\\n" marker; stty size\r'); expect('kqt-shell-marker'); expect('24 80')
                width, height = 132, 36
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', height, width, 0, 0))
                os.killpg(process.pid, signal.SIGWINCH); pump(1)
                send(b'stty size\r'); expect('36 132')
                send(b'sleep 30\r'); send(b'\x03'); send(b'printf "kqt-shell-%s\\n" after-interrupt\r'); expect('kqt-shell-after-interrupt')
                width, height = 80, 24
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', height, width, 0, 0))
                os.killpg(process.pid, signal.SIGWINCH); pump(0.5)
                send(b'exit\r'); expect('Shell closed'); expect('pods [1]')
                send(b's'); expect('kqt shell'); send(b'\x1d'); expect('Shell closed'); expect('pods [1]')
                send(b':forward 0:8080\r'); first = expect_port(1)
                with urllib.request.urlopen(first, timeout=3) as response:
                    assert b'kqt-forward-marker' in response.read(), 'wrong forwarded traffic'
                send(b'F'); expect('Forwards'); send(b'\r'); expect('Port forward #1'); expect('Local: 127.0.0.1:')
                send(b'x'); expect('stopped'); send(b'\x1b')
                assert closed(first), 'stopped forward still accepts traffic'
                send(b':forward 0:8080\r'); second = expect_port(2)
            else:
                send(b's'); expect('live Kubernetes connection')
        send(b'q')
        deadline = time.monotonic() + 5
        while b'__kqt_exit__0' not in raw and time.monotonic() < deadline:
            pump()
        assert b'__kqt_exit__0' in raw, 'kqt did not exit normally'
        assert original == termios.tcgetattr(slave), 'terminal attributes were not restored'
        if args.kubeconfig and not bad_shell:
            assert closed(second), 'quitting left a port forward open'
        send(b'\n'); process.wait(timeout=5)
        print('PASS operator PTY ' + ('missing-shell recovery' if bad_shell else 'live logs/events/metrics/shell/forward' if args.kubeconfig else 'demo logs/events/metrics'))
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL); process.wait()
        os.close(master); os.close(slave)

with tempfile.TemporaryDirectory(prefix='kqt-operator-') as temporary:
    run(Path(temporary))
    if args.kubeconfig:
        run(Path(temporary), bad_shell=True)
