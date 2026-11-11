#!/usr/bin/env python3
# MaClaw local relay helper, cloud desktop side. MaClawSrv writes and runs it.
# Modes: get (person's file -> here), put (here -> person's computer),
# forward (127.0.0.1:port -> one LAN host:port), socks (SOCKS5 to LAN hosts),
# stdio HOST PORT (ssh ProxyCommand through a socks grant),
# store (a file here -> Hub's bot file store, resumable, for large files).
import argparse, base64, json, os, socket, ssl, subprocess, sys, threading, time
import http.client
from urllib.parse import urlsplit

CONNECT = '/api/v1/desktop-services/relay/connect'


class RelayError(Exception):
    def __init__(self, code, msg):
        Exception.__init__(self, msg)
        self.code = code


def save(state_file, **kw):
    if not state_file:
        return
    kw['pid'] = os.getpid()
    kw['at'] = int(time.time())
    tmp = state_file + '.tmp'
    with open(tmp, 'w') as f:
        json.dump(kw, f)
    os.replace(tmp, state_file)


def token_of(a):
    with open(a.token_file) as f:
        t = f.read().strip()
    if a.mode in ('get', 'put', 'store'):
        try:
            os.unlink(a.token_file)
        except OSError:
            pass
    return t


def connect(url, token, extra):
    u = urlsplit(url)
    port = u.port or (443 if u.scheme == 'https' else 80)
    s = socket.create_connection((u.hostname, port), timeout=60)
    if u.scheme == 'https':
        s = ssl.create_default_context().wrap_socket(s, server_hostname=u.hostname)
    lines = ['GET %s%s HTTP/1.1' % (u.path.rstrip('/'), CONNECT), 'Host: ' + u.netloc,
             'Connection: Upgrade', 'Upgrade: maclaw-relay', 'X-Relay-Token: ' + token]
    lines += ['%s: %s' % kv for kv in extra.items()]
    s.sendall(('\r\n'.join(lines) + '\r\n\r\n').encode())
    buf = b''
    while b'\r\n\r\n' not in buf:
        c = s.recv(65536)
        if not c or len(buf) > 65536:
            s.close()
            raise RelayError('failed', 'the server closed the relay connection')
        buf += c
    head, rest = buf.split(b'\r\n\r\n', 1)
    rows = head.decode('latin-1').split('\r\n')
    status = int(rows[0].split()[1])
    hdr = {}
    for r in rows[1:]:
        if ':' in r:
            k, v = r.split(':', 1)
            hdr[k.strip().lower()] = v.strip()
    if status != 101:
        n = int(hdr.get('content-length') or 0)
        try:
            while len(rest) < n:
                c = s.recv(65536)
                if not c:
                    break
                rest += c
            e = json.loads(rest.decode('utf-8', 'replace') or '{}')
        except Exception:
            e = {}
        s.close()
        raise RelayError(e.get('code') or 'http_%d' % status, e.get('error') or 'HTTP %d' % status)
    info = {}
    if hdr.get('x-relay-info'):
        info = json.loads(base64.b64decode(hdr['x-relay-info']))
    s.settimeout(None)
    return s, rest, info


def unique(path):
    if not os.path.exists(path):
        return path
    stem, ext = os.path.splitext(path)
    for i in range(1, 1000):
        p = '%s (%d)%s' % (stem, i, ext)
        if not os.path.exists(p):
            return p
    raise RelayError('failed', 'too many files named ' + path)


def do_get(a):
    s, rest, info = connect(a.url, token_of(a), {})
    size = int(info.get('size') or 0)
    os.makedirs(os.path.dirname(a.out) or '.', exist_ok=True)
    out = unique(a.out)
    part = out + '.part%d' % os.getpid()
    done, last = 0, 0
    with open(part, 'wb') as f:
        if rest:
            f.write(rest[:size])
            done = min(len(rest), size)
        while done < size:
            c = s.recv(min(1 << 20, size - done))
            if not c:
                break
            f.write(c)
            done += len(c)
            if time.time() - last > 1:
                last = time.time()
                save(a.state, state='running', done=done, total=size, path=out, local=info.get('path'))
    s.close()
    if done != size:
        os.unlink(part)
        raise RelayError('failed', 'received %d of %d bytes; the partial file was removed' % (done, size))
    out = unique(out)
    os.link(part, out)  # never replaces a file that appeared meanwhile
    os.unlink(part)
    save(a.state, state='done', done=done, total=size, path=out, local=info.get('path'))


def do_put(a):
    size = os.path.getsize(a.file)
    if a.max and size > a.max:
        raise RelayError('too_large', 'the file is %d bytes, above the %d byte limit' % (size, a.max))
    s, rest, info = connect(a.url, token_of(a), {'X-Relay-Size': str(size)})
    done, last = 0, 0
    with open(a.file, 'rb') as f:
        while done < size:
            c = f.read(min(1 << 20, size - done))
            if not c:
                break
            s.sendall(c)
            done += len(c)
            if time.time() - last > 1:
                last = time.time()
                save(a.state, state='running', done=done, total=size, path=info.get('path'))
    buf = rest
    s.settimeout(180)
    while b'\n' not in buf:
        c = s.recv(65536)
        if not c:
            break
        buf += c
    s.close()
    try:
        final = json.loads(buf.split(b'\n', 1)[0].decode('utf-8'))
    except Exception:
        final = {}
    if not final.get('ok'):
        raise RelayError(final.get('code') or 'failed', final.get('error') or 'the person\'s computer did not confirm the saved file')
    save(a.state, state='done', done=done, total=size, path=final.get('path'))


STORE_CHUNK = 16 << 20


def store_call(a, method, path, token, body=None, length=0):
    u = urlsplit(a.url)
    port = u.port or (443 if u.scheme == 'https' else 80)
    if u.scheme == 'https':
        c = http.client.HTTPSConnection(u.hostname, port, timeout=120, context=ssl.create_default_context())
    else:
        c = http.client.HTTPConnection(u.hostname, port, timeout=120)
    headers = {'X-Upload-Token': token}
    if body is not None:
        headers['Content-Length'] = str(length)
        headers['Content-Type'] = 'application/octet-stream'
    c.request(method, u.path.rstrip('/') + path, body=body, headers=headers)
    r = c.getresponse()
    raw = r.read()
    c.close()
    try:
        out = json.loads(raw.decode('utf-8') or '{}')
    except Exception:
        out = {}
    return r.status, out


class Slice(object):
    """Reads at most n bytes of f, so one PUT carries one chunk."""
    def __init__(self, f, n):
        self.f, self.left = f, n

    def read(self, k=-1):
        if self.left <= 0:
            return b''
        if k is None or k < 0 or k > self.left:
            k = self.left
        b = self.f.read(min(k, 1 << 20))
        self.left -= len(b)
        return b


def do_store(a):
    token = token_of(a)
    size = os.path.getsize(a.file)
    base = '/api/v1/desktop-services/files/%s' % a.id
    st, out = store_call(a, 'GET', base + '/status', token)
    if st != 200:
        raise RelayError(out.get('code') or 'http_%d' % st, out.get('error') or 'the file store refused the upload (HTTP %d)' % st)
    if int(out.get('size') or 0) != size:
        raise RelayError('changed', 'the file is %d bytes now, %d when the upload started; send it again' % (size, int(out.get('size') or 0)))
    done = int(out.get('received') or 0)
    sha = out.get('sha256') or ''
    complete = bool(out.get('complete'))
    fails = 0
    with open(a.file, 'rb') as f:
        while not complete:
            n = min(STORE_CHUNK, size - done)
            f.seek(done)
            try:
                st, out = store_call(a, 'PUT', base + '/data?offset=%d' % done, token, Slice(f, n), n)
            except Exception as e:
                st, out = 0, {'error': repr(e)}
            if st == 200:
                done = int(out.get('received') or done)
                complete = bool(out.get('complete'))
                sha = out.get('sha256') or sha
                fails = 0
                save(a.state, state='running', done=done, total=size, path=a.file)
                continue
            if st == 409 and 'received' in out:
                done = int(out.get('received') or 0)
                continue
            if st in (403, 404, 410, 413, 507):
                raise RelayError(out.get('code') or 'http_%d' % st, out.get('error') or 'the file store refused the upload (HTTP %d)' % st)
            fails += 1
            if fails > 8:
                raise RelayError('failed', 'uploaded %d of %d bytes; the server kept failing: %s' % (done, size, out.get('error') or 'HTTP %d' % st))
            time.sleep(min(2 ** fails, 30))
            try:
                st2, o2 = store_call(a, 'GET', base + '/status', token)
                if st2 == 200:
                    done = int(o2.get('received') or done)
                    complete = bool(o2.get('complete'))
            except Exception:
                pass
    save(a.state, state='done', done=size, total=size, path=a.file, sha256=sha)


def pump(src, dst, first=b''):
    try:
        if first:
            dst.sendall(first)
        while True:
            c = src.recv(65536)
            if not c:
                break
            dst.sendall(c)
    except OSError:
        pass
    try:
        dst.shutdown(socket.SHUT_WR)
    except OSError:
        pass


def join(client, up, rest):
    t = threading.Thread(target=pump, args=(up, client, rest), daemon=True)
    t.start()
    pump(client, up)
    t.join(30)
    client.close()
    up.close()


class Tunnel:
    def __init__(self, a):
        self.a = a
        self.token = token_of(a)
        self.conns = 0
        self.error = ''

    def open(self, extra):
        try:
            return connect(self.a.url, self.token, extra)
        except RelayError as e:
            self.error = str(e)
            self.status('listening')
            if e.code == 'expired':
                save(self.a.state, state='closed', port=self.port, error=str(e))
                os._exit(0)
            raise

    def status(self, state):
        save(self.a.state, state=state, port=self.port, conns=self.conns, error=self.error)

    def forward(self, c):
        try:
            up, rest, _ = self.open({})
        except RelayError:
            c.close()
            return
        join(c, up, rest)

    def socks(self, c):
        try:
            def need(n):
                b = b''
                while len(b) < n:
                    x = c.recv(n - len(b))
                    if not x:
                        raise OSError('closed')
                    b += x
                return b
            v, n = need(2)
            need(n)
            if v != 5:
                raise OSError('not socks5')
            c.sendall(b'\x05\x00')
            _, cmd, _, at = need(4)
            if at == 1:
                host = socket.inet_ntoa(need(4))
            elif at == 3:
                host = need(need(1)[0]).decode()
            elif at == 4:
                host = '[%s]' % socket.inet_ntop(socket.AF_INET6, need(16))
            else:
                raise OSError('bad address')
            port = int.from_bytes(need(2), 'big')
            if cmd != 1:
                c.sendall(b'\x05\x07\x00\x01' + b'\x00' * 6)
                raise OSError('only CONNECT')
            try:
                up, rest, _ = self.open({'X-Relay-Target': '%s:%d' % (host, port)})
            except RelayError as e:
                c.sendall(b'\x05' + (b'\x02' if e.code in ('denied', 'disabled') else b'\x05') + b'\x00\x01' + b'\x00' * 6)
                raise OSError(str(e))
            c.sendall(b'\x05\x00\x00\x01' + b'\x00' * 6)
        except OSError:
            c.close()
            return
        join(c, up, rest)

    def serve(self, handler):
        ls = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        ls.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        ls.bind(('127.0.0.1', self.a.port))
        ls.listen(64)
        self.port = ls.getsockname()[1]
        self.status('listening')
        while True:
            c, _ = ls.accept()
            self.conns += 1
            threading.Thread(target=handler, args=(c,), daemon=True).start()


def do_stdio(a):
    t = token_of(a)
    up, rest, _ = connect(a.url, t, {'X-Relay-Target': '%s:%d' % (a.host, a.port)})
    if rest:
        sys.stdout.buffer.write(rest)
        sys.stdout.buffer.flush()

    def back():
        while True:
            c = up.recv(65536)
            if not c:
                break
            sys.stdout.buffer.write(c)
            sys.stdout.buffer.flush()
        os._exit(0)
    threading.Thread(target=back, daemon=True).start()
    fd = sys.stdin.fileno()
    while True:
        c = os.read(fd, 65536)
        if not c:
            break
        up.sendall(c)
    up.shutdown(socket.SHUT_WR)
    time.sleep(30)


def detach(a, argv):
    save(a.state, state='starting')
    child = [sys.executable, os.path.abspath(__file__)] + [x for x in argv if x != '--detach']
    log = open(a.state + '.log', 'ab')
    subprocess.Popen(child, stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
    end = time.time() + a.wait
    st = {}
    while time.time() < end:
        time.sleep(0.3)
        try:
            with open(a.state) as f:
                st = json.load(f)
        except Exception:
            continue
        if st.get('state') in ('done', 'failed', 'listening', 'closed'):
            break
    print('MACLAW_RELAY_RESULT ' + json.dumps(st or {'state': 'starting'}))


def main():
    p = argparse.ArgumentParser()
    p.add_argument('mode', choices=['get', 'put', 'forward', 'socks', 'stdio', 'store'])
    p.add_argument('host', nargs='?')
    p.add_argument('hport', nargs='?', type=int)
    p.add_argument('--url', required=True)
    p.add_argument('--token-file', required=True)
    p.add_argument('--state', default='')
    p.add_argument('--out', default='')
    p.add_argument('--file', default='')
    p.add_argument('--max', type=int, default=0)
    p.add_argument('--id', default='')
    p.add_argument('--port', type=int, default=0)
    p.add_argument('--wait', type=float, default=75)
    p.add_argument('--detach', action='store_true')
    a = p.parse_args()
    if a.detach:
        return detach(a, sys.argv[1:])
    try:
        if a.mode == 'get':
            do_get(a)
        elif a.mode == 'put':
            do_put(a)
        elif a.mode == 'store':
            do_store(a)
        elif a.mode == 'stdio':
            a.port = a.hport
            do_stdio(a)
        else:
            t = Tunnel(a)
            t.port = a.port
            t.serve(t.forward if a.mode == 'forward' else t.socks)
    except RelayError as e:
        save(a.state, state='failed', code=e.code, error=str(e))
        sys.stderr.write('relay: %s\n' % e)
        sys.exit(1)
    except Exception as e:
        save(a.state, state='failed', code='failed', error=repr(e))
        sys.stderr.write('relay: %r\n' % e)
        sys.exit(1)


if __name__ == '__main__':
    main()
