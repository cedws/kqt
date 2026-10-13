#!/usr/bin/env python3
"""Exercise the v0.1 feature workflows through actual terminal input."""
import argparse
import fcntl
import os
from pathlib import Path
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time

from terminal_screen import screen

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--binary',type=Path,default=Path('bin/kqt'))
parser.add_argument('--feature',required=True)
args=parser.parse_args()

def setup_child():
    signal.pthread_sigmask(signal.SIG_SETMASK, [])
    os.setsid()
    fcntl.ioctl(0,termios.TIOCSCTTY,0)

with tempfile.TemporaryDirectory(prefix='kqt-features-') as temporary:
    root=Path(temporary)
    config=root/'config.yaml'
    config.write_text('version: 1\ntheme: catppuccin-mocha\nlayout: classic\n')
    master,slave=os.openpty()
    width,height=120,32
    fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',height,width,0,0))
    original=termios.tcgetattr(slave)
    wrapper="import subprocess,sys; rc=subprocess.call(sys.argv[1:]); print('__kqt_exit__'+str(rc),flush=True); sys.stdin.readline(); sys.exit(rc)"
    launch=[str(args.binary.resolve()),'--demo','--config',str(config),'-n','default']
    process=subprocess.Popen([sys.executable,'-c',wrapper,*launch],stdin=slave,stdout=slave,stderr=slave,preexec_fn=setup_child,env=dict(os.environ,TERM='xterm-256color',COLORTERM='truecolor'))
    raw=bytearray()
    def pump(duration=0.15):
        end=time.monotonic()+duration
        while time.monotonic()<end:
            ready,_,_=select.select([master],[],[],max(0,end-time.monotonic()))
            if ready:
                try: data=os.read(master,65536)
                except OSError: break
                if not data: break
                raw.extend(data)
    def send(value):
        os.write(master,value.encode() if isinstance(value,str) else value)
        pump(0.2)
    def expect(value,timeout=10):
        end=time.monotonic()+timeout
        while time.monotonic()<end:
            pump()
            current=screen(raw,width,height)
            if value in current: return current
        raise AssertionError(value+' not on screen:\n'+current)
    try:
        expect('pods [12]')
        if args.feature=='relations':
            send(':deploy\r'); expect('deployments.apps [1]')
            send('b'); expect('Relations'); expect('owned ReplicaSet')
            send('owned ReplicaSet\r'); expect('replicasets.apps [1]')
            send('b'); expect('Relations'); send('Deployment default/web\r'); expect('deployments.apps [1]')
            send(':ing\r'); expect('ingresses.networking.k8s.io [1]')
            send(':related\r'); expect('ingress backend'); send('\r'); expect('services [1]')
            send('b'); expect('matching pod'); send('\x1b')
            send(':pvc\r'); expect('persistentvolumeclaims [1]')
            send('b'); expect('bound volume'); send('\r'); expect('persistentvolumes [1]')
            send('b'); expect('bound claim'); send('\x1b')
        else:
            raise AssertionError('unknown feature: '+args.feature)
        send('q')
        end=time.monotonic()+5
        while b'__kqt_exit__0' not in raw and time.monotonic()<end: pump()
        assert b'__kqt_exit__0' in raw,'unclean exit'
        assert original==termios.tcgetattr(slave),'terminal attributes not restored'
        send('\n'); process.wait(timeout=5)
        print('PASS feature PTY '+args.feature,flush=True)
    finally:
        if process.poll() is None:
            os.killpg(process.pid,signal.SIGTERM)
            try: process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid,signal.SIGKILL); process.wait()
        os.close(master); os.close(slave)
